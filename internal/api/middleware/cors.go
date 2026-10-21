package middleware

import "net/http"

// CORS returns middleware that adds CORS headers for the frontend dev server.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	originSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (len(originSet) == 0 || originSet[origin]) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, X-LLM-Include-Accounting")
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-LLM-Accounting-ID, X-LLM-Accounting-State, X-LLM-Accounting-Coverage, X-LLM-Cost-USD, X-LLM-Cost-Source, x-litellm-response-cost, x-litellm-total-tokens")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
