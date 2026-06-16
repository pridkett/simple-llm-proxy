package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/webhook"
)

// ChatCompletions handles POST /v1/chat/completions requests.
func ChatCompletions(r *router.Router, store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager, dispatcher *webhook.WebhookDispatcher, cfg config.GeneralSettings) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		startTime := time.Now()

		// Phase 14: capture request body snippet from BodyCapture middleware context.
		// Use cfg.BodySnippetLimit to enforce nil (SQL NULL) when capture is disabled.
		var reqBodySnippet *string
		if cfg.BodySnippetLimit > 0 {
			s := middleware.ReqBodySnippetFromContext(req.Context())
			reqBodySnippet = &s
		}
		// reqBodySnippet is nil when cfg.BodySnippetLimit == 0 → stored as SQL NULL

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

		// X-Charge-Key-ID header: session-auth clients can attribute charges to a specific API key.
		// Admins may charge any key; non-admins are verified via team membership.
		if chargeHeader := req.Header.Get("X-Charge-Key-ID"); chargeHeader != "" && ck == nil && store != nil {
			chargeID, parseErr := strconv.ParseInt(chargeHeader, 10, 64)
			if parseErr != nil {
				model.WriteError(w, model.ErrBadRequest("invalid X-Charge-Key-ID"))
				return
			}
			chargeKey, lookupErr := store.GetAPIKeyByID(ctx, chargeID)
			if lookupErr != nil || chargeKey == nil {
				model.WriteError(w, model.ErrBadRequest("API key not found"))
				return
			}
			if !chargeKey.IsActive {
				model.WriteError(w, model.ErrBadRequest("API key is revoked"))
				return
			}
			// Non-admin users must have access to the key via team membership.
			user := middleware.UserFromContext(ctx)
			if user != nil && !user.IsAdmin {
				accessible, accessErr := store.ListUserAccessibleKeys(ctx, user.ID)
				if accessErr != nil {
					model.WriteError(w, model.ErrInternal("failed to verify key access"))
					return
				}
				found := false
				for _, ak := range accessible {
					if ak.ID == chargeID {
						found = true
						break
					}
				}
				if !found {
					model.WriteError(w, model.ErrForbidden("you do not have access to this API key"))
					return
				}
			}
			apiKeyID = &chargeID
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

		requestID := middleware.RequestIDFromContext(ctx)

		// Emit routing events to webhook dispatcher (D-01: handler layer, after Route() returns).
		emitRoutingEvents(dispatcher, r, result, chatReq.Model, requestID)

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

		bodySnippetLimit := cfg.BodySnippetLimit
		if chatReq.Stream {
			handleStreamingResponse(ctx, w, result, r, store, sa, cm, budget, result.PoolName, apiKeyID, startTime, requestID, bodySnippetLimit, reqBodySnippet)
		} else {
			handleNonStreamingResponse(w, result, r, store, sa, cm, budget, result.PoolName, apiKeyID, startTime, requestID, reqBodySnippet)
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
	requestID string,
	reqBodySnippet *string,
) {
	r.ReportSuccess(result.DeploymentUsed)

	// Log the request if storage is available
	if store != nil && result.Response != nil && result.Response.Usage != nil {
		go logRequest(logRequestParams{
			Store:           store,
			SpendAcc:        sa,
			CostMap:         cm,
			Budget:          budget,
			PoolName:        poolName,
			APIKeyID:        apiKeyID,
			Deployment:      result.DeploymentUsed,
			Endpoint:        "/v1/chat/completions",
			Usage:           result.Response.Usage,
			Status:          http.StatusOK,
			StartTime:       startTime,
			IsStreaming:     false,
			RequestID:       requestID,
			TTFTMs:          nil,
			RespBodySnippet: "",
			ReqBodySnippet:  reqBodySnippet,
		})
	}

	router.SetRouteHeaders(w, result)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result.Response); err != nil {
		fmt.Fprintf(os.Stderr, "handleNonStreamingResponse: encode error (request_id=%s): %v\n", requestID, err)
	}
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
	requestID string,
	bodySnippetLimit int,
	reqBodySnippet *string,
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

	var streamUsage *model.Usage  // accumulated from chunks that carry Usage (Anthropic message_delta)
	var ttftMs *int64             // INSTR-01: nil until first successful Recv()
	var ttftSet bool              // set to true after first Recv() success
	var snippetBuilder strings.Builder // INSTR-03: accumulates Delta.Content up to bodySnippetLimit

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
				go logRequest(logRequestParams{
					Store:           store,
					SpendAcc:        sa,
					CostMap:         cm,
					Budget:          budget,
					PoolName:        poolName,
					APIKeyID:        apiKeyID,
					Deployment:      result.DeploymentUsed,
					Endpoint:        "/v1/chat/completions",
					Usage:           usage,
					Status:          http.StatusOK,
					StartTime:       startTime,
					IsStreaming:     true,
					RequestID:       requestID,
					TTFTMs:          ttftMs,
					RespBodySnippet: snippetBuilder.String(),
					ReqBodySnippet:  reqBodySnippet,
				})
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

		// INSTR-01: Record TTFT at first successful stream.Recv(), NOT at Flush().
		// This measures provider latency, not client serialization time. (Watch-out: LiteLLM #8999.)
		if !ttftSet {
			ms := time.Since(startTime).Milliseconds()
			ttftMs = &ms
			ttftSet = true
		}

		// INSTR-03: Accumulate response body snippet (cap-as-you-go to avoid unbounded memory).
		if chunk != nil && len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
			content := chunk.Choices[0].Delta.Content
			if content != "" && bodySnippetLimit > 0 {
				remaining := bodySnippetLimit - snippetBuilder.Len()
				if remaining > 0 {
					if len(content) > remaining {
						content = content[:remaining]
					}
					snippetBuilder.WriteString(content)
				}
			}
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

// logRequestParams holds all parameters for logRequest, replacing the 15-param positional signature.
// Phase 14 adds ReqBodySnippet as *string; nil means capture was disabled → SQL NULL.
type logRequestParams struct {
	Store           storage.Storage
	SpendAcc        *keystore.SpendAccumulator
	CostMap         *costmap.Manager
	Budget          *router.PoolBudgetManager
	PoolName        string
	APIKeyID        *int64
	Deployment      *provider.Deployment
	Endpoint        string
	Usage           *model.Usage
	Status          int
	StartTime       time.Time
	IsStreaming     bool
	RequestID       string
	TTFTMs          *int64
	RespBodySnippet string
	ReqBodySnippet  *string // nil = disabled (limit=0) → SQL NULL; &"" = empty body; &"..." = captured snippet
}

// logRequest writes the request log to storage, credits the spend accumulator,
// and credits the pool budget manager.
// Called asynchronously (via goroutine) after each successful request.
// requestID is the correlation ID from the X-Request-ID middleware.
// apiKeyID is nil when the request was authenticated via master key.
// budget/poolName may be nil/empty for non-pool models.
// isStreaming indicates whether this was a streaming or non-streaming request.
// ttftMs is nil for non-streaming requests; set to time-to-first-token in ms for streaming.
// respBodySnippet is empty for non-streaming requests; accumulated Delta.Content for streaming.
// reqBodySnippet is nil when capture is disabled (limit=0) → stored as SQL NULL.
func logRequest(p logRequestParams) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var totalCost float64
	if p.CostMap != nil && p.Usage != nil {
		spec := p.CostMap.GetEffectiveSpec(p.Deployment.ModelName, []string{p.Deployment.ActualModel})
		totalCost = float64(p.Usage.PromptTokens)*spec.Spec.InputCostPerToken +
			float64(p.Usage.CompletionTokens)*spec.Spec.OutputCostPerToken +
			float64(p.Usage.CacheReadTokens)*spec.Spec.CacheReadInputTokenCost +
			float64(p.Usage.CacheWriteTokens)*spec.Spec.CacheCreationInputTokenCost
	}

	log := &storage.RequestLog{
		RequestID:        p.RequestID,
		APIKeyID:         p.APIKeyID,
		Model:            p.Deployment.ModelName,
		Provider:         p.Deployment.ProviderName,
		Endpoint:         p.Endpoint,
		InputTokens:      p.Usage.PromptTokens,
		OutputTokens:     p.Usage.CompletionTokens,
		TotalCost:        totalCost,
		StatusCode:       p.Status,
		LatencyMS:        time.Since(p.StartTime).Milliseconds(),
		RequestTime:      p.StartTime,
		IsStreaming:      p.IsStreaming,
		DeploymentKey:    p.Deployment.DeploymentKey(),
		PoolName:         p.PoolName,           // INSTR-02: wired from poolName param
		TTFTMs:           p.TTFTMs,             // INSTR-01: nil for non-streaming
		RespBodySnippet:  p.RespBodySnippet,    // INSTR-03: empty for non-streaming
		ReqBodySnippet:   p.ReqBodySnippet,     // Phase 14: nil *string → SQL NULL when disabled
		CacheReadTokens:  p.Usage.CacheReadTokens,  // INSTR-04: 0 for non-Anthropic
		CacheWriteTokens: p.Usage.CacheWriteTokens, // INSTR-04: 0 for non-Anthropic
	}

	if err := p.Store.LogRequest(ctx, log); err != nil {
		fmt.Fprintf(os.Stderr, "logRequest: failed to write usage log (request_id=%s): %v\n", log.RequestID, err)
	}

	// Credit the spend accumulator after DB write.
	if p.APIKeyID != nil && p.SpendAcc != nil && log.TotalCost > 0 {
		p.SpendAcc.Credit(*p.APIKeyID, log.TotalCost)
	}

	// Credit the pool budget accumulator after DB write (BUDGET-02).
	if p.Budget != nil && p.PoolName != "" && totalCost > 0 {
		p.Budget.Credit(p.PoolName, totalCost)
	}
}

// emitRoutingEvents inspects a RouteResult and emits webhook events for
// routing anomalies (failover, budget exhaustion, pool cooldown).
// Per D-01: event emission happens in the handler layer after Route() returns.
// Per D-03: events fire on every qualifying occurrence, not debounced.
// requestID is threaded into each event's Context map for traceability.
func emitRoutingEvents(dispatcher *webhook.WebhookDispatcher, r *router.Router, result *router.RouteResult, model string, requestID string) {
	if dispatcher == nil {
		return
	}

	// provider_failover: failover occurred AND request ultimately succeeded (D-02)
	if len(result.FailoverReasons) > 0 && result.Error == nil {
		ev := webhook.NewProviderFailoverEvent(model, result)
		ev.Context["request_id"] = requestID
		dispatcher.Emit(ev)
	}

	// budget_exhausted: all pools exhausted for this model (D-02)
	for _, reason := range result.FailoverReasons {
		if reason == router.FailoverBudgetExhausted {
			ev := webhook.NewBudgetExhaustedEvent(model, result)
			ev.Context["request_id"] = requestID
			dispatcher.Emit(ev)
			break
		}
	}

	// pool_cooldown: all members of the result's pool are in cooldown (D-02)
	// Check by looking up the pool and testing each member against CooldownManager.
	if result.PoolName != "" {
		pool := r.GetPool(result.PoolName)
		if pool != nil && len(pool.Members) > 0 && r.IsPoolFullyCooled(pool) {
			ev := webhook.NewPoolCooldownEvent(pool.Name, pool.Members)
			ev.Context["request_id"] = requestID
			dispatcher.Emit(ev)
		}
	}
}
