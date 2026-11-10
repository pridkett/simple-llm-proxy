package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"
	"golang.org/x/oauth2"
)

func TestOAuthGateway(t *testing.T) {
	var exchanges, refreshes atomic.Int64
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := "https://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/tools":
			json.NewEncoder(w).Encode(map[string]any{
				"resource": origin + "/tools", "authorization_servers": []string{origin},
			})
		case "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(oauthProvider{
				Issuer: origin, AuthURL: origin + "/authorize", TokenURL: origin + "/token",
				PKCEMethods: []string{"S256"}, AuthMethods: []string{"none", "client_secret_basic"}, IssuerInResponse: true,
			})
		case "/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("resource") != origin+"/tools" {
				t.Error("token request did not include the MCP resource")
			}
			var name string
			var expires int
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				exchanges.Add(1)
				var challenge string
				name, challenge, _ = strings.Cut(r.Form.Get("code"), ":")
				if oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != challenge {
					t.Error("invalid PKCE verifier")
				}
				if name == "shared" {
					id, secret, ok := r.BasicAuth()
					if !ok || id != "registered-client" || secret != "client-secret" {
						t.Error("confidential client did not authenticate")
					}
				} else if r.Form.Get("client_id") != "registered-client" || r.Header.Get("Authorization") != "" {
					t.Error("unexpected public client authentication")
				}
				expires = 3600
				if name == "alice" {
					expires = 1 // refresh on first call
				}
			case "refresh_token":
				refreshes.Add(1)
				name = strings.TrimPrefix(r.Form.Get("refresh_token"), "refresh-")
				expires = 3600
			default:
				t.Error("unexpected OAuth grant")
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-" + name, "refresh_token": "refresh-" + name, "token_type": "Bearer", "expires_in": expires,
			})
		case "/tools":
			name := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer access-")
			if name != "alice" && name != "bob" && name != "shared" {
				t.Error("wrong credential forwarded to upstream MCP")
			}
			json.NewEncoder(w).Encode(map[string]string{"account": name})
		default:
			t.Errorf("unexpected provider path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	ctx := context.Background()
	store, err := sqlite.New(":memory:")
	must(t, err)
	defer store.Close()
	must(t, store.Initialize(ctx))
	for _, user := range []string{"alice", "bob", "admin"} {
		must(t, store.UpsertUser(ctx, &storage.User{
			ID: user, Name: user, Email: user + "@example.test", IsAdmin: user == "admin",
		}))
	}
	team, err := store.CreateTeam(ctx, "team")
	must(t, err)
	app, err := store.CreateApplication(ctx, team.ID, "app")
	must(t, err)
	appToken := "sk-app-" + strings.Repeat("a", 48)
	key, err := store.CreateAPIKey(ctx, app.ID, "key", "aaaaaaaa", digest(appToken), nil, nil, nil, nil, nil)
	must(t, err)

	settings := func(server, secret string) *config.MCPOAuth {
		return &config.MCPOAuth{
			Issuer: upstream.URL, ClientID: "registered-client", ClientSecret: secret,
			RedirectURL: "https://proxy.example.test/api/mcp/oauth/callback/" + server,
			Resource:    upstream.URL + "/tools", ResourceMetadataURL: upstream.URL + "/.well-known/oauth-protected-resource/tools",
		}
	}
	cfg := config.Defaults()
	cfg.MCP.CredentialKey[0] = 99
	cfg.MCP.Servers = map[string]config.MCPServer{
		"personal": {
			URL: upstream.URL + "/tools", Scope: "user", Header: "Authorization",
			AllowedUserIDs: []string{"alice", "bob"}, OAuth: settings("personal", ""),
		},
		"shared": {
			URL: upstream.URL + "/tools", Scope: "shared", Header: "Authorization",
			AllowedAppKeyIDs: []int64{key.ID}, OAuth: settings("shared", "client-secret"),
		},
	}

	g := newGateway(cfg, store, keystore.NewRateLimiter(), upstream.Client().Transport)
	sm := scs.New()
	routes := chi.NewRouter()
	g.register(routes, sm, config.LocalAdminSettings{})
	cookie := func(user string) *http.Cookie {
		session, err := sm.Load(ctx, "")
		must(t, err)
		sm.Put(session, "user_id", user)
		token, _, err := sm.Commit(session)
		must(t, err)
		return &http.Cookie{Name: sm.Cookie.Name, Value: token}
	}
	do := func(method, path string, session *http.Cookie, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if session != nil {
			req.AddCookie(session)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, req)
		return w
	}
	status := func(w *httptest.ResponseRecorder, want int) {
		t.Helper()
		if w.Code != want {
			t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body)
		}
	}
	start := func(server, account string, session *http.Cookie) string {
		t.Helper()
		result := do("POST", "/api/mcp/oauth/"+server, session, "")
		status(result, 200)
		var body struct {
			AuthorizationURL string `json:"authorization_url"`
		}
		must(t, json.Unmarshal(result.Body.Bytes(), &body))
		target, err := url.Parse(body.AuthorizationURL)
		must(t, err)
		q := target.Query()
		if target.Path != "/authorize" || q.Get("resource") != upstream.URL+"/tools" || q.Get("code_challenge_method") != "S256" {
			t.Fatal("incorrect authorization URL")
		}
		return "/api/mcp/oauth/callback/" + server + "?" + url.Values{
			"state": {q.Get("state")}, "code": {account + ":" + q.Get("code_challenge")}, "iss": {upstream.URL},
		}.Encode()
	}

	alice, bob, admin := cookie("alice"), cookie("bob"), cookie("admin")
	const personalToken = "/api/mcp/connections/personal/token"
	status(do("POST", personalToken, alice, ""), 409)
	status(do("POST", "/api/mcp/oauth/shared", alice, ""), 404)
	status(do("POST", "/mcp/shared", nil, appToken), 403)
	callback := start("personal", "alice", alice)
	status(do("GET", callback, cookie("alice"), ""), 400)
	status(do("GET", callback, bob, ""), 400)
	if exchanges.Load() != 0 {
		t.Fatal("a different browser reached the token endpoint")
	}
	wrongIssuer := strings.Replace(callback, url.QueryEscape(upstream.URL), url.QueryEscape("https://wrong.example.test"), 1)
	result := do("GET", wrongIssuer, alice, "")
	status(result, 303)
	if result.Header().Get("Location") != "/#/mcp?oauth=failed" || exchanges.Load() != 0 {
		t.Fatal("wrong issuer reached the token endpoint")
	}
	callback = start("personal", "alice", alice)
	result = do("GET", callback, alice, "")
	status(result, 303)
	if result.Header().Get("Location") != "/#/mcp?oauth=connected" {
		t.Fatal("Alice connection did not succeed")
	}
	status(do("GET", callback, alice, ""), 400)
	status(do("GET", start("personal", "bob", bob), bob, ""), 303)
	status(do("GET", start("shared", "shared", admin), admin, ""), 303)
	aliceToken := mint(t, do("POST", personalToken, alice, ""))
	bobToken := mint(t, do("POST", personalToken, bob, ""))
	status(do("POST", "/api/mcp/connections/shared/token", admin, ""), 404)

	calls := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { calls <- do("POST", "/mcp/personal", nil, aliceToken) })
	}
	wg.Wait()
	close(calls)
	for call := range calls {
		status(call, 200)
		if !strings.Contains(call.Body.String(), `"account":"alice"`) {
			t.Fatal("personal OAuth used the wrong account")
		}
	}
	for _, call := range []struct{ path, token, account string }{
		{"/mcp/personal", bobToken, "bob"}, {"/mcp/shared", appToken, "shared"},
	} {
		response := do("POST", call.path, nil, call.token)
		status(response, 200)
		if !strings.Contains(response.Body.String(), fmt.Sprintf(`"account":%q`, call.account)) {
			t.Fatal("OAuth used the wrong account")
		}
	}
	if refreshes.Load() != 1 || exchanges.Load() != 3 {
		t.Fatalf("OAuth calls: %d refreshes, %d exchanges", refreshes.Load(), exchanges.Load())
	}
	record, err := store.GetMCPOAuth(ctx, g.servers["personal"].binding, "alice")
	must(t, err)
	if record == nil || bytes.Contains(record.Ciphertext, []byte("refresh-alice")) {
		t.Fatal("OAuth grant was not encrypted")
	}
	status(do("DELETE", "/api/mcp/oauth/personal", alice, ""), 204)
	status(do("POST", "/mcp/personal", nil, aliceToken), 401)
	status(do("POST", personalToken, alice, ""), 409)
	status(do("DELETE", "/api/mcp/oauth/shared", admin, ""), 204)
	status(do("POST", "/mcp/shared", nil, appToken), 403)
}
