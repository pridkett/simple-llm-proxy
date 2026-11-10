package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"
)

func TestGatewayCredentialsAndSessions(t *testing.T) {
	ctx := context.Background()
	var upstreamCalls atomic.Int64
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/destination", 307)
			return
		}
		if r.URL.RawQuery != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Forwarded-Host") != "" {
			t.Error("client routing or auth headers reached upstream")
		}
		switch r.URL.Path {
		case "/shared":
			if r.Header.Get("X-Api-Key") != "shared-secret" || r.Header.Get("Authorization") != "" {
				t.Error("shared upstream received incorrect credential")
			}
		case "/personal":
			if r.Header.Get("Authorization") != "Bearer alice-upstream" && r.Header.Get("Authorization") != "Bearer bob-upstream" {
				t.Error("personal upstream received incorrect credential")
			}
		default:
			t.Errorf("unexpected upstream path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "proxy_session=bad")
		if r.Header.Get("Mcp-Protocol-Version") == "2026-07-28" {
			if r.Header.Get("Mcp-Method") != "tools/call" || r.Header.Get("Mcp-Param-Region") != "local" {
				t.Error("modern MCP headers were lost")
			}
		} else if r.Header.Get(sessionHeader) == "" {
			w.Header().Set(sessionHeader, "upstream-session")
		} else if r.Header.Get(sessionHeader) != "upstream-session" {
			t.Error("proxy did not restore the upstream session ID")
		}
		// Report the upstream account selected by the fixture without echoing its secret.
		account, _, _ := strings.Cut(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "-")
		json.NewEncoder(w).Encode(map[string]string{"account": account})
	}))
	defer upstream.Close()

	store, err := sqlite.New(":memory:")
	must(t, err)
	defer store.Close()
	must(t, store.Initialize(ctx))
	for _, user := range []string{"alice", "bob", "carol"} {
		must(t, store.UpsertUser(ctx, &storage.User{ID: user, Name: user, Email: user + "@example.test"}))
	}
	team, err := store.CreateTeam(ctx, "team")
	must(t, err)
	app, err := store.CreateApplication(ctx, team.ID, "app")
	must(t, err)
	appToken := "sk-app-" + strings.Repeat("a", 48)
	appKey, err := store.CreateAPIKey(ctx, app.ID, "key", "aaaaaaaa", digest(appToken), nil, nil, nil, nil, nil)
	must(t, err)
	expiredToken := "sk-app-" + strings.Repeat("b", 48)
	past := time.Now().Add(-time.Hour)
	expiredKey, err := store.CreateAPIKey(ctx, app.ID, "expired", "bbbbbbbb", digest(expiredToken),
		nil, nil, nil, nil, nil, storage.APIKeyOptions{ExpiresAt: &past})
	must(t, err)
	cfg := config.Defaults()
	cfg.MCP.CredentialKey[0] = 123
	cfg.MCP.Servers = map[string]config.MCPServer{
		"personal": {URL: upstream.URL + "/personal", Scope: "user", Header: "Authorization", AllowedUserIDs: []string{"alice", "bob"}},
		"shared":   {URL: upstream.URL + "/shared", Scope: "shared", Header: "X-Api-Key", SharedKey: "shared-secret", AllowedAppKeyIDs: []int64{appKey.ID, expiredKey.ID}},
		"redirect": {URL: upstream.URL + "/redirect", Scope: "none", AllowedAppKeyIDs: []int64{appKey.ID}},
	}

	g := newGateway(cfg, store, keystore.NewRateLimiter(), upstream.Client().Transport)
	sm := scs.New()
	routes := chi.NewRouter()
	g.register(routes, sm)

	do := func(method, path, token, body string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status = %d, want %d: %s", w.Code, status, w.Body)
		}
	}
	cookie := func(user string) string {
		t.Helper()
		session, err := sm.Load(ctx, "")
		must(t, err)
		sm.Put(session, "user_id", user)
		token, _, err := sm.Commit(session)
		must(t, err)
		return sm.Cookie.Name + "=" + token
	}
	const personal = "/api/mcp/connections/personal"
	aliceCookie := map[string]string{"Cookie": cookie("alice")}
	check(do("PUT", personal, "", `{"key":"alice-upstream"}`, nil), 401)
	check(do("PUT", personal, "", `{"key":"carol-upstream"}`, map[string]string{"Cookie": cookie("carol")}), 404)
	check(do("PUT", personal, "", `{"key":"cross-origin"}`, map[string]string{"Cookie": aliceCookie["Cookie"], "Origin": "https://untrusted.test"}), 403)
	check(do("POST", personal+"/token", "", "", aliceCookie), 409)
	connect := func(user string, headers map[string]string) string {
		t.Helper()
		check(do("PUT", personal, "", fmt.Sprintf(`{"key":%q}`, user+"-upstream"), headers), 204)
		return mint(t, do("POST", personal+"/token", "", "", headers))
	}
	alice := connect("alice", aliceCookie)
	bob := connect("bob", map[string]string{"Cookie": cookie("bob")})
	status := do("GET", "/api/mcp/connections/", "", "", aliceCookie)
	check(status, 200)
	if !strings.Contains(status.Body.String(), `"key_set":true`) || strings.Contains(status.Body.String(), "alice-upstream") || strings.Contains(status.Body.String(), alice) {
		t.Fatal("unexpected connection status")
	}
	record, err := store.GetMCPConnection(ctx, g.servers["personal"].binding, "alice")
	must(t, err)
	if len(record.Ciphertext) == 0 || bytes.Contains(record.Ciphertext, []byte("alice-upstream")) {
		t.Fatal("upstream key must be encrypted in storage")
	}

	const message = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	check(do("POST", "/mcp/personal", appToken, message, nil), 403)
	check(do("POST", "/mcp/shared", alice, message, nil), 401)
	check(do("POST", "/mcp/shared", "master", message, nil), 401)
	check(do("POST", "/mcp/shared", appToken, message, map[string]string{"Origin": "https://untrusted.test"}), 403)
	check(do("POST", "/mcp/shared?target=other", appToken, message, map[string]string{
		"Cookie": "fake=secret", "X-Api-Key": "override", "X-Forwarded-Host": "other.test",
	}), 200)
	redirect := do("POST", "/mcp/redirect", appToken, message, nil)
	check(redirect, 502)
	if redirect.Header().Get("Location") != "" || upstreamCalls.Load() != 2 {
		t.Fatal("redirect must stay inside the gateway")
	}
	aliceCall := do("POST", "/mcp/personal", alice, message, nil)
	check(aliceCall, 200)
	if !strings.Contains(aliceCall.Body.String(), `"account":"alice"`) || aliceCall.Header().Get("Set-Cookie") != "" {
		t.Fatal("incorrect Alice response or leaked upstream cookie")
	}
	handle := aliceCall.Header().Get(sessionHeader)
	if handle == "" || handle == "upstream-session" {
		t.Fatal("gateway did not wrap upstream session ID")
	}
	headers := map[string]string{sessionHeader: handle}
	check(do("POST", "/mcp/personal", bob, message, headers), 404)
	check(do("GET", "/mcp/personal", alice, "", headers), 200)
	check(do("DELETE", "/mcp/personal", alice, "", headers), 200)
	bobCall := do("POST", "/mcp/personal", bob, `{"jsonrpc":"2.0","id":1,"method":"tools/call"}`, map[string]string{
		"Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/call", "Mcp-Param-Region": "local",
	})
	check(bobCall, 200)
	if !strings.Contains(bobCall.Body.String(), `"account":"bob"`) || bobCall.Header().Get(sessionHeader) != "" {
		t.Fatal("incorrect Bob response")
	}

	replacement := mint(t, do("POST", personal+"/token", "", "", aliceCookie))
	check(do("POST", "/mcp/personal", alice, message, nil), 401)
	check(do("POST", "/mcp/personal", replacement, message, headers), 404)
	check(do("DELETE", personal+"/token", "", "", aliceCookie), 204)
	check(do("POST", "/mcp/personal", replacement, message, nil), 401)
	check(do("DELETE", personal, "", "", aliceCookie), 204)
	check(do("POST", personal+"/token", "", "", aliceCookie), 409)
	check(do("POST", "/mcp/shared", expiredToken, message, nil), 401)
	must(t, store.RevokeAPIKey(ctx, appKey.ID))
	check(do("POST", "/mcp/shared", appToken, message, nil), 401)
}

func TestRelayFlushesEventStream(t *testing.T) {
	sent := make(chan struct{})
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message\ndata: first\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-sent:
			fmt.Fprint(w, "event: message\ndata: second\n\n")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()

	store, err := sqlite.New(":memory:")
	must(t, err)
	defer store.Close()
	must(t, store.Initialize(context.Background()))
	team, err := store.CreateTeam(context.Background(), "team")
	must(t, err)
	app, err := store.CreateApplication(context.Background(), team.ID, "app")
	must(t, err)
	token := "sk-app-" + strings.Repeat("b", 48)
	key, err := store.CreateAPIKey(context.Background(), app.ID, "key", "bbbbbbbb", digest(token), nil, nil, nil, nil, nil)
	must(t, err)

	cfg := config.Defaults()
	cfg.MCP.Servers = map[string]config.MCPServer{
		"stream": {URL: upstream.URL, Scope: "none", AllowedAppKeyIDs: []int64{key.ID}},
	}
	routes := chi.NewRouter()
	routes.Use(middleware.Recovery(), middleware.Logging())
	g := newGateway(cfg, store, keystore.NewRateLimiter(), upstream.Client().Transport)
	g.register(routes, scs.New())
	proxy := httptest.NewServer(routes)
	defer proxy.Close()
	defer close(sent)
	client := proxy.Client()
	client.Timeout = 3 * time.Second
	req, err := http.NewRequest("POST", proxy.URL+"/mcp/stream", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	must(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(req)
	must(t, err)
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected stream response %s: %s", response.Status, body)
	}
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	must(t, err)
	second, err := reader.ReadString('\n')
	must(t, err)
	if first+second != "event: message\ndata: first\n" {
		t.Fatalf("unexpected first event %q", first+second)
	}
}

func mint(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	if response.Code != 201 {
		t.Fatalf("mint status = %d: %s", response.Code, response.Body)
	}
	var result struct{ Token string }
	must(t, json.Unmarshal(response.Body.Bytes(), &result))
	if !strings.HasPrefix(result.Token, tokenPrefix) {
		t.Fatal("expected a personal gateway token")
	}
	return result.Token
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
