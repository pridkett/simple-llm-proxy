package handler

import (
	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// RegisterAdminRoutes registers all identity CRUD routes into the provided chi.Router group.
// The group is expected to already have session middleware applied (sm.LoadAndSave + RequireSession).
// Called from internal/api/router.go's /admin/* group setup.
func RegisterAdminRoutes(r chi.Router, store storage.Storage, cache *keystore.Cache, getCfg func() *config.Config) {
	r.Get("/admin/users", AdminUsers(store))
	r.Get("/admin/teams", AdminTeams(store))
	r.Post("/admin/teams", AdminCreateTeam(store))
	r.Delete("/admin/teams/{id}", AdminDeleteTeam(store))
	r.Get("/admin/teams/mine", AdminMyTeams(store))
	r.Get("/admin/teams/{id}/members", AdminTeamMembers(store))
	r.Put("/admin/teams/{id}/members", AdminAddTeamMember(store))
	r.Delete("/admin/teams/{id}/members/{user_id}", AdminRemoveTeamMember(store))
	r.Patch("/admin/teams/{id}/members/{user_id}", AdminUpdateTeamMemberRole(store))
	r.Get("/admin/applications", AdminApplications(store))
	r.Post("/admin/applications", AdminCreateApplication(store))
	r.Delete("/admin/applications/{id}", AdminDeleteApplication(store))

	// Key management routes (Phase 2)
	r.Get("/admin/keys/mine", AdminMyKeys(store))
	r.Get("/admin/applications/{id}/keys", AdminListKeys(store))
	r.Post("/admin/applications/{id}/keys", AdminCreateKey(store))
	r.Delete("/admin/api-keys/{id}", AdminRevokeKey(store, cache))
	r.Patch("/admin/api-keys/{id}", AdminUpdateKey(store, cache))

	// Cost/spend routes (Phase 3)
	// Authorization: /admin/spend exposes deployment-wide spend data.
	// Access is restricted to admin users. The route group applies RequireSession middleware
	// (sm.LoadAndSave + RequireSession) which handles 401 for unauthenticated requests.
	// Admin-only enforcement (403 for non-admin authenticated users) is done per-handler
	// via middleware.UserFromContext — the same pattern used by all admin-only handlers.
	r.Get("/admin/spend", AdminSpend(store))

	// Webhook management (Phase 9)
	r.Get("/admin/webhooks", AdminListWebhooks(store, getCfg))
	r.Post("/admin/webhooks", AdminCreateWebhook(store))
	r.Put("/admin/webhooks/{id}", AdminUpdateWebhook(store))
	r.Delete("/admin/webhooks/{id}", AdminDeleteWebhook(store))

	// Notification events feed (Phase 9)
	r.Get("/admin/events", AdminEvents(store))
}
