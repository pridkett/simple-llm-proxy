package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/openapi"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/anthropic"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/ollama"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/openai"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/openrouter"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"
)

type migrationFixture struct {
	url      string
	cookie   *http.Cookie
	store    *sqlite.Storage
	captured chan map[string]any
}

func newMigrationFixture(t *testing.T) *migrationFixture {
	t.Helper()
	return newMigrationFixtureWithCostMap(t, nil)
}

func newMigrationFixtureWithCostMap(t *testing.T, cm *costmap.Manager, overrides ...http.HandlerFunc) *migrationFixture {
	return newConfiguredMigrationFixture(t, cm, nil, overrides...)
}
func newConfiguredMigrationFixture(t *testing.T, cm *costmap.Manager, configure func(*config.Config), overrides ...http.HandlerFunc) *migrationFixture {
	t.Helper()
	f := &migrationFixture{captured: make(chan map[string]any, 100)}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(overrides) > 0 {
			overrides[0](w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		f.captured <- req
		if r.Header.Get("Authorization") != "Bearer upstream-only" && r.Header.Get("X-Api-Key") != "upstream-only" {
			t.Error("client key forwarded instead of provider key")
		}
		actual, _ := req["model"].(string)
		if strings.Contains(actual, "error400") || strings.Contains(actual, "error429") {
			status := 400
			if strings.Contains(actual, "429") {
				status = 429
			}
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"message":"upstream rejection","type":"invalid_request_error"}}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			vector := any([]float64{0.1, 0.2})
			if req["encoding_format"] == "base64" {
				vector = "zczMPc3MTD4="
			}
			json.NewEncoder(w).Encode(map[string]any{"object": "list", "model": actual, "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": vector}}, "usage": map[string]int{"prompt_tokens": 10, "total_tokens": 10}})
			return
		}
		if req["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			if strings.HasSuffix(r.URL.Path, "/messages") {
				fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
				return
			}
			fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"created\":1,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n", actual)
			opts, _ := req["stream_options"].(map[string]any)
			if opts["include_usage"] == true && !strings.Contains(actual, "unknown") {
				fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"created\":1,\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3,\"total_tokens\":13}}\n\n")
			}
			fmt.Fprint(w, "data: {\"id\":\"c\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages") {
			fmt.Fprint(w, `{"id":"msg","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":3}}`)
			return
		}
		fmt.Fprint(w, `{"id":"c","object":"chat.completion","model":"upstream","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13},"provider_extra":{"kept":true}}`)
	}))
	t.Cleanup(upstream.Close)
	cfg := config.Defaults()
	cfg.GeneralSettings.MasterKey = "master-only"
	cfg.GeneralSettings.BodySnippetLimit = 0
	for _, name := range []string{"openai", "openrouter", "anthropic", "ollama", "unknown", "error400", "error429", "capacity"} {
		providerName := name
		if name == "unknown" || strings.HasPrefix(name, "error") || name == "capacity" {
			providerName = "openai"
		}
		rpm := 0
		if name == "capacity" {
			rpm = 1
		}
		cfg.ModelList = append(cfg.ModelList, config.ModelConfig{ModelName: name, RPM: rpm, LiteLLMParams: config.LiteLLMParams{Model: providerName + "/wire-" + name, APIKey: "upstream-only", APIBase: upstream.URL + "/v1"}})
	}
	cfg.RouterSettings.AllowedFails = 1
	if configure != nil {
		configure(cfg)
	}
	// Config supports YAML; JSON is also a valid YAML representation.
	// yaml tags differ from JSON, so use a small literal fixture for the reloader.
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(configPath, []byte("model_list:\n - model_name: test\n   litellm_params:\n     model: openai/test\n     api_key: unused\ngeneral_settings:\n master_key: master-only\n body_snippet_limit: 0\n"), 0600)
	reloader, err := config.NewReloader(configPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	f.store = store
	t.Cleanup(func() { store.Close() })
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertUser(context.Background(), &storage.User{ID: "test-admin", Email: "admin@example.test", Name: "Admin", IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	route, err := router.New(cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(route.Close)
	sm := scs.New()
	sm.Cookie.Secure = false
	// Mint a fixture session through SCS; OIDC protocol verification has its own tests.
	login := httptest.NewServer(sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sm.Put(r.Context(), "user_id", "test-admin") })))
	response, err := http.Get(login.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	f.cookie = response.Cookies()[0]
	login.Close()
	spec := openapi.New()
	if err := spec.Build(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.NewRouter(route, store, reloader, cm, time.Now(), spec, sm, nil, keystore.New(time.Minute), keystore.NewRateLimiter(), keystore.NewSpendAccumulator(), nil))
	f.url = server.URL
	t.Cleanup(server.Close)
	return f
}
func (f *migrationFixture) request(t *testing.T, method, path, body, key string, admin bool) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.url+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if admin {
		req.AddCookie(f.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, data
}
func (f *migrationFixture) create(t *testing.T, path, body string) map[string]any {
	t.Helper()
	status, _, data := f.request(t, "POST", path, body, "", true)
	if status != 201 {
		t.Fatalf("create %s: %d", path, status)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func (f *migrationFixture) log(t *testing.T, id string) *storage.RequestLog {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		row, err := f.store.GetLogByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if row != nil {
			return row
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("request log not persisted")
	return nil
}
func TestMigrationFourProvidersAndAppKeys(t *testing.T) {
	f := newMigrationFixture(t)
	team := f.create(t, "/admin/teams", `{"name":"apps"}`)
	app := f.create(t, "/admin/applications", fmt.Sprintf(`{"name":"OpenWebUI","team_id":%.0f}`, team["id"]))
	key := f.create(t, fmt.Sprintf("/admin/applications/%.0f/keys", app["id"]), `{"name":"OpenWebUI pilot"}`)
	token := key["key"].(string)
	for _, name := range []string{"openai", "openrouter", "anthropic", "ollama"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", name, stream), func(t *testing.T) {
				body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"stream":%t,"stream_options":{"include_usage":true}}`, name, stream)
				status, headers, data := f.request(t, "POST", "/v1/chat/completions", body, token, false)
				if status != 200 || !bytes.Contains(data, []byte("hello")) {
					t.Fatalf("chat failed: %d %s", status, data)
				}
				row := f.log(t, headers.Get("X-Request-ID"))
				if !row.UsageKnown || row.InputTokens != 10 || row.OutputTokens != 3 || row.APIKeyID == nil {
					t.Fatalf("bad accounting: %+v", row)
				}
				captured := <-f.captured
				if captured["model"] != "wire-"+name {
					t.Fatal("alias was not rewritten", captured)
				}
			})
		}
	}
	// Revocation must invalidate an already cached token immediately.
	status, _, _ := f.request(t, "DELETE", fmt.Sprintf("/admin/api-keys/%.0f", key["id"]), "", "", true)
	if status != 204 {
		t.Fatal(status)
	}
	status, _, _ = f.request(t, "POST", "/v1/chat/completions", `{"model":"openai","messages":[{"role":"user","content":"hello"}]}`, token, false)
	if status != 401 {
		t.Fatalf("revoked key accepted: %d", status)
	}
	status, _, _ = f.request(t, "GET", "/admin/status", "", "master-only", false)
	if status != 401 {
		t.Fatal("master key bypassed browser auth")
	}
}
func TestMigrationCompatibilityAndErrors(t *testing.T) {
	f := newMigrationFixture(t)
	modern := `{"model":"openai","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":7,"reasoning_effort":"low","parallel_tool_calls":false,"stop":"END","response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{},"strict":true}}}`
	status, _, data := f.request(t, "POST", "/v1/chat/completions", modern, "master-only", false)
	if status != 200 {
		t.Fatal(status, string(data))
	}
	captured := <-f.captured
	if captured["max_completion_tokens"] != float64(7) || captured["reasoning_effort"] != "low" || captured["parallel_tool_calls"] != false || captured["stop"] != "END" {
		t.Fatal("request fields lost", captured)
	}
	if !bytes.Contains(data, []byte("provider_extra")) {
		t.Fatal("response fields lost")
	}
	format := captured["response_format"].(map[string]any)
	if format["json_schema"] == nil {
		t.Fatal("schema lost")
	}
	for _, encoding := range []string{"float", "base64"} {
		status, _, data = f.request(t, "POST", "/v1/embeddings", fmt.Sprintf(`{"model":"openai","input":"hello","dimensions":2,"encoding_format":%q}`, encoding), "master-only", false)
		if status != 200 {
			t.Fatal(status, string(data))
		}
		if req := <-f.captured; req["dimensions"] != float64(2) {
			t.Fatal("dimensions lost")
		}
	}
	for _, code := range []int{400, 429} {
		status, headers, _ := f.request(t, "POST", "/v1/chat/completions", fmt.Sprintf(`{"model":"error%d","messages":[{"role":"user","content":"hello"}]}`, code), "master-only", false)
		if status != code || headers.Get("Retry-After") != "7" {
			t.Fatal("upstream status lost", status, headers)
		}
		<-f.captured
	}
	for i := 0; i < 2; i++ {
		status, _, _ = f.request(t, "POST", "/v1/chat/completions", `{"model":"capacity","messages":[{"role":"user","content":"hello"}]}`, "master-only", false)
		want := 200
		if i == 1 {
			want = 429
		}
		if status != want {
			t.Fatalf("capacity request %d: %d", i, status)
		}
	}
	<-f.captured
	for _, name := range []string{"openai", "unknown"} {
		status, headers, body := f.request(t, "POST", "/v1/chat/completions", fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"stream":true}`, name), "master-only", false)
		if status != 200 {
			t.Fatal(status)
		}
		if bytes.Contains(body, []byte(`"usage"`)) {
			t.Fatal("unsolicited usage delivered")
		}
		row := f.log(t, headers.Get("X-Request-ID"))
		if row.UsageKnown != (name != "unknown") {
			t.Fatalf("usage availability lost: %+v", row)
		}
		req := <-f.captured
		if req["stream_options"].(map[string]any)["include_usage"] != true {
			t.Fatal("usage not requested")
		}
	}
}
