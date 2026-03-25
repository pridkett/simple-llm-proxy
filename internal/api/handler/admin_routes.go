package handler

import (
	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// RegisterAdminRoutes registers identity CRUD routes (/admin/users, /admin/teams, /admin/applications)
// into the provided router group. This function is fully implemented by Plan 05.
// Plan 04 creates this stub so router.go compiles; Plan 05 replaces the body.
func RegisterAdminRoutes(r chi.Router, store storage.Storage) {
	// Stub: identity CRUD routes will be registered here by Plan 05
	_ = store
}
