package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
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
		json.NewEncoder(w).Encode(resp)
	}
}
