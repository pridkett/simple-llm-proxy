package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// AdminTeams handles GET /admin/teams — returns all teams.
// Any authenticated user may call this.
func AdminTeams(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teams, err := store.ListTeams(r.Context())
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to list teams"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(teams)
	}
}

// AdminCreateTeam handles POST /admin/teams — creates a new team.
// Admin only.
func AdminCreateTeam(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			model.WriteError(w, model.ErrBadRequest("name is required"))
			return
		}
		team, err := store.CreateTeam(r.Context(), body.Name)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to create team"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(team)
	}
}

// AdminDeleteTeam handles DELETE /admin/teams/{id} — deletes a team.
// Admin only.
func AdminDeleteTeam(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid team id"))
			return
		}
		if err := store.DeleteTeam(r.Context(), id); err != nil {
			model.WriteError(w, model.ErrInternal("failed to delete team"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// AdminTeamMembers handles GET /admin/teams/{id}/members — lists team members.
// Admin only.
func AdminTeamMembers(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid team id"))
			return
		}
		members, err := store.ListTeamMembers(r.Context(), id)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to list team members"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(members)
	}
}

// AdminAddTeamMember handles PUT /admin/teams/{id}/members — adds a user to a team.
// Admin only.
func AdminAddTeamMember(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		idStr := chi.URLParam(r, "id")
		teamID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid team id"))
			return
		}
		var body struct {
			UserID string `json:"user_id"`
			Role   string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.UserID == "" || body.Role == "" {
			model.WriteError(w, model.ErrBadRequest("user_id and role are required"))
			return
		}
		if err := store.AddTeamMember(r.Context(), teamID, body.UserID, body.Role); err != nil {
			model.WriteError(w, model.ErrInternal("failed to add team member"))
			return
		}
		w.WriteHeader(http.StatusCreated)
	}
}

// AdminRemoveTeamMember handles DELETE /admin/teams/{id}/members/{user_id} — removes a user from a team.
// Admin only.
func AdminRemoveTeamMember(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		idStr := chi.URLParam(r, "id")
		teamID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid team id"))
			return
		}
		userID := chi.URLParam(r, "user_id")
		if userID == "" {
			model.WriteError(w, model.ErrBadRequest("user_id is required"))
			return
		}
		if err := store.RemoveTeamMember(r.Context(), teamID, userID); err != nil {
			model.WriteError(w, model.ErrInternal("failed to remove team member"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// AdminUpdateTeamMemberRole handles PATCH /admin/teams/{id}/members/{user_id} — updates a user's role.
// Admin only.
func AdminUpdateTeamMemberRole(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		idStr := chi.URLParam(r, "id")
		teamID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid team id"))
			return
		}
		userID := chi.URLParam(r, "user_id")
		if userID == "" {
			model.WriteError(w, model.ErrBadRequest("user_id is required"))
			return
		}
		var body struct {
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Role == "" {
			model.WriteError(w, model.ErrBadRequest("role is required"))
			return
		}
		if err := store.UpdateTeamMemberRole(r.Context(), teamID, userID, body.Role); err != nil {
			model.WriteError(w, model.ErrInternal("failed to update team member role"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// AdminMyTeams handles GET /admin/teams/mine — returns teams the session user belongs to.
// Any authenticated user may call this (uses session user).
func AdminMyTeams(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil {
			model.WriteError(w, model.ErrForbidden("authentication required"))
			return
		}
		myTeams, err := store.ListMyTeams(r.Context(), user.ID)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to list teams"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(myTeams)
	}
}
