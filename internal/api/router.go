package api

import (
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/pwagstro/simple_llm_proxy/internal/api/handler"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/auth"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/openapi"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/webhook"
)

// NewRouter creates a new HTTP router with all routes configured.
// sm is the SCS session manager (must not be nil).
// oidcProvider may be nil when OIDC is not configured — auth routes will return 503.
// cache, rl, sa are the keystore enforcement objects created at startup.
func NewRouter(r *router.Router, store storage.Storage, reloader *config.Reloader, cm *costmap.Manager, startTime time.Time, spec *openapi.Spec, sm *scs.SessionManager, oidcProvider *auth.OIDCProvider, cache *keystore.Cache, rl *keystore.RateLimiter, sa *keystore.SpendAccumulator, dispatcher *webhook.WebhookDispatcher) *chi.Mux {
	mux := chi.NewRouter()

	// Global middleware
	mux.Use(middleware.Recovery())
	mux.Use(middleware.RequestID())
	mux.Use(middleware.Logging())
	mux.Use(middleware.CORS([]string{
		"http://localhost:5173",
		"http://localhost:5174",
		"http://127.0.0.1:5173",
		"http://127.0.0.1:5174",
	}))

	// Public routes — no auth required
	mux.Get("/health", handler.Health())
	mux.Get("/openapi.json", handler.OpenAPI(spec))

	// OIDC auth routes — public (no session required; login initiates the flow)
	// These are wrapped in sm.LoadAndSave so session cookies are processed on callback.
	mux.Group(func(mux chi.Router) {
		mux.Use(sm.LoadAndSave)
		mux.Get("/auth/login", handler.AuthLogin(oidcProvider))
		mux.Get("/auth/callback", handler.AuthCallback(oidcProvider, store, sm))
		mux.Post("/auth/logout", handler.AuthLogout(sm))
		mux.Get("/admin/me", handler.AdminMe(store, sm))
	})

	// Group 1: /v1/* — machine clients, KeyAuth replaces old Auth() middleware.
	// KeyAuth accepts both master key (bypass) and per-app keys (enforcement).
	mux.Group(func(mux chi.Router) {
		mux.Use(middleware.KeyAuth(reloader.Config().GeneralSettings.MasterKey, store, cache, rl, sa))

		// OpenAI-compatible endpoints
		mux.Post("/v1/chat/completions", handler.ChatCompletions(r, store, sa, cm, dispatcher))
		mux.Post("/v1/completions", handler.Completions())
		mux.Post("/v1/embeddings", handler.Embeddings(r, store, sa, cm, dispatcher))
		mux.Get("/v1/models", handler.Models(r))
		mux.Get("/v1/models/{model}", handler.ModelDetail(r, cm))
		mux.Patch("/v1/models/{model}/cost_map_key", handler.PatchModelMapping(cm, store))
		mux.Patch("/v1/models/{model}/costs", handler.PatchModelCosts(cm, store))
		mux.Delete("/v1/models/{model}/costs", handler.DeleteModelCosts(cm, store))
	})

	// Group 2: /admin/* — browser clients, session auth
	mux.Group(func(mux chi.Router) {
		mux.Use(sm.LoadAndSave)
		mux.Use(middleware.RequireSession(store, sm))

		mux.Get("/admin/status", handler.AdminStatus(r, startTime))
		mux.Get("/admin/config", handler.AdminConfig(reloader.Config))
		mux.Post("/admin/reload", handler.AdminReload(reloader, r))
		mux.Get("/admin/logs", handler.AdminLogs(store))

		// Cost map endpoints
		mux.Get("/admin/costmap", handler.AdminCostMapStatus(cm))
		mux.Get("/admin/costmap/models", handler.AdminCostMapModels(cm))
		mux.Post("/admin/costmap/reload", handler.AdminCostMapReload(cm))
		mux.Put("/admin/costmap/url", handler.AdminCostMapSetURL(cm))

		// Model endpoints mirrored for session-auth browser clients
		mux.Get("/admin/models", handler.Models(r))
		mux.Get("/admin/models/{model}", handler.ModelDetail(r, cm))
		mux.Post("/admin/chat/completions", handler.ChatCompletions(r, store, sa, cm, dispatcher))
		mux.Post("/admin/embeddings", handler.Embeddings(r, store, sa, cm, dispatcher))

		// Identity and key management CRUD routes
		handler.RegisterAdminRoutes(mux, store, cache, reloader.Config)
	})

	return mux
}
