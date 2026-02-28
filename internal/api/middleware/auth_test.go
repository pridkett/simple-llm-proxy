package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuth(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		masterKey  string
		authHeader string
		wantStatus int
	}{
		{
			name:       "no master key configured",
			masterKey:  "",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid bearer token",
			masterKey:  "secret",
			authHeader: "Bearer secret",
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid token without bearer prefix",
			masterKey:  "secret",
			authHeader: "secret",
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing auth header",
			masterKey:  "secret",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid token",
			masterKey:  "secret",
			authHeader: "Bearer wrong",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			Auth(tt.masterKey)(handler).ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}
