package handler

import (
	"encoding/json"
	"net/http"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// AdminUsers handles GET /admin/users — returns all authenticated users.
// Admin only.
func AdminUsers(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := middleware.UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			model.WriteError(w, model.ErrForbidden("admin required"))
			return
		}
		users, err := store.ListUsers(r.Context())
		if err != nil {
			model.WriteError(w, model.ErrInternal("failed to list users"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
	}
}
