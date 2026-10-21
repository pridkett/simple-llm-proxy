package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestMessagesAuthenticationAndLimits(t *testing.T) {
	for _, test := range []struct {
		name, path, key, bearer string
		limit                   *int
		budget                  *float64
		active                  bool
		status                  int
		errorType               string
	}{
		{"sdk key", "/v1/messages", "app", "", nil, nil, true, 200, ""},
		{"master", "/v1/messages", "master", "", nil, nil, true, 200, ""},
		{"authorization precedence", "/v1/messages", "master", "bad", nil, nil, true, 401, "authentication_error"},
		{"revoked", "/v1/messages", "app", "", nil, nil, false, 401, "authentication_error"},
		{"rpm", "/v1/messages", "app", "", intPtr(1), nil, true, 429, "rate_limit_error"},
		{"budget", "/v1/messages", "app", "", nil, float64Ptr(0), true, 429, "rate_limit_error"},
		{"scope", "/v1/chat/completions", "master", "", nil, nil, true, 401, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, cache, rl, sa := newTestComponents(map[string]*storage.APIKey{computeSHA256Hex("app"): {ID: 1, IsActive: test.active, MaxRPM: test.limit, MaxBudget: test.budget}}, nil)
			if test.limit != nil {
				rl.CheckAndIncrementRPM(1, *test.limit)
			}
			handler := RequestID()(KeyAuth("master", store, cache, rl, sa)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })))
			req := httptest.NewRequest("POST", test.path, nil)
			req.Header.Set("x-api-key", test.key)
			if test.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+test.bearer)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != test.status || (test.errorType != "" && !strings.Contains(rr.Body.String(), `"type":"`+test.errorType+`"`)) {
				t.Fatal(rr.Code, rr.Body.String())
			}
		})
	}
}
