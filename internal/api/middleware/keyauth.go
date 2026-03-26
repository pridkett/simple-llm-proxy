package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

type contextKeyAPIKey struct{}

// APIKeyFromContext returns the CachedKey injected by KeyAuth middleware.
// Returns nil if the request was authenticated via master key or no key middleware was applied.
func APIKeyFromContext(ctx context.Context) *keystore.CachedKey {
	ck, _ := ctx.Value(contextKeyAPIKey{}).(*keystore.CachedKey)
	return ck
}

// KeyAuth replaces Auth() on /v1/*. It accepts:
//   - Master key (string match) — full bypass, no enforcement, no context injection.
//   - Per-app key (sk-app-... format) — validated via cache, rate limits and budget enforced.
//
// Per D-05: master key bypasses ALL enforcement (rate limits, budgets, model restrictions).
// Per D-06: CachedKey is injected into context so downstream handlers can read key config
// for model allowlist enforcement and cost attribution without extra DB calls.
func KeyAuth(masterKey string, store storage.Storage, cache *keystore.Cache, rl *keystore.RateLimiter, sa *keystore.SpendAccumulator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if token == "" {
				model.WriteError(w, model.ErrUnauthorized("missing Authorization header"))
				return
			}

			// Master key: bypass all enforcement (admin/testing credential)
			if masterKey != "" && token == masterKey {
				next.ServeHTTP(w, r)
				return
			}

			// Per-app key path
			ctx := r.Context()
			ck, err := cache.Get(ctx, token, store)
			if err != nil {
				model.WriteError(w, model.ErrInternal("key lookup failed"))
				return
			}
			if ck == nil {
				model.WriteError(w, model.ErrUnauthorized("invalid API key"))
				return
			}
			if !ck.Key.IsActive {
				model.WriteError(w, model.ErrUnauthorized("API key has been revoked"))
				return
			}

			// Rate limit checks (RPM then RPD)
			if ck.Key.MaxRPM != nil {
				if !rl.CheckAndIncrementRPM(ck.Key.ID, *ck.Key.MaxRPM) {
					model.WriteError(w, model.ErrRateLimited("rate limit exceeded: requests per minute"))
					return
				}
			}
			if ck.Key.MaxRPD != nil {
				if !rl.CheckAndIncrementRPD(ck.Key.ID, *ck.Key.MaxRPD) {
					model.WriteError(w, model.ErrRateLimited("rate limit exceeded: requests per day"))
					return
				}
			}

			// Hard budget check
			if ck.Key.MaxBudget != nil {
				if sa.CurrentSpend(ck.Key.ID) >= *ck.Key.MaxBudget {
					model.WriteError(w, model.ErrBudgetExceeded("hard budget limit exceeded for this API key"))
					return
				}
			}

			// Inject key into context for downstream enforcement (model allowlist) and cost attribution
			ctx = context.WithValue(ctx, contextKeyAPIKey{}, ck)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
