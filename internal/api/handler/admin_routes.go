package handler

import (
	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// RegisterAdminRoutes registers all identity CRUD routes into the provided chi.Router group.
// The group is expected to already have session middleware applied (sm.LoadAndSave + RequireSession).
// Called from internal/api/router.go's /admin/* group setup by Plan 04.
func RegisterAdminRoutes(r chi.Router, store storage.Storage) {
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
}
