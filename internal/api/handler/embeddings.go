package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// Embeddings handles POST /v1/embeddings requests.
func Embeddings(r *router.Router, store storage.Storage, sa *keystore.SpendAccumulator) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		startTime := time.Now()

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

		// Try to get a deployment with retries
		tried := make(map[*provider.Deployment]bool)
		var lastErr error

		for attempt := 0; attempt <= r.NumRetries(); attempt++ {
			deployment, err := r.GetDeploymentWithRetry(embReq.Model, tried)
			if err != nil {
				if attempt == 0 {
					model.WriteError(w, model.ErrModelNotFound(embReq.Model))
					return
				}
				break
			}
			tried[deployment] = true

			// Check if provider supports embeddings
			if !deployment.Provider.SupportsEmbeddings() {
				model.WriteError(w, model.ErrBadRequest("provider does not support embeddings"))
				return
			}

			// Create request with actual model name
			providerReq := embReq
			providerReq.Model = deployment.ActualModel

			resp, err := deployment.Provider.Embeddings(ctx, &providerReq)
			if err != nil {
				lastErr = err
				r.ReportFailure(deployment)
				continue
			}

			r.ReportSuccess(deployment)

			// Log the request if storage is available
			if store != nil && resp.Usage != nil {
				go logRequest(store, sa, apiKeyID, deployment, "/v1/embeddings", resp.Usage, http.StatusOK, startTime)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		model.WriteError(w, model.ErrProviderError("all providers", lastErr))
	}
}
