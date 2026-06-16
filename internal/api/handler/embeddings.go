package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/webhook"
)

// Embeddings handles POST /v1/embeddings requests.
func Embeddings(r *router.Router, store storage.Storage, sa *keystore.SpendAccumulator, cm *costmap.Manager, dispatcher *webhook.WebhookDispatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		startTime := time.Now()

		// Phase 14: capture request body snippet from BodyCapture middleware context.
		// Embeddings handler does not receive cfg.BodySnippetLimit, so it always passes
		// a non-nil pointer. Empty string stored as '' (not NULL) when body is empty.
		// The NULL guarantee for limit=0 is enforced in chat.go via cfg.BodySnippetLimit.
		s := middleware.ReqBodySnippetFromContext(req.Context())
		reqBodySnippet := &s

		var embReq model.EmbeddingsRequest
		if err := json.NewDecoder(req.Body).Decode(&embReq); err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid request body: "+err.Error()))
			return
		}

		if embReq.Model == "" {
			model.WriteError(w, model.ErrBadRequest("model is required"))
			return
		}

		if embReq.Input == nil {
			model.WriteError(w, model.ErrBadRequest("input is required"))
			return
		}

		// Model allowlist enforcement (per D-10, KEY-02).
		// Check is done here (not middleware) because it requires the decoded model name.
		ck := middleware.APIKeyFromContext(ctx)
		if ck != nil && len(ck.AllowedModels) > 0 {
			allowed := false
			for _, m := range ck.AllowedModels {
				if m == embReq.Model {
					allowed = true
					break
				}
			}
			if !allowed {
				model.WriteError(w, model.ErrForbidden("model not allowed: "+embReq.Model))
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

		// Route() owns all retry/failover logic. Use a closure variable to
		// capture the embeddings response since RouteCallback returns
		// ChatCompletionResponse (not EmbeddingsResponse).
		var embResp *model.EmbeddingsResponse
		result := r.Route(ctx, embReq.Model, stickyKey, func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
			if !d.Provider.SupportsEmbeddings() {
				return nil, nil, fmt.Errorf("provider does not support embeddings")
			}
			providerReq := embReq
			providerReq.Model = d.ActualModel
			resp, err := d.Provider.Embeddings(ctx, &providerReq)
			if err != nil {
				return nil, nil, err
			}
			embResp = resp
			return nil, nil, nil // signal success via nil error
		})

		// Emit routing events to webhook dispatcher.
		requestID := middleware.RequestIDFromContext(ctx)
		emitRoutingEvents(dispatcher, r, result, embReq.Model, requestID)

		if result.Error != nil {
			// Check for budget exhaustion specifically (BUDGET-04).
			for _, reason := range result.FailoverReasons {
				if reason == router.FailoverBudgetExhausted {
					model.WriteError(w, model.ErrBudgetExceeded("budget exhausted for all available pools"))
					return
				}
			}
			if len(result.DeploymentsTried) == 0 {
				model.WriteError(w, model.ErrModelNotFound(embReq.Model))
			} else {
				model.WriteError(w, model.ErrProviderError("all providers", result.Error))
			}
			return
		}

		budget := r.BudgetManager()
		r.ReportSuccess(result.DeploymentUsed)

		// Log the request if storage is available
		if store != nil && embResp != nil && embResp.Usage != nil {
			go logRequest(logRequestParams{
				Store:           store,
				SpendAcc:        sa,
				CostMap:         cm,
				Budget:          budget,
				PoolName:        result.PoolName,
				APIKeyID:        apiKeyID,
				Deployment:      result.DeploymentUsed,
				Endpoint:        "/v1/embeddings",
				Usage:           embResp.Usage,
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
		json.NewEncoder(w).Encode(embResp)
	}
}
