package middleware

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
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

// BrowserAuth selects the browser identity middleware at startup.
func BrowserAuth(store storage.Storage, sm *scs.SessionManager, local config.LocalAdminSettings) []func(http.Handler) http.Handler {
	if !local.Enabled {
		return []func(http.Handler) http.Handler{sm.LoadAndSave, RequireSession(store, sm)}
	}
	return []func(http.Handler) http.Handler{localAdminAuth(local.Name)}
}

func localAdminAuth(name string) func(http.Handler) http.Handler {
	user := &storage.User{ID: "local-admin", Name: name, IsAdmin: true}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer, err := netip.ParseAddrPort(r.RemoteAddr)
			host, _, splitErr := net.SplitHostPort(r.Host)
			if splitErr != nil {
				host = r.Host
			}
			localHost := strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1" || host == "[::1]"
			// Check the actual peer and Host to prevent remote access and DNS
			// rebinding. Browser requests with an Origin must be same-origin.
			origin := r.Header.Get("Origin")
			if err != nil || !peer.Addr().Unmap().IsLoopback() || !localHost ||
				r.Header.Get("Sec-Fetch-Site") == "cross-site" ||
				(origin != "" && !strings.EqualFold(origin, "http://"+r.Host)) {
				model.WriteError(w, model.ErrForbidden("local admin requires a loopback request from the same origin"))
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			ctx := context.WithValue(r.Context(), ContextKeyUser, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
