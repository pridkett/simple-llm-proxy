package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	backgroundresponses "github.com/pwagstro/simple_llm_proxy/internal/responses"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/webhook"
	"go.opentelemetry.io/otel/trace"
)

// errNotResponsesProvider signals that the routed deployment does not implement
// provider.ResponsesProvider (ADR 010 D-01) — only OpenAI does today.
var errNotResponsesProvider = errors.New("model does not support the Responses API")

// Responses handles POST /v1/responses requests: synchronous, streaming, and
// background (async) creation, per ADR 010.
func Responses(r *router.Router, store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager, dispatcher *webhook.WebhookDispatcher, cfg config.GeneralSettings, tracers ...trace.Tracer) http.HandlerFunc {
	var tracer trace.Tracer
	if len(tracers) > 0 {
		tracer = tracers[0]
	}
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		startTime := time.Now()

		var reqBodySnippet *string
		if cfg.BodySnippetLimit > 0 {
			s := middleware.ReqBodySnippetFromContext(req.Context())
			reqBodySnippet = &s
		}

		var respReq model.ResponsesRequest
		sessionID, err := decodeSessionRequest(req, &respReq)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid request body: "+err.Error()))
			return
		}
		if respReq.Model == "" {
			model.WriteError(w, model.ErrBadRequest("model is required"))
			return
		}
		if respReq.Input == nil {
			model.WriteError(w, model.ErrBadRequest("input is required"))
			return
		}

		ck := middleware.APIKeyFromContext(ctx)
		if ck != nil && len(ck.AllowedModels) > 0 {
			allowed := false
			for _, m := range ck.AllowedModels {
				if m == respReq.Model {
					allowed = true
					break
				}
			}
			if !allowed {
				model.WriteError(w, model.ErrForbidden("model not allowed: "+respReq.Model))
				return
			}
		}

		var apiKeyID *int64
		if ck != nil {
			id := ck.Key.ID
			apiKeyID = &id
		}

		stickyKey := ""
		if ck != nil {
			stickyKey = ck.Key.KeyHash
		}
		ctx, acc := startAccounting(ctx, w, req, store, sa, cm, r, respReq.Model, "/v1/responses", sessionID, apiKeyID, respReq.Stream)
		defer acc.close()
		ctx, operation := beginOperation(ctx, tracer, "generate_content", respReq.Model, respReq.Stream)
		defer operation.end()
		acc.ctx = ctx

		var respResult *model.ResponsesResponse
		var respStream provider.ResponsesStream
		result := r.Route(ctx, respReq.Model, stickyKey, func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
			ctx := acc.attemptCtx
			rp, ok := d.Provider.(provider.ResponsesProvider)
			if !ok {
				operation.failedAttempt(d, errNotResponsesProvider)
				return nil, nil, errNotResponsesProvider
			}
			providerReq := respReq
			providerReq.Model = d.ActualModel

			if respReq.Stream {
				stream, err := rp.CreateResponseStream(ctx, &providerReq)
				if err != nil {
					operation.failedAttempt(d, err)
					return nil, nil, err
				}
				respStream = stream
				return nil, nil, nil
			}

			resp, err := rp.CreateResponse(ctx, &providerReq)
			if err != nil {
				operation.failedAttempt(d, err)
				return nil, nil, err
			}
			respResult = resp
			return &model.ChatCompletionResponse{Usage: resp.Usage}, nil, nil
		}, acc.options(1))

		requestID := middleware.RequestIDFromContext(ctx)
		emitRoutingEvents(dispatcher, r, result, respReq.Model, requestID)

		if result.Error != nil {
			acc.err = result.Error
			acc.streaming = false
			acc.complete()
			operation.finish(result, nil, acc.cost(nil), acc.hasCost())
			operation.fail(result.Error)
			if errors.Is(result.Error, errNotResponsesProvider) {
				model.WriteError(w, model.ErrBadRequest(errNotResponsesProvider.Error()+": "+respReq.Model))
				return
			}
			for _, reason := range result.FailoverReasons {
				if reason == router.FailoverBudgetExhausted {
					model.WriteError(w, model.ErrBudgetExceeded("budget exhausted for all available pools"))
					return
				}
			}
			model.WriteError(w, model.ErrProviderError("all providers", result.Error))
			return
		}

		budget := r.BudgetManager()

		// STREAM-01 (ADR 006): success is reported only after the stream fully
		// completes (see handleResponsesStream's EOF branch), not at stream-open —
		// a mid-stream failure must still count as a deployment failure.
		if respReq.Stream {
			usage, _, err := handleResponsesStream(ctx, w, respStream, result, r, store, sa, cm, budget, requestID, apiKeyID, startTime, reqBodySnippet, sessionID)
			operation.finish(result, usage, acc.cost(usage), acc.hasCost())
			acc.err = err
			operation.fail(err)
			return
		}
		r.ReportSuccess(result.DeploymentUsed)

		if respReq.Background && !respResult.IsTerminal() {
			acc.savePending()
			if store != nil {
				if err := persistResponsesJob(store, respResult, result.DeploymentUsed, result.PoolName, apiKeyID, &respReq, sessionID, acc.collector.Snapshot(false).ID); err != nil {
					acc.err = err
					operation.fail(err)
					// The upstream job was already created — the client must not receive a
					// response_id that GET /v1/responses/{id} can never resolve.
					fmt.Fprintf(os.Stderr, "Responses: failed to persist background job %s (request_id=%s): %v\n", respResult.ID, requestID, err)
					model.WriteError(w, model.ErrInternal("background job created upstream but failed to persist; retry may create a duplicate job"))
					return
				}
			}
			acc.background = true
			if acc.inline {
				respResult.ProxyAccounting = acc.collector.Snapshot(false)
			}
			operation.finish(result, nil, acc.cost(nil), acc.hasCost())
			router.SetRouteHeaders(w, result)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(respResult)
			return
		}

		cost := acc.cost(respResult.Usage)
		operation.finish(result, respResult.Usage, cost, acc.hasCost())
		if store != nil && respResult.Usage != nil {
			logRequest(logRequestParams{
				Accounting:      callAccountingFromContext(ctx),
				Store:           store,
				SpendAcc:        sa,
				CostMap:         cm,
				TotalCost:       cost,
				CostKnown:       true,
				Budget:          budget,
				PoolName:        result.PoolName,
				APIKeyID:        apiKeyID,
				Deployment:      result.DeploymentUsed,
				Endpoint:        "/v1/responses",
				Usage:           respResult.Usage,
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

		respResult.ProxyAccounting = acc.inlineRecord()
		router.SetRouteHeaders(w, result)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(respResult); err != nil {
			fmt.Fprintf(os.Stderr, "Responses: encode error (request_id=%s): %v\n", requestID, err)
		}
	}
}

func handleResponsesStream(
	ctx context.Context,
	w http.ResponseWriter,
	stream provider.ResponsesStream,
	result *router.RouteResult,
	r *router.Router,
	store storage.Storage,
	sa *keystore.SpendAccumulator,
	cm *costmap.Manager,
	budget *router.PoolBudgetManager,
	requestID string,
	apiKeyID *int64,
	startTime time.Time,
	reqBodySnippet *string,
	sessionID string,
) (*model.Usage, float64, error) {
	defer stream.Close()

	router.SetRouteHeaders(w, result)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, 0, fmt.Errorf("streaming response writer does not support flushing")
	}

	var finalUsage *model.Usage

	for {
		event, err := stream.Recv()
		if err == io.EOF {
			r.ReportSuccess(result.DeploymentUsed)
			usage := finalUsage
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
					PoolName:        result.PoolName,
					APIKeyID:        apiKeyID,
					Deployment:      result.DeploymentUsed,
					Endpoint:        "/v1/responses",
					Usage:           usage,
					Status:          http.StatusOK,
					StartTime:       startTime,
					IsStreaming:     true,
					RequestID:       requestID,
					SessionID:       sessionID,
					TTFTMs:          nil,
					RespBodySnippet: "",
					ReqBodySnippet:  reqBodySnippet,
				})
			}
			callAccountingFromContext(ctx).emitStream(w, true)
			flusher.Flush()
			return usage, cost, nil
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, 0, err
			}
			r.ReportFailure(result.DeploymentUsed)
			return nil, 0, err
		}

		if event.Response != nil && event.Response.Usage != nil {
			finalUsage = event.Response.Usage
			callAccountingFromContext(ctx).collector.ObserveUsage(finalUsage)
		}

		data, err := json.Marshal(event)
		if err != nil {
			return nil, 0, err
		}
		// The Responses API's SSE contract pairs each data line with an `event:`
		// line naming the event type (response.created, response.completed, ...);
		// OpenAI's own SDK dispatches on it. event.Type mirrors it exactly since
		// it's parsed from the same upstream payload's "type" field.
		if event.Type != "" {
			fmt.Fprintf(w, "event: %s\n", event.Type)
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// persistResponsesJob inserts a responses_jobs row for a newly-created background job.
// Runs synchronously, before the "queued" response is returned to the client — a
// client must never receive a response_id that has no matching row, since every
// subsequent GET /v1/responses/{id} would 404 with no visible error. Uses a
// detached 5s context (not the request's) so the write isn't aborted by a client
// disconnect racing the insert.
func persistResponsesJob(store storage.Storage, resp *model.ResponsesResponse, d *provider.Deployment, poolName string, apiKeyID *int64, req *model.ResponsesRequest, sessionID string, accountingID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}
	respJSON, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshaling response: %w", err)
	}
	respJSONStr := string(respJSON)

	job := &storage.ResponsesJob{
		AccountingID:  accountingID,
		ID:            resp.ID,
		SessionID:     sessionID,
		APIKeyID:      apiKeyID,
		DeploymentKey: d.DeploymentKey(),
		ModelName:     d.ModelName,
		PoolName:      poolName,
		Status:        resp.Status,
		RequestJSON:   string(reqJSON),
		ResponseJSON:  &respJSONStr,
	}
	if err := store.CreateResponsesJob(ctx, job); err != nil {
		return fmt.Errorf("creating responses job: %w", err)
	}
	return nil
}

// GetResponseJob handles GET /v1/responses/{id}: returns the last known state
// of a background job from storage without making an upstream call.
func GetResponseJob(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		id := chi.URLParam(req, "id")

		if store == nil {
			model.WriteError(w, model.ErrNotFound("response not found: "+id))
			return
		}

		job, err := store.GetResponsesJob(ctx, id)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to look up response: "+err.Error()))
			return
		}
		if job == nil {
			model.WriteError(w, model.ErrNotFound("response not found: "+id))
			return
		}

		if !responsesJobAccessible(req, job) {
			model.WriteError(w, model.ErrNotFound("response not found: "+id))
			return
		}

		setJobAccounting(w, req, store, job, nil)
		writeResponsesJob(w, job)
	}
}

// CancelResponseJob handles DELETE /v1/responses/{id}: cancels an in-progress
// background job upstream and updates its stored state.
func CancelResponseJob(r *router.Router, store storage.Storage, accumulators ...*keystore.SpendAccumulator) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		id := chi.URLParam(req, "id")

		if store == nil {
			model.WriteError(w, model.ErrNotFound("response not found: "+id))
			return
		}

		job, err := store.GetResponsesJob(ctx, id)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to look up response: "+err.Error()))
			return
		}
		if job == nil || !responsesJobAccessible(req, job) {
			model.WriteError(w, model.ErrNotFound("response not found: "+id))
			return
		}

		if isTerminalStatus(job.Status) {
			setJobAccounting(w, req, store, job, nil)
			writeResponsesJob(w, job)
			return
		}

		d, err := r.GetDeploymentByKey(job.ModelName, job.DeploymentKey)
		if err != nil {
			model.WriteError(w, model.ErrInternal("cannot resolve deployment for cancellation: "+err.Error()))
			return
		}
		rp, ok := d.Provider.(provider.ResponsesProvider)
		if !ok {
			model.WriteError(w, model.ErrBadRequest(errNotResponsesProvider.Error()))
			return
		}

		ctx, collector, resumeErr := backgroundresponses.ResumeAccounting(ctx, store, job)
		if resumeErr != nil {
			model.WriteError(w, model.ErrInternal("cannot resume accounting"))
			return
		}
		resp, err := rp.CancelResponse(ctx, id)
		if err != nil {
			model.WriteError(w, model.ErrProviderError(d.ProviderName, err))
			return
		}

		var sa *keystore.SpendAccumulator
		if len(accumulators) > 0 {
			sa = accumulators[0]
		}
		if resp.IsTerminal() {
			if handled, err := backgroundresponses.FinishAccounting(ctx, store, job, resp, collector, sa, r); handled {
				if err != nil {
					model.WriteError(w, model.ErrInternal("failed to finalize cancelled response"))
					return
				}
				setJobAccounting(w, req, store, job, resp)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
				return
			}
		}
		respJSON, err := json.Marshal(resp)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to marshal cancelled response: "+err.Error()))
			return
		}
		respJSONStr := string(respJSON)
		now := time.Now().UTC()
		var completedAt *time.Time
		if resp.IsTerminal() {
			completedAt = &now
		}
		if collector != nil {
			if db, ok := store.(storage.AccountingStorage); ok {
				if err := db.PutAccounting(ctx, collector.Snapshot(false)); err != nil {
					model.WriteError(w, model.ErrInternal("failed to save cancellation accounting"))
					return
				}
			}
		}
		if updErr := store.UpdateResponsesJob(ctx, id, resp.Status, &respJSONStr, nil, completedAt); updErr != nil {
			fmt.Fprintf(os.Stderr, "CancelResponseJob: update failed for %s: %v\n", id, updErr)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// responsesJobAccessible reports whether the current caller (master key or the
// API key that created the job) may read/cancel this job.
func responsesJobAccessible(req *http.Request, job *storage.ResponsesJob) bool {
	ck := middleware.APIKeyFromContext(req.Context())
	if ck == nil {
		return true // master key: full access
	}
	return job.APIKeyID != nil && *job.APIKeyID == ck.Key.ID
}

func isTerminalStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled", "incomplete":
		return true
	default:
		return false
	}
}

// writeResponsesJob writes the job's last known response_json verbatim, or a
// minimal synthesized body when no response has been recorded yet.
func writeResponsesJob(w http.ResponseWriter, job *storage.ResponsesJob) {
	w.Header().Set("Content-Type", "application/json")
	if job.ResponseJSON != nil {
		w.Write([]byte(*job.ResponseJSON))
		return
	}
	json.NewEncoder(w).Encode(model.ResponsesResponse{
		ID:     job.ID,
		Object: "response",
		Model:  job.ModelName,
		Status: job.Status,
	})
}
