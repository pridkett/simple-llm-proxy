package middleware

import (
	"log"
	"net/http"
	"runtime/debug"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

// Recovery returns middleware that recovers from panics.
func Recovery() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					log.Printf("panic recovered: %v\n%s", err, debug.Stack())
					model.WriteError(w, model.ErrInternalServer("internal server error", nil))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
