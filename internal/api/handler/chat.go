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

	messagesapi "github.com/pwagstro/simple_llm_proxy/internal/anthropic"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/webhook"
	"go.opentelemetry.io/otel/trace"
)

// ChatCompletions handles POST /v1/chat/completions requests.
func ChatCompletions(r *router.Router, store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager, dispatcher *webhook.WebhookDispatcher, cfg config.GeneralSettings, tracers ...trace.Tracer) http.HandlerFunc {
	return chatCompletions(r, store, sa, cm, dispatcher, cfg, false, tracers...)
}

// Messages handles Anthropic-compatible POST /v1/messages for any chat provider.
func Messages(r *router.Router, store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager, dispatcher *webhook.WebhookDispatcher, cfg config.GeneralSettings, tracers ...trace.Tracer) http.HandlerFunc {
	return chatCompletions(r, store, sa, cm, dispatcher, cfg, true, tracers...)
}

type chatProtocol struct {
	messages bool
	model    string
}

func (p chatProtocol) endpoint() string {
	if p.messages {
		return "/v1/messages"
	}
	return "/v1/chat/completions"
}

func chatCompletions(r *router.Router, store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager, dispatcher *webhook.WebhookDispatcher, cfg config.GeneralSettings, messages bool, tracers ...trace.Tracer) http.HandlerFunc {
	var tracer trace.Tracer
	if len(tracers) > 0 {
		tracer = tracers[0]
	}
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
		var sessionID string
		var err error
		if messages {
			req.Body = http.MaxBytesReader(w, req.Body, 32<<20)
			var input messagesapi.Request
			sessionID, err = decodeSessionRequest(req, &input, true)
			if err == nil {
				var translated *model.ChatCompletionRequest
				translated, err = input.ChatRequest()
				if err == nil {
					chatReq = *translated
				}
			}
		} else {
			sessionID, err = decodeSessionRequest(req, &chatReq)
		}
		protocol := chatProtocol{messages: messages, model: chatReq.Model}
		writeError := func(w http.ResponseWriter, err *model.ProxyError) {
			if messages {
				model.WriteAnthropicError(w, err)
			} else {
				model.WriteError(w, err)
			}
		}
		if err != nil {
			var sizeErr *http.MaxBytesError
			if messages && errors.As(err, &sizeErr) {
				writeError(w, &model.ProxyError{StatusCode: http.StatusRequestEntityTooLarge, Message: "request exceeds 32 MiB limit"})
			} else {
				writeError(w, model.ErrBadRequest("invalid request body: "+err.Error()))
			}
			return
		}

		if chatReq.Model == "" {
			writeError(w, model.ErrBadRequest("model is required"))
			return
		}

		if len(chatReq.Messages) == 0 {
			writeError(w, model.ErrBadRequest("messages is required"))
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
				writeError(w, model.ErrForbidden("model not allowed: "+chatReq.Model))
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
				writeError(w, model.ErrBadRequest("invalid X-Charge-Key-ID"))
				return
			}
			chargeKey, lookupErr := store.GetAPIKeyByID(ctx, chargeID)
			if lookupErr != nil || chargeKey == nil {
				writeError(w, model.ErrBadRequest("API key not found"))
				return
			}
			if !chargeKey.IsActive {
				writeError(w, model.ErrBadRequest("API key is revoked"))
				return
			}
			// Non-admin users must have access to the key via team membership.
			user := middleware.UserFromContext(ctx)
			if user != nil && !user.IsAdmin {
				accessible, accessErr := store.ListUserAccessibleKeys(ctx, user.ID)
				if accessErr != nil {
					writeError(w, model.ErrInternal("failed to verify key access"))
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
					writeError(w, model.ErrForbidden("you do not have access to this API key"))
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
		ctx, acc := startAccounting(ctx, w, req, store, sa, cm, r, chatReq.Model, protocol.endpoint(), sessionID, apiKeyID, chatReq.Stream)
		defer acc.close()
		ctx, operation := beginOperation(ctx, tracer, "chat", chatReq.Model, chatReq.Stream)
		defer operation.end()
		acc.ctx = ctx

		// Route() owns all retry/failover logic. The callback performs the
		// actual provider call; Route selects deployments and handles failover.
		result := r.Route(ctx, chatReq.Model, stickyKey, func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
			ctx := acc.attemptCtx
			providerReq := chatReq
			providerReq.Model = d.ActualModel
			if chatReq.Stream {
				stream, err := d.Provider.ChatCompletionStream(ctx, &providerReq)
				operation.failedAttempt(d, err)
				return nil, stream, err
			}
			resp, err := d.Provider.ChatCompletion(ctx, &providerReq)
			operation.failedAttempt(d, err)
			return resp, nil, err
		}, acc.options(chatReq.EstimatedTokens()))

		requestID := middleware.RequestIDFromContext(ctx)

		// Emit routing events to webhook dispatcher (D-01: handler layer, after Route() returns).
		emitRoutingEvents(dispatcher, r, result, chatReq.Model, requestID)

		if result.Error != nil {
			acc.err = result.Error
			acc.streaming = false
			acc.complete()
			operation.finish(result, nil, acc.cost(nil), acc.hasCost())
			operation.fail(result.Error)
			// Check for budget exhaustion specifically (BUDGET-04).
			for _, reason := range result.FailoverReasons {
				if reason == router.FailoverBudgetExhausted {
					writeError(w, model.ErrBudgetExceeded("budget exhausted for all available pools"))
					return
				}
			}
			writeError(w, model.ErrProviderError("all providers", result.Error))
			return
		}

		budget := r.BudgetManager()

		bodySnippetLimit := cfg.BodySnippetLimit
		if chatReq.Stream {
			usage, _, err := handleStreamingResponse(ctx, w, result, r, store, sa, cm, budget, result.PoolName, apiKeyID, startTime, requestID, bodySnippetLimit, reqBodySnippet, sessionID, chatReq.StreamOptions != nil && chatReq.StreamOptions.IncludeUsage, protocol)
			acc.err = err
			operation.finish(result, usage, acc.cost(usage), acc.hasCost())
			operation.fail(err)
		} else {
			var usage *model.Usage
			if result.Response != nil {
				usage = result.Response.Usage
			}
			cost := callAccountingFromContext(ctx).cost(usage)
			err := handleNonStreamingResponse(ctx, w, result, r, store, sa, cm, budget, result.PoolName, apiKeyID, startTime, requestID, reqBodySnippet, cost, sessionID, protocol)
			acc.err = err
			operation.finish(result, usage, acc.cost(usage), acc.hasCost())
			operation.fail(err)
		}
	}
}

func handleNonStreamingResponse(
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
	reqBodySnippet *string,
	cost float64,
	sessionID string,
	protocol chatProtocol,
) error {
	var output any = result.Response
	if protocol.messages {
		response, err := messagesapi.FromChat(result.Response, protocol.model)
		if err != nil {
			r.ReportFailure(result.DeploymentUsed)
			model.WriteAnthropicError(w, model.ErrProviderError(result.DeploymentUsed.ProviderName, err))
			return err
		}
		output = response
	}
	r.ReportSuccess(result.DeploymentUsed)

	// Log the request if storage is available
	if store != nil && result.Response != nil {
		logRequest(logRequestParams{
			Accounting:      callAccountingFromContext(ctx),
			Store:           store,
			SpendAcc:        sa,
			CostMap:         cm,
			TotalCost:       cost,
			CostKnown:       true,
			Budget:          budget,
			PoolName:        poolName,
			APIKeyID:        apiKeyID,
			Deployment:      result.DeploymentUsed,
			Endpoint:        protocol.endpoint(),
			Usage:           result.Response.Usage,
			Status:          http.StatusOK,
			StartTime:       startTime,
			IsStreaming:     false,
			RequestID:       requestID,
			SessionID:       sessionID,
			TTFTMs:          nil,
			RespBodySnippet: "",
			ReqBodySnippet:  reqBodySnippet,
		})
	}

	record := callAccountingFromContext(ctx).inlineRecord()
	if protocol.messages {
		output.(*messagesapi.Response).ProxyAccounting = record
	} else {
		result.Response.ProxyAccounting = record
	}
	router.SetRouteHeaders(w, result)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		fmt.Fprintf(os.Stderr, "handleNonStreamingResponse: encode error (request_id=%s): %v\n", requestID, err)
		return err
	}
	return nil
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
	sessionID string,
	includeUsage bool,
	protocol chatProtocol,
) (*model.Usage, float64, error) {
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
		return nil, 0, fmt.Errorf("streaming response writer does not support flushing")
	}

	var encoder *messagesapi.StreamEncoder
	if protocol.messages {
		encoder = messagesapi.NewStreamEncoder(w, protocol.model)
		encoder.BeforeStop = func() { callAccountingFromContext(ctx).emitStream(w, true) }
	}
	streamError := func(err error) {
		if encoder != nil {
			_ = encoder.Error(model.ErrProviderError(result.DeploymentUsed.ProviderName, err), requestID)
			flusher.Flush()
		}
	}
	var streamUsage *model.Usage       // accumulated from chunks that carry Usage (Anthropic message_delta)
	var ttftMs *int64                  // INSTR-01: nil until first successful Recv()
	var ttftSet bool                   // set to true after first Recv() success
	var snippetBuilder strings.Builder // INSTR-03: accumulates Delta.Content up to bodySnippetLimit

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			// Stream completed successfully.
			// Retain missing usage as unknown instead of manufacturing zero tokens.
			usage := streamUsage
			cost := callAccountingFromContext(ctx).cost(usage)
			if store != nil {
				logRequest(logRequestParams{
					Accounting:      callAccountingFromContext(ctx),
					Store:           store,
					SpendAcc:        sa,
					CostMap:         cm,
					TotalCost:       cost,
					CostKnown:       true,
					Budget:          budget,
					PoolName:        poolName,
					APIKeyID:        apiKeyID,
					Deployment:      result.DeploymentUsed,
					Endpoint:        protocol.endpoint(),
					Usage:           usage,
					Status:          http.StatusOK,
					StartTime:       startTime,
					IsStreaming:     true,
					RequestID:       requestID,
					SessionID:       sessionID,
					TTFTMs:          ttftMs,
					RespBodySnippet: snippetBuilder.String(),
					ReqBodySnippet:  reqBodySnippet,
				})
			}
			if encoder != nil {
				if err := encoder.Finish(streamUsage); err != nil {
					var writeErr *messagesapi.WriteError
					if errors.As(err, &writeErr) || ctx.Err() != nil {
						return streamUsage, 0, err
					}
					r.ReportFailure(result.DeploymentUsed)
					streamError(err)
					return streamUsage, 0, err
				}
			}

			// STREAM-01: ReportSuccess fires here, after all chunks received.
			r.ReportSuccess(result.DeploymentUsed)

			if !protocol.messages {
				callAccountingFromContext(ctx).emitStream(w, false)
				if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
					return usage, cost, err
				}
			}
			flusher.Flush()
			return usage, cost, nil
		}
		if err != nil {
			// STREAM-04: client disconnect is not a provider failure.
			// Return without calling ReportFailure.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, 0, err
			}
			// Mid-stream error from provider — report failure.
			r.ReportFailure(result.DeploymentUsed)
			streamError(err)
			return nil, 0, err
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
			callAccountingFromContext(ctx).collector.ObserveUsage(chunk.Usage)
		}

		if encoder != nil {
			if err := encoder.WriteChunk(chunk); err != nil {
				var writeErr *messagesapi.WriteError
				if errors.As(err, &writeErr) || ctx.Err() != nil {
					return streamUsage, 0, err
				}
				r.ReportFailure(result.DeploymentUsed)
				streamError(err)
				return streamUsage, 0, err
			}
			flusher.Flush()
			continue
		}
		if chunk == nil {
			return streamUsage, 0, fmt.Errorf("upstream returned a nil stream chunk")
		}
		output := *chunk
		if !includeUsage {
			output.Usage = nil
			if len(output.Choices) == 0 {
				continue
			}
		}
		data, err := json.Marshal(output)
		if err != nil {
			return nil, 0, err
		}

		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return streamUsage, 0, err
		}
		flusher.Flush()
	}
}

// logRequestParams holds all parameters for logRequest, replacing the 15-param positional signature.
// Phase 14 adds ReqBodySnippet as *string; nil means capture was disabled → SQL NULL.
type logRequestParams struct {
	Accounting      *callAccounting
	Store           storage.Storage
	SpendAcc        *keystore.SpendAccumulator
	CostMap         *costmap.Manager
	TotalCost       float64
	CostKnown       bool
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
	SessionID       string
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
	if p.Accounting != nil {
		p.Accounting.logParams = &p
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	usageKnown := p.Usage != nil
	if p.Usage == nil {
		p.Usage = &model.Usage{}
	}

	totalCost := p.TotalCost
	if !p.CostKnown {
		totalCost = computeCost(p.CostMap, p.Deployment, p.Usage)
	}

	log := &storage.RequestLog{
		RequestID:        p.RequestID,
		SessionID:        p.SessionID,
		APIKeyID:         p.APIKeyID,
		Model:            p.Deployment.ModelName,
		Provider:         p.Deployment.ProviderName,
		Endpoint:         p.Endpoint,
		UsageKnown:       usageKnown,
		InputTokens:      p.Usage.PromptTokens,
		OutputTokens:     p.Usage.CompletionTokens,
		TotalCost:        totalCost,
		StatusCode:       p.Status,
		LatencyMS:        time.Since(p.StartTime).Milliseconds(),
		RequestTime:      p.StartTime,
		IsStreaming:      p.IsStreaming,
		DeploymentKey:    p.Deployment.DeploymentKey(),
		PoolName:         p.PoolName,               // INSTR-02: wired from poolName param
		TTFTMs:           p.TTFTMs,                 // INSTR-01: nil for non-streaming
		RespBodySnippet:  p.RespBodySnippet,        // INSTR-03: empty for non-streaming
		ReqBodySnippet:   p.ReqBodySnippet,         // Phase 14: nil *string → SQL NULL when disabled
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

// computeCost is an in-memory lookup; it must complete before a request span ends.
func computeCost(cm *costmap.Manager, deployment *provider.Deployment, usage *model.Usage) float64 {
	if cm == nil || deployment == nil || usage == nil {
		return 0
	}
	spec := cm.GetEffectiveSpec(deployment.ModelName, []string{deployment.ActualModel})
	return float64(usage.PromptTokens)*spec.Spec.InputCostPerToken +
		float64(usage.CompletionTokens)*spec.Spec.OutputCostPerToken +
		float64(usage.CacheReadTokens)*spec.Spec.CacheReadInputTokenCost +
		float64(usage.CacheWriteTokens)*spec.Spec.CacheCreationInputTokenCost
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
