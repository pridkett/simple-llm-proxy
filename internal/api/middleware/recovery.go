package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/rs/zerolog/log"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

// Recovery returns middleware that recovers from panics.
func Recovery() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					log.Error().
						Interface("panic", err).
						Str("stack", string(debug.Stack())).
						Msg("panic recovered")
					model.WriteError(w, model.ErrInternalServer("internal server error", nil))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
