package middleware

import (
	"context"
	"net/http"
	"strings"

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
// For API callers (Accept: application/json or XHR), returns 401 JSON on missing/invalid session.
// For browser navigation, returns 302 redirect to /login.
func RequireSession(store storage.Storage, sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := sm.GetString(r.Context(), "user_id")
			if userID == "" {
				respondAuthRequired(w, r)
				return
			}
			user, err := store.GetUser(r.Context(), userID)
			if err != nil || user == nil {
				sm.Destroy(r.Context())
				respondAuthRequired(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), ContextKeyUser, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func respondAuthRequired(w http.ResponseWriter, r *http.Request) {
	acceptsJSON := strings.Contains(r.Header.Get("Accept"), "application/json") ||
		strings.Contains(r.Header.Get("Content-Type"), "application/json") ||
		r.Header.Get("X-Requested-With") == "XMLHttpRequest"
	if acceptsJSON {
		model.WriteError(w, model.ErrUnauthorized("authentication required"))
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
