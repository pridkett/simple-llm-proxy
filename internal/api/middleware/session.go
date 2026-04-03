package middleware

import (
	"context"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

type contextKey string

// ContextKeyUser is the context key for the authenticated *storage.User.
const ContextKeyUser contextKey = "user"

// UserFromContext extracts the authenticated user from the request context.
// Returns nil if no user is present.
func UserFromContext(ctx context.Context) *storage.User {
	u, _ := ctx.Value(ContextKeyUser).(*storage.User)
	return u
}

// RequireSession validates the SCS session and injects the user into context.
// All unauthenticated requests receive a 401 JSON response.
//
// This middleware is only applied to /admin/* routes (see router.go), which are
// exclusively called by the Vue SPA via JavaScript fetch(). The SPA uses hash-
// based routing (createWebHashHistory), so browsers never navigate directly to
// /admin/* paths. Therefore header-based detection of "browser vs API caller"
// is unnecessary — every request here is an API call.
func RequireSession(store storage.Storage, sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := sm.GetString(r.Context(), "user_id")
			if userID == "" {
				model.WriteError(w, model.ErrUnauthorized("authentication required"))
				return
			}
			user, err := store.GetUser(r.Context(), userID)
			if err != nil || user == nil {
				sm.Destroy(r.Context())
				model.WriteError(w, model.ErrUnauthorized("authentication required"))
				return
			}
			ctx := context.WithValue(r.Context(), ContextKeyUser, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
