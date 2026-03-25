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

// AdminApplications handles GET /admin/applications?team_id=N — returns all applications for a team.
// Any authenticated user may call this.
func AdminApplications(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamIDStr := r.URL.Query().Get("team_id")
		if teamIDStr == "" {
			model.WriteError(w, model.ErrBadRequest("team_id query parameter is required"))
			return
		}
		teamID, err := strconv.ParseInt(teamIDStr, 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid team_id"))
			return
		}
		apps, err := store.ListApplications(r.Context(), teamID)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to list applications"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(apps)
	}
}

// AdminCreateApplication handles POST /admin/applications — creates a new application.
// Admin only.
func AdminCreateApplication(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		var body struct {
			TeamID int64  `json:"team_id"`
			Name   string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.TeamID == 0 {
			model.WriteError(w, model.ErrBadRequest("team_id and name are required"))
			return
		}
		app, err := store.CreateApplication(r.Context(), body.TeamID, body.Name)
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to create application"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(app)
	}
}

// AdminDeleteApplication handles DELETE /admin/applications/{id} — deletes an application.
// Admin only.
func AdminDeleteApplication(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest("invalid application id"))
			return
		}
		if err := store.DeleteApplication(r.Context(), id); err != nil {
			model.WriteError(w, model.ErrInternal("failed to delete application"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
