package handler

import (
	"encoding/json"
	"net/http"

	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

// costMapModelItem is the per-model shape returned by AdminCostMapModels.
type costMapModelItem struct {
	Name                        string  `json:"name"`
	InputCostPerToken           float64 `json:"input_cost_per_token"`
	OutputCostPerToken          float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost     float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost float64 `json:"cache_creation_input_token_cost"`
	MaxTokens                   int     `json:"max_tokens"`
	MaxInputTokens              int     `json:"max_input_tokens"`
	MaxOutputTokens             int     `json:"max_output_tokens"`
	LiteLLMProvider             string  `json:"litellm_provider,omitempty"`
	Mode                        string  `json:"mode,omitempty"`
}

// AdminCostMapModels handles GET /admin/costmap/models.
// Returns the full list of model entries from the loaded cost map, sorted by name.
// Returns an empty array when the cost map has not been loaded yet.
func AdminCostMapModels(cm *costmap.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		entries := cm.ListModels()
		items := make([]costMapModelItem, len(entries))
		for i, e := range entries {
			items[i] = costMapModelItem{
				Name:                        e.Name,
				InputCostPerToken:           e.Spec.InputCostPerToken,
				OutputCostPerToken:          e.Spec.OutputCostPerToken,
				CacheReadInputTokenCost:     e.Spec.CacheReadInputTokenCost,
				CacheCreationInputTokenCost: e.Spec.CacheCreationInputTokenCost,
				MaxTokens:                   e.Spec.MaxTokens,
				MaxInputTokens:              e.Spec.MaxInputTokens,
				MaxOutputTokens:             e.Spec.MaxOutputTokens,
				LiteLLMProvider:             e.Spec.LiteLLMProvider,
				Mode:                        e.Spec.Mode,
			}
		}
		if items == nil {
			items = []costMapModelItem{} // return [] not null when empty
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items) //nolint:errcheck
	}
}

// AdminCostMapStatus handles GET /admin/costmap.
// Returns the current status of the LiteLLM cost map.
func AdminCostMapStatus(cm *costmap.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cm.Status())
	}
}

type costmapReloadResponse struct {
	Status     string `json:"status"`
	ModelCount int    `json:"model_count"`
}

// AdminCostMapReload handles POST /admin/costmap/reload.
// Fetches and caches the cost map from the configured source URL.
func AdminCostMapReload(cm *costmap.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if err := cm.Reload(req.Context()); err != nil {
			model.WriteError(w, model.ErrInternalServer("failed to reload cost map", err))
			return
		}
		s := cm.Status()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(costmapReloadResponse{
			Status:     "ok",
			ModelCount: s.ModelCount,
		})
	}
}

type costmapSetURLRequest struct {
	URL string `json:"url"`
}

type costmapURLResponse struct {
	URL string `json:"url"`
}

// AdminCostMapSetURL handles PUT /admin/costmap/url.
// Updates the source URL used for future cost map reloads.
func AdminCostMapSetURL(cm *costmap.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body costmapSetURLRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid request body"))
			return
		}
		if err := cm.SetURL(body.URL); err != nil {
			model.WriteError(w, model.ErrBadRequest(err.Error()))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(costmapURLResponse{URL: cm.GetURL()})
	}
}
