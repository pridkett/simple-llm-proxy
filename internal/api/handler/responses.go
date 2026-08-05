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
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/webhook"
)

// errNotResponsesProvider signals that the routed deployment does not implement
// provider.ResponsesProvider (ADR 010 D-01) — only OpenAI does today.
var errNotResponsesProvider = errors.New("model does not support the Responses API")

// Responses handles POST /v1/responses requests: synchronous, streaming, and
// background (async) creation, per ADR 010.
func Responses(r *router.Router, store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager, dispatcher *webhook.WebhookDispatcher, cfg config.GeneralSettings) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		startTime := time.Now()

		var reqBodySnippet *string
		if cfg.BodySnippetLimit > 0 {
			s := middleware.ReqBodySnippetFromContext(req.Context())
			reqBodySnippet = &s
		}

		var respReq model.ResponsesRequest
		if err := json.NewDecoder(req.Body).Decode(&respReq); err != nil {
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

		var respResult *model.ResponsesResponse
		var respStream provider.ResponsesStream
		result := r.Route(ctx, respReq.Model, stickyKey, func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
			rp, ok := d.Provider.(provider.ResponsesProvider)
			if !ok {
				return nil, nil, errNotResponsesProvider
			}
			providerReq := respReq
			providerReq.Model = d.ActualModel

			if respReq.Stream {
				stream, err := rp.CreateResponseStream(ctx, &providerReq)
				if err != nil {
					return nil, nil, err
				}
				respStream = stream
				return nil, nil, nil
			}

			resp, err := rp.CreateResponse(ctx, &providerReq)
			if err != nil {
				return nil, nil, err
			}
			respResult = resp
			return nil, nil, nil
		})

		requestID := middleware.RequestIDFromContext(ctx)
		emitRoutingEvents(dispatcher, r, result, respReq.Model, requestID)

		if result.Error != nil {
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
			if result.DeploymentUsed == nil && len(result.DeploymentsTried) == 0 {
				model.WriteError(w, model.ErrModelNotFound(respReq.Model))
			} else {
				model.WriteError(w, model.ErrProviderError("all providers", result.Error))
			}
			return
		}

		budget := r.BudgetManager()
		r.ReportSuccess(result.DeploymentUsed)

		if respReq.Stream {
			handleResponsesStream(w, respStream, result, r, store, sa, cm, budget, requestID, apiKeyID, startTime, reqBodySnippet)
			return
		}

		if respReq.Background && respResult.Status != "completed" && respResult.Status != "failed" {
			if store != nil {
				persistResponsesJob(store, respResult, result.DeploymentUsed, apiKeyID, &respReq)
			}
			router.SetRouteHeaders(w, result)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(respResult)
			return
		}

		if store != nil && respResult.Usage != nil {
			go logRequest(logRequestParams{
				Store:           store,
				SpendAcc:        sa,
				CostMap:         cm,
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
				TTFTMs:          nil,
				RespBodySnippet: "",
				ReqBodySnippet:  reqBodySnippet,
			})
		}

		router.SetRouteHeaders(w, result)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(respResult); err != nil {
			fmt.Fprintf(os.Stderr, "Responses: encode error (request_id=%s): %v\n", requestID, err)
		}
	}
}

func handleResponsesStream(
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
) {
	defer stream.Close()

	router.SetRouteHeaders(w, result)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	var finalUsage *model.Usage

	for {
		event, err := stream.Recv()
		if err == io.EOF {
			r.ReportSuccess(result.DeploymentUsed)
			if store != nil {
				usage := finalUsage
				if usage == nil {
					usage = &model.Usage{}
				}
				go logRequest(logRequestParams{
					Store:           store,
					SpendAcc:        sa,
					CostMap:         cm,
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
					TTFTMs:          nil,
					RespBodySnippet: "",
					ReqBodySnippet:  reqBodySnippet,
				})
			}
			return
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			r.ReportFailure(result.DeploymentUsed)
			return
		}

		if event.Response != nil && event.Response.Usage != nil {
			finalUsage = event.Response.Usage
		}

		data, err := json.Marshal(event)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// persistResponsesJob inserts a responses_jobs row for a newly-created background job.
// Uses a detached context (not the request's) since the write must complete even
// after the HTTP handler has already returned the "queued" response to the client.
func persistResponsesJob(store storage.Storage, resp *model.ResponsesResponse, d *provider.Deployment, apiKeyID *int64, req *model.ResponsesRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reqJSON, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "persistResponsesJob: marshal request failed for %s: %v\n", resp.ID, err)
		return
	}
	respJSON, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "persistResponsesJob: marshal response failed for %s: %v\n", resp.ID, err)
		return
	}
	respJSONStr := string(respJSON)

	job := &storage.ResponsesJob{
		ID:            resp.ID,
		APIKeyID:      apiKeyID,
		DeploymentKey: d.DeploymentKey(),
		ModelName:     d.ModelName,
		Status:        resp.Status,
		RequestJSON:   string(reqJSON),
		ResponseJSON:  &respJSONStr,
	}
	if err := store.CreateResponsesJob(ctx, job); err != nil {
		fmt.Fprintf(os.Stderr, "persistResponsesJob: create failed for %s: %v\n", resp.ID, err)
	}
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

		writeResponsesJob(w, job)
	}
}

// CancelResponseJob handles DELETE /v1/responses/{id}: cancels an in-progress
// background job upstream and updates its stored state.
func CancelResponseJob(r *router.Router, store storage.Storage) http.HandlerFunc {
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

		resp, err := rp.CancelResponse(ctx, id)
		if err != nil {
			model.WriteError(w, model.ErrProviderError(d.ProviderName, err))
			return
		}

		respJSON, err := json.Marshal(resp)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to marshal cancelled response: "+err.Error()))
			return
		}
		respJSONStr := string(respJSON)
		now := time.Now().UTC()
		if updErr := store.UpdateResponsesJob(ctx, id, resp.Status, &respJSONStr, nil, &now); updErr != nil {
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
