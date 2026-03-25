package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// Models handles GET /v1/models requests.
func Models(r *router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		models := r.ListModels()

		data := make([]model.ModelInfo, len(models))
		for i, name := range models {
			data[i] = model.ModelInfo{
				ID:      name,
				Object:  "model",
				Created: time.Now().Unix(),
				OwnedBy: "simple-llm-proxy",
			}
		}

		resp := model.ModelsResponse{
			Object: "list",
			Data:   data,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// ModelDetail handles GET /v1/models/{model} requests.
// Returns model detail with cost information from the cost map when available.
func ModelDetail(r *router.Router, cm *costmap.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		modelName := chi.URLParam(req, "model")

		// Find the model in the router's current status.
		var found *router.ModelStatusInfo
		for _, s := range r.GetStatus() {
			s := s
			if s.ModelName == modelName {
				found = &s
				break
			}
		}
		if found == nil {
			model.WriteError(w, model.ErrModelNotFound(modelName))
			return
		}

		// Build candidate actual model strings for cost map lookup.
		// Try "provider/actual_model" first (canonical LiteLLM format), then bare "actual_model".
		var candidates []string
		seen := make(map[string]bool)
		for _, dep := range found.Deployments {
			if dep.ProviderName != "" && dep.ActualModel != "" {
				k := dep.ProviderName + "/" + dep.ActualModel
				if !seen[k] {
					candidates = append(candidates, k)
					seen[k] = true
				}
			}
			if dep.ActualModel != "" && !seen[dep.ActualModel] {
				candidates = append(candidates, dep.ActualModel)
				seen[dep.ActualModel] = true
			}
		}

		resp := buildModelDetailResponse(modelName, cm, candidates)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// PatchModelMapping handles PATCH /v1/models/{model}/cost_map_key.
// Sets a cost map key override for the given proxy model name.
// The model need not exist in the router (supports novel/unmapped models per ADR 002).
func PatchModelMapping(cm *costmap.Manager, store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		modelName := chi.URLParam(req, "model")

		var body struct {
			CostMapKey string `json:"cost_map_key"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			model.WriteError(w, model.ErrBadRequest("Request body must be valid JSON: "+err.Error()))
			return
		}
		if body.CostMapKey == "" {
			model.WriteError(w, model.ErrBadRequest("cost_map_key must not be empty"))
			return
		}

		if err := store.UpsertCostMapKey(req.Context(), modelName, body.CostMapKey); err != nil {
			model.WriteError(w, model.ErrInternalServer("Failed to save cost map key", err))
			return
		}
		cm.SetOverrideKey(modelName, body.CostMapKey)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
			"model":        modelName,
			"cost_map_key": body.CostMapKey,
		})
	}
}

// PatchModelCosts handles PATCH /v1/models/{model}/costs.
// Sets a fully custom cost spec for the given proxy model name.
// The model need not exist in the router (supports novel/unmapped models per ADR 002).
func PatchModelCosts(cm *costmap.Manager, store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		modelName := chi.URLParam(req, "model")

		var spec costmap.ModelSpec
		if err := json.NewDecoder(req.Body).Decode(&spec); err != nil {
			model.WriteError(w, model.ErrBadRequest("Request body must be valid JSON: "+err.Error()))
			return
		}

		specJSON, err := json.Marshal(spec)
		if err != nil {
			model.WriteError(w, model.ErrInternalServer("Failed to encode cost spec", err))
			return
		}
		if err := store.UpsertCustomCostSpec(req.Context(), modelName, string(specJSON)); err != nil {
			model.WriteError(w, model.ErrInternalServer("Failed to save custom cost spec", err))
			return
		}
		cm.SetCustomSpec(modelName, spec)

		resp := buildModelDetailResponse(modelName, cm, nil)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// DeleteModelCosts handles DELETE /v1/models/{model}/costs.
// Removes any cost override (key or custom spec) for the given proxy model name,
// reverting to auto-detection behaviour.
func DeleteModelCosts(cm *costmap.Manager, store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		modelName := chi.URLParam(req, "model")

		if err := store.DeleteCostOverride(req.Context(), modelName); err != nil {
			model.WriteError(w, model.ErrInternalServer("Failed to delete cost override", err))
			return
		}
		cm.ClearOverride(modelName)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"model": modelName, "status": "cleared"}) //nolint:errcheck
	}
}

// buildModelDetailResponse constructs a ModelDetailResponse for the given model name,
// resolving cost data from the cost map using the provided candidate actual model strings.
func buildModelDetailResponse(modelName string, cm *costmap.Manager, candidates []string) model.ModelDetailResponse {
	result := cm.GetEffectiveSpec(modelName, candidates)
	costs := model.CostsInfo{}
	if result.Found {
		s := result.Spec
		costs = model.CostsInfo{
			MaxTokens:                       s.MaxTokens,
			MaxInputTokens:                  s.MaxInputTokens,
			MaxOutputTokens:                 s.MaxOutputTokens,
			InputCostPerToken:               s.InputCostPerToken,
			OutputCostPerToken:              s.OutputCostPerToken,
			CacheReadInputTokenCost:         s.CacheReadInputTokenCost,
			CacheCreationInputTokenCost:     s.CacheCreationInputTokenCost,
			LiteLLMProvider:                 s.LiteLLMProvider,
			Mode:                            s.Mode,
			SupportsFunctionCalling:         s.SupportsFunctionCalling,
			SupportsParallelFunctionCalling: s.SupportsParallelFunctionCalling,
			SupportsVision:                  s.SupportsVision,
			Source:                          result.Source,
			CostMapKey:                      result.Key,
		}
	}
	return model.ModelDetailResponse{
		ID:      modelName,
		Object:  "model",
		Created: time.Now().Unix(),
		OwnedBy: "simple-llm-proxy",
		Costs:   costs,
	}
}
