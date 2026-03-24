package api

import (
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pwagstro/simple_llm_proxy/internal/api/handler"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/openapi"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// NewRouter creates a new HTTP router with all routes configured.
func NewRouter(r *router.Router, store storage.Storage, reloader *config.Reloader, cm *costmap.Manager, startTime time.Time, spec *openapi.Spec) *chi.Mux {
	mux := chi.NewRouter()

	// Global middleware
	mux.Use(middleware.Recovery())
	mux.Use(middleware.Logging())
	mux.Use(middleware.CORS([]string{
		"http://localhost:5173",
		"http://localhost:5174",
		"http://127.0.0.1:5173",
		"http://127.0.0.1:5174",
	}))

	// Public routes
	mux.Get("/health", handler.Health())
	mux.Get("/openapi.json", handler.OpenAPI(spec))

	// Protected routes
	mux.Group(func(mux chi.Router) {
		// Auth uses the master key from the initial config load.
		// Changing master_key requires a server restart.
		mux.Use(middleware.Auth(reloader.Config().GeneralSettings.MasterKey))

		// OpenAI-compatible endpoints
		mux.Post("/v1/chat/completions", handler.ChatCompletions(r, store))
		mux.Post("/v1/completions", handler.Completions())
		mux.Post("/v1/embeddings", handler.Embeddings(r, store))
		mux.Get("/v1/models", handler.Models(r))
		mux.Get("/v1/models/{model}", handler.ModelDetail(r, cm))
		mux.Patch("/v1/models/{model}/cost_map_key", handler.PatchModelMapping(cm, store))
		mux.Patch("/v1/models/{model}/costs", handler.PatchModelCosts(cm, store))

		// Admin endpoints
		mux.Get("/admin/status", handler.AdminStatus(r, startTime))
		mux.Get("/admin/config", handler.AdminConfig(reloader.Config))
		mux.Post("/admin/reload", handler.AdminReload(reloader, r))
		mux.Get("/admin/logs", handler.AdminLogs(store))

		// Cost map endpoints
		mux.Get("/admin/costmap", handler.AdminCostMapStatus(cm))
		mux.Get("/admin/costmap/models", handler.AdminCostMapModels(cm))
		mux.Post("/admin/costmap/reload", handler.AdminCostMapReload(cm))
		mux.Put("/admin/costmap/url", handler.AdminCostMapSetURL(cm))
	})

	return mux
}
