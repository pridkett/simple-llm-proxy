package middleware

import (
	"net/http"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

// Auth returns middleware that validates the master key.
func Auth(masterKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth if no master key is configured
			if masterKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				model.WriteError(w, model.ErrUnauthorized("missing Authorization header"))
				return
			}

			// Support both "Bearer token" and just "token"
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token != masterKey {
				model.WriteError(w, model.ErrUnauthorized("invalid API key"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
