package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/pwagstro/simple_llm_proxy/internal/api"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/openapi"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"
)

func TestLocalAdminBrowserRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("local_admin: {enabled: true, name: Dev Admin}\ngeneral_settings: {master_key: test-key}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewReloader(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.New(filepath.Join(dir, "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	models, err := router.New(cfg.Config(), store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(models.Close)
	routes := api.NewRouter(models, store, cfg, nil, time.Now(), openapi.New(), scs.New(), nil,
		keystore.New(time.Minute), keystore.NewRateLimiter(), keystore.NewSpendAccumulator(), nil)

	request := func(method, host, path, origin, peer, body string) *http.Request {
		req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		req.RemoteAddr = peer
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		return req
	}
	const localhost = "127.0.0.1:5173"
	const localPeer = "127.0.0.1:12345"
	profile := httptest.NewRecorder()
	routes.ServeHTTP(profile, request("GET", localhost, "/admin/me", "", localPeer, ""))
	var user struct {
		Name       string `json:"name"`
		IsAdmin    bool   `json:"is_admin"`
		LocalAdmin bool   `json:"local_admin"`
	}
	if err := json.Unmarshal(profile.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	if profile.Code != http.StatusOK || user.Name != "Dev Admin" || !user.IsAdmin || !user.LocalAdmin || profile.Header().Get("Set-Cookie") != "" {
		t.Fatalf("local admin profile: %d %s", profile.Code, profile.Body)
	}

	for _, tc := range []struct {
		name string
		req  *http.Request
		want int
	}{
		{"admin write through Vite", request("POST", localhost, "/admin/teams", "http://"+localhost, localPeer, `{"name":"Development"}`), http.StatusCreated},
		{"MCP connections", request("GET", localhost, "/api/mcp/connections/", "", localPeer, ""), http.StatusOK},
		{"model API still requires a key", request("GET", localhost, "/v1/models", "", localPeer, ""), http.StatusUnauthorized},
		{"DNS rebinding", request("GET", "example.test:8080", "/admin/me", "", localPeer, ""), http.StatusForbidden},
		{"cross-origin write", request("POST", localhost, "/admin/teams", "https://example.test", localPeer, `{"name":"Cross origin"}`), http.StatusForbidden},
		{"different localhost port", request("POST", localhost, "/admin/teams", "http://127.0.0.1:9090", localPeer, `{"name":"Cross port"}`), http.StatusForbidden},
		{"non-loopback peer", request("GET", localhost, "/admin/me", "", "192.0.2.1:12345", ""), http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.Header.Set("X-Forwarded-For", "127.0.0.1")
			rec := httptest.NewRecorder()
			routes.ServeHTTP(rec, tc.req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
}
