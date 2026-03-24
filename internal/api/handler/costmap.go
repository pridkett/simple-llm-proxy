package handler

import (
	"encoding/json"
	"net/http"

	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

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
