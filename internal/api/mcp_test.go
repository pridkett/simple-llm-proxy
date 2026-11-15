package api_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pwagstro/simple_llm_proxy/internal/api"
	"github.com/pwagstro/simple_llm_proxy/internal/auth"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/openapi"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"
	"golang.org/x/oauth2"
)

func mcpRoutes(t *testing.T, settings string, oidc *auth.OIDCProvider) (http.Handler, *sqlite.Storage) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("general_settings: {database_url: proxy.db, master_key: master-only}\n"+settings), 0600); err != nil {
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
	if err := store.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	models, err := router.New(cfg.Config(), store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(models.Close)
	return api.NewRouter(models, store, cfg, nil, time.Now(), openapi.New(), scs.New(), oidc,
		keystore.New(time.Minute), keystore.NewRateLimiter(), keystore.NewSpendAccumulator(), nil), store
}

type mcpBearerTransport string

func (token mcpBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(token))
	return http.DefaultTransport.RoundTrip(r)
}

func mcpSession(t *testing.T, endpoint, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "proxy-test", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true}
	if token != "" {
		transport.HTTPClient = &http.Client{Transport: mcpBearerTransport(token)}
	}
	session, err := client.Connect(t.Context(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func mcpCall(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any, output any) {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil || result.IsError {
		t.Fatalf("%s failed: %+v %v", name, result, err)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, output); err != nil {
		t.Fatal(err)
	}
}

func mcpHTTP(routes http.Handler, host, path, token, origin, peer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", host+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.RemoteAddr = peer
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	return rec
}

func TestManagementMCPLocal(t *testing.T) {
	routes, store := mcpRoutes(t, "local_admin: {enabled: true}\n", nil)
	team, err := store.CreateTeam(t.Context(), "Engineering")
	if err != nil {
		t.Fatal(err)
	}
	app, err := store.CreateApplication(t.Context(), team.ID, "Agent")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(routes)
	t.Cleanup(server.Close)
	session := mcpSession(t, server.URL+auth.MCPManagementPath, "")
	var teams struct{ Teams []storage.Team }
	mcpCall(t, session, "list_teams", nil, &teams)
	if len(teams.Teams) != 1 || teams.Teams[0].ID != team.ID {
		t.Fatalf("unexpected team list: %+v", teams)
	}
	var apps struct{ Applications []storage.Application }
	mcpCall(t, session, "list_applications", map[string]any{"team_id": team.ID}, &apps)
	if len(apps.Applications) != 1 || apps.Applications[0].ID != app.ID {
		t.Fatalf("unexpected app list: %+v", apps)
	}
	var key struct {
		ID  int64
		Key string
	}
	mcpCall(t, session, "create_key", map[string]any{"app_id": app.ID, "name": "Reports", "max_rpm": 10, "allowed_models": []string{"test"}}, &key)
	if key.ID <= 0 || !strings.HasPrefix(key.Key, "sk-app-") {
		t.Fatal("create_key did not return an app key")
	}
	var keys struct {
		Keys []struct {
			storage.APIKey
			Secret        string   `json:"key"`
			AllowedModels []string `json:"allowed_models"`
		}
	}
	mcpCall(t, session, "list_keys", map[string]any{"app_id": app.ID}, &keys)
	if len(keys.Keys) != 1 || keys.Keys[0].ID != key.ID || keys.Keys[0].Secret != "" ||
		keys.Keys[0].MaxRPM == nil || *keys.Keys[0].MaxRPM != 10 || !slices.Equal(keys.Keys[0].AllowedModels, []string{"test"}) {
		t.Fatal("list_keys returned unexpected key metadata")
	}
	stamp := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	for _, status := range []int{200, 429} {
		if err := store.LogRequest(t.Context(), &storage.RequestLog{RequestID: fmt.Sprintf("mcp-%d", status),
			Model: "test", Provider: "openai", Endpoint: "/v1/chat/completions", RequestTime: stamp,
			APIKeyID: &key.ID, StatusCode: status}); err != nil {
			t.Fatal(err)
		}
	}
	var usage struct {
		Summary storage.UsageMetrics
		Series  []storage.UsageAggregate
	}
	mcpCall(t, session, "query_usage", map[string]any{"from": "2026-11-01", "to": "2026-11-01",
		"team_id": team.ID, "key_id": key.ID, "group_by": "status", "status_code": 429}, &usage)
	if usage.Summary.Calls != 1 || usage.Summary.FailedCalls != 1 || len(usage.Series) != 1 || usage.Series[0].GroupID != "429" {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	for _, tc := range []struct{ host, peer, origin string }{
		{"http://other.test", "127.0.0.1:1234", ""},
		{"http://localhost", "192.0.2.1:1234", ""},
		{"http://localhost", "127.0.0.1:1234", "https://other.test"},
	} {
		if r := mcpHTTP(routes, tc.host, auth.MCPManagementPath, "", tc.origin, tc.peer); r.Code != 403 {
			t.Fatalf("unsafe local request accepted: %+v (%d)", tc, r.Code)
		}
	}
}

func TestManagementMCPBearer(t *testing.T) {
	read := "sk-proxy-mcp-" + strings.Repeat("a", 64)
	write := "sk-proxy-mcp-" + strings.Repeat("b", 64)
	t.Setenv("TEST_PROXY_MCP_READ", read)
	t.Setenv("TEST_PROXY_MCP_WRITE", write)
	routes, _ := mcpRoutes(t, `mcp:
  management:
    read_token: os.environ/TEST_PROXY_MCP_READ
    write_token: os.environ/TEST_PROXY_MCP_WRITE
`, nil)
	const host, peer = "https://proxy.example.test", "127.0.0.1:1234"
	for _, token := range []string{"", "master-only", "sk-app-" + strings.Repeat("c", 48), "sk-issuer-" + strings.Repeat("d", 64)} {
		r := mcpHTTP(routes, host, auth.MCPManagementPath, token, "", peer)
		if r.Code != 401 || !strings.HasPrefix(r.Header().Get("WWW-Authenticate"), "Bearer") {
			t.Fatalf("invalid or missing credential: %d", r.Code)
		}
	}
	if r := mcpHTTP(routes, host, auth.MCPManagementPath, write, "https://other.test", peer); r.Code != 403 {
		t.Fatalf("cross-origin request accepted: %d", r.Code)
	}
	server := httptest.NewServer(routes)
	t.Cleanup(server.Close)
	readSession := mcpSession(t, server.URL+auth.MCPManagementPath, read)
	result, err := readSession.CallTool(t.Context(), &mcp.CallToolParams{Name: "create_key", Arguments: map[string]any{"app_id": 1, "name": "Forbidden"}})
	if err == nil && (result == nil || !result.IsError) {
		t.Fatal("read credential can call create_key")
	}
	for _, tc := range []struct {
		token string
		count int
	}{{read, 4}, {write, 5}} {
		r := mcpHTTP(routes, host, auth.MCPManagementPath, tc.token, "", peer)
		var body struct {
			Result struct{ Tools []mcp.Tool }
		}
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &body) != nil || len(body.Result.Tools) != tc.count {
			t.Fatalf("unexpected tool list: %d %s", r.Code, r.Body)
		}
	}
}

func TestManagementMCPOAuth(t *testing.T) {
	const origin = "https://proxy.example.test"
	const resource = origin + auth.MCPManagementPath
	const peer = "127.0.0.1:1234"
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	idp := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "jwks_uri": issuer + "/jwks"})
		case "/jwks":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: pub, KeyID: "test", Algorithm: string(jose.EdDSA)}}})
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(idp.Close)
	issuer = idp.URL
	oidcContext := context.WithValue(t.Context(), oauth2.HTTPClient, idp.Client())
	callback := origin + "/auth/callback"
	provider, err := auth.NewOIDCProvider(oidcContext, issuer, "browser", "", callback, "admin")
	if err != nil {
		t.Fatal(err)
	}
	provider.MCP, err = auth.NewMCPOAuth(oidcContext, provider.Provider, issuer, callback)
	if err != nil {
		t.Fatal(err)
	}
	routes, _ := mcpRoutes(t, fmt.Sprintf("oidc_settings: {issuer_url: %s, client_id: browser, redirect_url: %s}\nmcp: {management: {oauth: true}}\n", issuer, callback), provider)
	meta := httptest.NewRecorder()
	routes.ServeHTTP(meta, httptest.NewRequest("GET", origin+auth.MCPMetadataPath, nil))
	var metadata auth.MCPResourceMetadata
	if meta.Code != 200 || json.Unmarshal(meta.Body.Bytes(), &metadata) != nil || metadata.Resource != resource ||
		!slices.Equal(metadata.AuthorizationServers, []string{issuer}) ||
		!slices.Equal(metadata.ScopesSupported, []string{auth.MCPReadScope, auth.MCPWriteScope}) {
		t.Fatalf("invalid discovery: %d %s", meta.Code, meta.Body)
	}
	unauthorized := mcpHTTP(routes, origin, auth.MCPManagementPath, "", origin, peer)
	if unauthorized.Code != 401 || !strings.Contains(unauthorized.Header().Get("WWW-Authenticate"), origin+auth.MCPMetadataPath) {
		t.Fatal("missing discovery challenge")
	}
	for _, tc := range []struct {
		name, typ, scope string
		audience         jwt.Audience
		expiry           time.Duration
		code             int
		canWrite         bool
	}{
		{"read", "at+jwt", auth.MCPReadScope, jwt.Audience{resource, issuer}, time.Hour, 200, false},
		{"write", "at+jwt", auth.MCPReadScope + " " + auth.MCPWriteScope, jwt.Audience{resource}, time.Hour, 200, true},
		{"write only", "at+jwt", auth.MCPWriteScope, jwt.Audience{resource}, time.Hour, 403, false},
		{"id token", "JWT", auth.MCPReadScope, jwt.Audience{resource}, time.Hour, 401, false},
		{"wrong audience", "at+jwt", auth.MCPReadScope, jwt.Audience{resource + "/other"}, time.Hour, 401, false},
		{"additional resource", "at+jwt", auth.MCPReadScope, jwt.Audience{resource, resource + "/other"}, time.Hour, 401, false},
		{"expired", "at+jwt", auth.MCPReadScope, jwt.Audience{resource}, -time.Hour, 401, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: priv}, new(jose.SignerOptions).WithType(jose.ContentType(tc.typ)).WithHeader("kid", "test"))
			if err != nil {
				t.Fatal(err)
			}
			token, err := jwt.Signed(signer).Claims(jwt.Claims{Issuer: issuer, Subject: "user", Audience: tc.audience,
				Expiry: jwt.NewNumericDate(time.Now().Add(tc.expiry))}).Claims(map[string]any{"scope": tc.scope}).Serialize()
			if err != nil {
				t.Fatal(err)
			}
			r := mcpHTTP(routes, origin, auth.MCPManagementPath, token, origin, peer)
			if r.Code != tc.code || strings.Contains(r.Body.String(), "create_key") != tc.canWrite {
				t.Fatalf("unexpected OAuth response: %d %s", r.Code, r.Body)
			}
		})
	}
}
