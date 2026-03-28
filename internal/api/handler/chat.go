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

		// Try to get a deployment with retries
		tried := make(map[*provider.Deployment]bool)
		var lastErr error

		for attempt := 0; attempt <= r.NumRetries(); attempt++ {
			deployment, err := r.GetDeploymentWithRetry(chatReq.Model, tried)
			if err != nil {
				if attempt == 0 {
					// First attempt - model doesn't exist
					model.WriteError(w, model.ErrModelNotFound(chatReq.Model))
					return
				}
				// All deployments tried
				break
			}
			tried[deployment] = true

			// Create request with actual model name
			providerReq := chatReq
			providerReq.Model = deployment.ActualModel

			if chatReq.Stream {
				err = handleStreamingResponse(ctx, w, deployment, &providerReq, r, store, sa, cm, apiKeyID, startTime)
			} else {
				err = handleNonStreamingResponse(ctx, w, deployment, &providerReq, r, store, sa, cm, apiKeyID, startTime)
			}

			if err == nil {
				return
			}

			lastErr = err
			var rlErr *provider.RateLimitError
			if errors.As(err, &rlErr) {
				// 429: apply backoff, do NOT trigger cooldown
				r.ReportRateLimit(deployment, rlErr.RetryAfter)
			} else {
				r.ReportFailure(deployment)
			}
		}

		// All retries exhausted
		model.WriteError(w, model.ErrProviderError("all providers", lastErr))
	}
}

func handleNonStreamingResponse(
	ctx context.Context,
	w http.ResponseWriter,
	deployment *provider.Deployment,
	req *model.ChatCompletionRequest,
	r *router.Router,
	store storage.Storage,
	sa *keystore.SpendAccumulator,
	cm *costmap.Manager,
	apiKeyID *int64,
	startTime time.Time,
) error {
	resp, err := deployment.Provider.ChatCompletion(ctx, req)
	if err != nil {
		return err
	}

	r.ReportSuccess(deployment)

	// Log the request if storage is available
	if store != nil && resp.Usage != nil {
		go logRequest(store, sa, cm, apiKeyID, deployment, "/v1/chat/completions", resp.Usage, http.StatusOK, startTime, false)
	}

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(resp)
}

func handleStreamingResponse(
	ctx context.Context,
	w http.ResponseWriter,
	deployment *provider.Deployment,
	req *model.ChatCompletionRequest,
	r *router.Router,
	store storage.Storage,
	sa *keystore.SpendAccumulator,
	cm *costmap.Manager,
	apiKeyID *int64,
	startTime time.Time,
) error {
	stream, err := deployment.Provider.ChatCompletionStream(ctx, req)
	if err != nil {
		return err
	}
	defer stream.Close()

	// NOTE: r.ReportSuccess is NOT called here — it fires only after successful
	// stream completion (in the io.EOF branch below).
	// See ADR 006 D-01: calling here (at stream open) was the STREAM-01 bug.

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	var streamUsage *model.Usage // accumulated from chunks that carry Usage (Anthropic message_delta)

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			// Stream completed successfully.
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()

			// STREAM-01: ReportSuccess fires here, after all chunks received.
			r.ReportSuccess(deployment)

			// STREAM-02: use token counts from stream; fall back to empty usage if none provided.
			usage := streamUsage
			if usage == nil {
				usage = &model.Usage{}
			}
			if store != nil {
				go logRequest(store, sa, cm, apiKeyID, deployment, "/v1/chat/completions", usage, http.StatusOK, startTime, true)
			}
			return nil
		}
		if err != nil {
			// STREAM-04: client disconnect is not a provider failure.
			// Return nil so the caller does NOT call ReportFailure.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}

		// Accumulate usage from any chunk that carries it (Anthropic sends on message_delta).
		if chunk != nil && chunk.Usage != nil {
			streamUsage = chunk.Usage
		}

		data, err := json.Marshal(chunk)
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// logRequest writes the request log to storage and credits the spend accumulator.
// Called asynchronously (via goroutine) after each successful request.
// apiKeyID is nil when the request was authenticated via master key.
// isStreaming indicates whether this was a streaming or non-streaming request.
func logRequest(store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager, apiKeyID *int64, deployment *provider.Deployment, endpoint string, usage *model.Usage, status int, startTime time.Time, isStreaming bool) {
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
}
