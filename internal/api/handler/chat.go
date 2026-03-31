package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// ChatCompletions handles POST /v1/chat/completions requests.
func ChatCompletions(r *router.Router, store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		startTime := time.Now()

		var chatReq model.ChatCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid request body: "+err.Error()))
			return
		}

		if chatReq.Model == "" {
			model.WriteError(w, model.ErrBadRequest("model is required"))
			return
		}

		if len(chatReq.Messages) == 0 {
			model.WriteError(w, model.ErrBadRequest("messages is required"))
			return
		}

		// Model allowlist enforcement (per D-10, KEY-02).
		// Check is done here (not middleware) because it requires the decoded model name.
		ck := middleware.APIKeyFromContext(ctx)
		if ck != nil && len(ck.AllowedModels) > 0 {
			allowed := false
			for _, m := range ck.AllowedModels {
				if m == chatReq.Model {
					allowed = true
					break
				}
			}
			if !allowed {
				model.WriteError(w, model.ErrForbidden("model not allowed: "+chatReq.Model))
				return
			}
		}

		// Extract apiKeyID for cost attribution (nil when authenticated via master key).
		var apiKeyID *int64
		if ck != nil {
			id := ck.Key.ID
			apiKeyID = &id
		}

		// Derive sticky session key from API key hash (empty for master key).
		stickyKey := ""
		if ck != nil {
			stickyKey = ck.Key.KeyHash
		}

		// Route() owns all retry/failover logic. The callback performs the
		// actual provider call; Route selects deployments and handles failover.
		result := r.Route(ctx, chatReq.Model, stickyKey, func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
			providerReq := chatReq
			providerReq.Model = d.ActualModel
			if chatReq.Stream {
				stream, err := d.Provider.ChatCompletionStream(ctx, &providerReq)
				return nil, stream, err
			}
			resp, err := d.Provider.ChatCompletion(ctx, &providerReq)
			return resp, nil, err
		})

		if result.Error != nil {
			// Check for budget exhaustion specifically (BUDGET-04).
			for _, reason := range result.FailoverReasons {
				if reason == router.FailoverBudgetExhausted {
					model.WriteError(w, model.ErrBudgetExceeded("budget exhausted for all available pools"))
					return
				}
			}
			if result.DeploymentUsed == nil && len(result.DeploymentsTried) == 0 {
				model.WriteError(w, model.ErrModelNotFound(chatReq.Model))
			} else {
				model.WriteError(w, model.ErrProviderError("all providers", result.Error))
			}
			return
		}

		budget := r.BudgetManager()

		if chatReq.Stream {
			handleStreamingResponse(ctx, w, result, r, store, sa, cm, budget, result.PoolName, apiKeyID, startTime)
		} else {
			handleNonStreamingResponse(w, result, r, store, sa, cm, budget, result.PoolName, apiKeyID, startTime)
		}
	}
}

func handleNonStreamingResponse(
	w http.ResponseWriter,
	result *router.RouteResult,
	r *router.Router,
	store storage.Storage,
	sa *keystore.SpendAccumulator,
	cm *costmap.Manager,
	budget *router.PoolBudgetManager,
	poolName string,
	apiKeyID *int64,
	startTime time.Time,
) {
	r.ReportSuccess(result.DeploymentUsed)

	// Log the request if storage is available
	if store != nil && result.Response != nil && result.Response.Usage != nil {
		go logRequest(store, sa, cm, budget, poolName, apiKeyID, result.DeploymentUsed, "/v1/chat/completions", result.Response.Usage, http.StatusOK, startTime, false)
	}

	router.SetRouteHeaders(w, result)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result.Response)
}

func handleStreamingResponse(
	ctx context.Context,
	w http.ResponseWriter,
	result *router.RouteResult,
	r *router.Router,
	store storage.Storage,
	sa *keystore.SpendAccumulator,
	cm *costmap.Manager,
	budget *router.PoolBudgetManager,
	poolName string,
	apiKeyID *int64,
	startTime time.Time,
) {
	stream := result.Stream
	defer stream.Close()

	// NOTE: r.ReportSuccess is NOT called here — it fires only after successful
	// stream completion (in the io.EOF branch below).
	// See ADR 006 D-01: calling here (at stream open) was the STREAM-01 bug.

	// Set route metadata headers BEFORE SSE headers and first chunk.
	router.SetRouteHeaders(w, result)

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	var streamUsage *model.Usage // accumulated from chunks that carry Usage (Anthropic message_delta)

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			// Stream completed successfully.
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()

			// STREAM-01: ReportSuccess fires here, after all chunks received.
			r.ReportSuccess(result.DeploymentUsed)

			// STREAM-02: use token counts from stream; fall back to empty usage if none provided.
			usage := streamUsage
			if usage == nil {
				usage = &model.Usage{}
			}
			if store != nil {
				go logRequest(store, sa, cm, budget, poolName, apiKeyID, result.DeploymentUsed, "/v1/chat/completions", usage, http.StatusOK, startTime, true)
			}
			return
		}
		if err != nil {
			// STREAM-04: client disconnect is not a provider failure.
			// Return without calling ReportFailure.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			// Mid-stream error from provider — report failure.
			r.ReportFailure(result.DeploymentUsed)
			return
		}

		// Accumulate usage from any chunk that carries it (Anthropic sends on message_delta).
		if chunk != nil && chunk.Usage != nil {
			streamUsage = chunk.Usage
		}

		data, err := json.Marshal(chunk)
		if err != nil {
			return
		}

		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// logRequest writes the request log to storage, credits the spend accumulator,
// and credits the pool budget manager.
// Called asynchronously (via goroutine) after each successful request.
// apiKeyID is nil when the request was authenticated via master key.
// budget/poolName may be nil/empty for non-pool models.
// isStreaming indicates whether this was a streaming or non-streaming request.
func logRequest(store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager, budget *router.PoolBudgetManager, poolName string, apiKeyID *int64, deployment *provider.Deployment, endpoint string, usage *model.Usage, status int, startTime time.Time, isStreaming bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var totalCost float64
	if cm != nil && usage != nil {
		spec := cm.GetEffectiveSpec(deployment.ModelName, []string{deployment.ActualModel})
		totalCost = float64(usage.PromptTokens)*spec.Spec.InputCostPerToken +
			float64(usage.CompletionTokens)*spec.Spec.OutputCostPerToken
	}

	log := &storage.RequestLog{
		RequestID:     fmt.Sprintf("%d", time.Now().UnixNano()),
		APIKeyID:      apiKeyID,
		Model:         deployment.ModelName,
		Provider:      deployment.ProviderName,
		Endpoint:      endpoint,
		InputTokens:   usage.PromptTokens,
		OutputTokens:  usage.CompletionTokens,
		TotalCost:     totalCost,
		StatusCode:    status,
		LatencyMS:     time.Since(startTime).Milliseconds(),
		RequestTime:   startTime,
		IsStreaming:   isStreaming,
		DeploymentKey: deployment.DeploymentKey(),
	}

	store.LogRequest(ctx, log)

	// Credit the spend accumulator after DB write.
	if apiKeyID != nil && sa != nil && log.TotalCost > 0 {
		sa.Credit(*apiKeyID, log.TotalCost)
	}

	// Credit the pool budget accumulator after DB write (BUDGET-02).
	if budget != nil && poolName != "" && totalCost > 0 {
		budget.Credit(poolName, totalCost)
	}
}
