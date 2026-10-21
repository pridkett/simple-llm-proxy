package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/pwagstro/simple_llm_proxy/internal/api"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/openapi"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"
	"gopkg.in/yaml.v3"
)

type decisionFixture struct {
	*migrationFixture
	router *router.Router
	spend  *keystore.SpendAccumulator
	calls  atomic.Int64
	appID  int64
	cfg    *config.Config
}

func newDecisionFixture(t *testing.T, snippetLimit ...int) *decisionFixture {
	t.Helper()
	f := &decisionFixture{migrationFixture: &migrationFixture{captured: make(chan map[string]any, 100)}, spend: keystore.NewSpendAccumulator()}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer deployment-only" {
			t.Error("deployment credentials were not used")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		f.captured <- body
		actual, _ := body["model"].(string)
		if actual == "fail" {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"message":"temporarily down"}}`)
			return
		}
		if actual == "error422" || actual == "error429" {
			status := 422
			if actual == "error429" {
				status = 429
				w.Header().Set("Retry-After", "7")
			}
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"message":"rejected by provider"}}`)
			return
		}
		if actual == "malformed" {
			fmt.Fprint(w, `{"model":"malformed","usage":{"input_tokens":100}}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			fmt.Fprint(w, `{"model":"chat","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0}}`)
			return
		}
		answers := any(nil)
		if strings.HasSuffix(r.URL.Path, "/decisions") {
			result := []any{}
			for _, q := range body["questions"].([]any) {
				question := q.(map[string]any)
				result = append(result, map[string]any{"name": question["name"], "type": "predicate", "probability": 0.9, "extension": "kept"})
			}
			answers = result
		} else {
			if actual == "jev" && r.Header.Get("TypeSafe-Danger-Zone-Images") != "true" {
				t.Error("TypeSafe deployment header missing")
			}
			result := map[string]any{}
			for name, q := range body["questions"].(map[string]any) {
				question := q.(map[string]any)
				kind := question["type"]
				if kind == "box" {
					result[name] = map[string]any{"type": "box", "box": map[string]float64{"top": 0.1, "bottom": 0.5, "left": 0.2, "right": 0.8}, "confidence": 0.9}
				} else {
					result[name] = map[string]any{"type": "noul", "noul": 0.9, "extension": "kept"}
				}
			}
			answers = result
		}
		response := map[string]any{"model": actual, "answers": answers, "provider_extra": map[string]bool{"kept": true}}
		if actual != "unknown" {
			response["usage"] = map[string]any{"input_tokens": 100, "output_tokens": 2, "input_tokens_details": map[string]int{"cached_tokens": 30, "cache_write_tokens": 20}}
		}
		if actual == "zero" {
			response["usage"] = map[string]int{"input_tokens": 0, "output_tokens": 0}
		}
		if strings.Contains(r.URL.Path, "/@cf/") {
			response = map[string]any{"success": true, "errors": []any{}, "result": response}
		}
		json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(upstream.Close)
	cfg := config.Defaults()
	cfg.GeneralSettings.MasterKey = "master-only"
	cfg.GeneralSettings.BodySnippetLimit = 100
	if len(snippetLimit) > 0 {
		cfg.GeneralSettings.BodySnippetLimit = snippetLimit[0]
	}
	cfg.RouterSettings.RoutingStrategy = "round-robin"
	pricing := &config.DecisionPricing{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, CostPerRequest: 0.01}
	read, write := 0.0001, 0.002
	pricing.CacheReadInputTokenCost = &read
	pricing.CacheCreationInputTokenCost = &write
	add := func(alias, actual, protocol string) {
		cfg.ModelList = append(cfg.ModelList, config.ModelConfig{ModelName: alias, LiteLLMParams: config.LiteLLMParams{Model: "mock/" + actual, APIKey: "deployment-only", APIBase: upstream.URL + "/v1", ExtraHeaders: map[string]string{"TypeSafe-Danger-Zone-Images": "true"}}, Decision: &config.DecisionConfig{Protocol: protocol, Pricing: pricing, Modalities: []string{"text", "image"}, QuestionTypes: []string{"predicate", "choice", "score", "box"}}})
	}
	add("luna", "luna", "decisions")
	add("jev", "jev", "systemone")
	add("unknown", "unknown", "systemone")
	add("zero", "zero", "systemone")
	add("malformed", "malformed", "systemone")
	add("error422", "error422", "systemone")
	add("error429", "error429", "systemone")
	add("failover", "good", "systemone")
	add("failover", "fail", "systemone")
	add("text-only", "text-only", "systemone")
	cfg.ModelList[len(cfg.ModelList)-1].Decision.Modalities = []string{"text"}
	add("capacity", "capacity", "systemone")
	cfg.ModelList[len(cfg.ModelList)-1].RPM = 1
	add("budgeted", "budgeted", "systemone")
	add("clef", "clef-omni", "cloudflare")
	cfg.ModelList[len(cfg.ModelList)-1].Decision.Path = "/@cf/cloudflare/clef-omni"
	cfg.ModelList[len(cfg.ModelList)-1].Decision.Modalities = []string{"text", "image", "audio", "video"}
	cfg.ModelList = append(cfg.ModelList, config.ModelConfig{ModelName: "chat", LiteLLMParams: config.LiteLLMParams{Model: "openai/chat", APIKey: "deployment-only", APIBase: upstream.URL + "/v1"}})
	cfg.ProviderPools = []config.ProviderPool{{Name: "capped", BudgetCapDaily: 0.1, Members: []config.PoolMember{{ModelName: "budgeted", Weight: 1}}}, {Name: "mixed", Strategy: "round-robin", Members: []config.PoolMember{{ModelName: "text-only", Weight: 1}, {ModelName: "jev", Weight: 1}, {ModelName: "chat", Weight: 1}}}}
	f.cfg = cfg
	path := filepath.Join(t.TempDir(), "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	reloader, err := config.NewReloader(path)
	if err != nil {
		t.Fatal(err)
	}
	f.store, err = sqlite.New(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.store.Close() })
	if err := f.store.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := f.store.UpsertUser(ctx, &storage.User{ID: "admin", Email: "admin@test", IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	team, err := f.store.CreateTeam(ctx, "decision-team")
	if err != nil {
		t.Fatal(err)
	}
	app, err := f.store.CreateApplication(ctx, team.ID, "decision-app")
	if err != nil {
		t.Fatal(err)
	}
	f.appID = app.ID
	f.router, err = router.New(cfg, f.store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.router.Close)
	sm := scs.New()
	sm.Cookie.Secure = false
	login := httptest.NewServer(sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sm.Put(r.Context(), "user_id", "admin") })))
	res, err := http.Get(login.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	f.cookie = res.Cookies()[0]
	login.Close()
	spec := openapi.New()
	if err := spec.Build(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.NewRouter(f.router, f.store, reloader, nil, time.Now(), spec, sm, nil, keystore.New(time.Minute), keystore.NewRateLimiter(), f.spend, nil))
	f.url = server.URL
	t.Cleanup(server.Close)
	return f
}

func TestDecisionDiscoveryAndDisabledSnippets(t *testing.T) {
	f := newDecisionFixture(t, 0)
	status, _, body := f.request(t, "GET", "/v1/models/luna", "", "master-only", false)
	if status != 200 || !strings.Contains(string(body), `"decision_deployments"`) || !strings.Contains(string(body), `"source":"config"`) || !strings.Contains(string(body), `"input_cost_per_token":0.001`) {
		t.Fatalf("model detail: %d %s", status, body)
	}
	status, _, body = f.request(t, "GET", "/v1/models", "", "master-only", false)
	if status != 200 || !strings.Contains(string(body), `"decision_deployments"`) {
		t.Fatalf("discovery: %d %s", status, body)
	}
	status, _, body = f.request(t, "GET", "/admin/config", "", "", true)
	if status != 200 || strings.Contains(string(body), "deployment-only") || !strings.Contains(string(body), `"decision"`) {
		t.Fatalf("sanitized config: %d %s", status, body)
	}
	status, _, body = f.request(t, "POST", "/v1/decisions", decisionBody("luna"), "master-only", false)
	if status != 200 {
		t.Fatalf("call: %d %s", status, body)
	}
	logs, _, err := f.store.GetLogs(context.Background(), 10, 0, storage.LogsFilter{RequestType: "decision"})
	if err != nil || len(logs) != 1 || logs[0].ReqBodySnippet != nil || logs[0].RespBodySnippet != "" {
		t.Fatalf("disabled snippets: %+v %v", logs, err)
	}
	status, _, _ = f.request(t, "GET", "/admin/logs?request_type=typo", "", "", true)
	if status != 400 {
		t.Fatal("invalid request_type silently broadened log query")
	}
}

func TestDecisionRequestSizeBound(t *testing.T) {
	f := newDecisionFixture(t, 0)
	large := `{"model":"luna","input":"` + strings.Repeat("x", 32<<20) + `","questions":[{"type":"predicate","instructions":"x"}]}`
	status, _, _ := f.request(t, "POST", "/v1/decisions", large, "master-only", false)
	if status != 413 || f.calls.Load() != 0 {
		t.Fatalf("size cap: status=%d upstream calls=%d", status, f.calls.Load())
	}
}

func (f *decisionFixture) key(t *testing.T, name string, enabled bool, allowed []string, maxRPM *int, maxBudget *float64) (string, *storage.APIKey) {
	t.Helper()
	token := "sk-app-test-" + name
	hash := sha256.Sum256([]byte(token))
	key, err := f.store.CreateAPIKey(context.Background(), f.appID, name, "prefix", hex.EncodeToString(hash[:]), maxRPM, nil, maxBudget, nil, allowed, &enabled)
	if err != nil {
		t.Fatal(err)
	}
	return token, key
}
func decisionBody(alias string) string {
	return fmt.Sprintf(`{"model":%q,"input":"ticket","questions":[{"type":"predicate","name":"urgent","instructions":"Urgent?"}],"session_id":"session:decision"}`, alias)
}

func TestDecisionPermissionsAndImmediateInvalidation(t *testing.T) {
	f := newDecisionFixture(t)
	blocked, key := f.key(t, "blocked", false, nil, nil, nil)
	for _, path := range []string{"/v1/decisions", "/decisions", "/v1/systemone", "/systemone"} {
		status, _, _ := f.request(t, "POST", path, decisionBody("luna"), blocked, false)
		if status != 403 {
			t.Fatalf("coarse permission at %s: %d", path, status)
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("denied request reached upstream")
	}
	allowed, _ := f.key(t, "restricted", true, []string{"jev"}, nil, nil)
	status, _, _ := f.request(t, "POST", "/v1/decisions", decisionBody("luna"), allowed, false)
	if status != 403 {
		t.Fatalf("model allowlist status=%d", status)
	}
	status, _, body := f.request(t, "PATCH", fmt.Sprintf("/admin/api-keys/%d", key.ID), `{"name":"blocked","allow_decisions":true,"allowed_models":["luna"]}`, "", true)
	if status != 204 {
		t.Fatalf("enable: %d %s", status, body)
	}
	status, _, body = f.request(t, "POST", "/v1/decisions", decisionBody("luna"), blocked, false)
	if status != 200 {
		t.Fatalf("enable did not invalidate cache: %d %s", status, body)
	}
	status, _, _ = f.request(t, "PATCH", fmt.Sprintf("/admin/api-keys/%d", key.ID), `{"name":"renamed","allowed_models":["luna"]}`, "", true)
	if status != 204 {
		t.Fatal(status)
	}
	stored, err := f.store.GetAPIKeyByID(context.Background(), key.ID)
	if err != nil || !stored.AllowDecisions {
		t.Fatalf("omitted permission reset: %+v %v", stored, err)
	}
	status, _, _ = f.request(t, "PATCH", fmt.Sprintf("/admin/api-keys/%d", key.ID), `{"name":"renamed","allow_decisions":false}`, "", true)
	if status != 204 {
		t.Fatal(status)
	}
	status, _, _ = f.request(t, "POST", "/v1/decisions", decisionBody("luna"), blocked, false)
	if status != 403 {
		t.Fatal("disable did not invalidate cached permission")
	}
	status, _, _ = f.request(t, "POST", "/v1/decisions", decisionBody("luna"), "master-only", false)
	if status != 200 {
		t.Fatal("master key did not bypass app permission")
	}
	status, _, _ = f.request(t, "POST", "/v1/decisions", decisionBody("luna"), "", false)
	if status != 401 {
		t.Fatal("missing authentication accepted")
	}
}

func TestDecisionCostUsageLogsAndBudgets(t *testing.T) {
	f := newDecisionFixture(t)
	cap := 0.1
	token, key := f.key(t, "billing", true, []string{"luna"}, nil, &cap)
	status, headers, body := f.request(t, "POST", "/v1/decisions", decisionBody("luna"), token, false)
	if status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	if headers.Get("X-Request-Cost") != "0.10700000000000001" && headers.Get("X-Request-Cost") != "0.107" {
		t.Fatalf("cost header: %s", headers.Get("X-Request-Cost"))
	}
	if math.Abs(f.spend.CurrentSpend(key.ID)-0.107) > 1e-10 {
		t.Fatalf("spend: %v", f.spend.CurrentSpend(key.ID))
	}
	logs, total, err := f.store.GetLogs(context.Background(), 10, 0, storage.LogsFilter{RequestType: "decision", SessionID: "session:decision"})
	if err != nil || total != 1 || len(logs) != 1 {
		t.Fatalf("logs=%+v total=%d err=%v", logs, total, err)
	}
	log := logs[0]
	if log.Model != "luna" || log.APIKeyID == nil || *log.APIKeyID != key.ID || log.InputTokens != 50 || log.CacheReadTokens != 30 || log.CacheWriteTokens != 20 || log.OutputTokens != 2 || !log.UsageKnown || math.Abs(log.TotalCost-0.107) > 1e-10 || log.ReqBodySnippet == nil || log.RespBodySnippet == "" {
		t.Fatalf("log=%+v", log)
	}
	status, _, _ = f.request(t, "POST", "/v1/decisions", decisionBody("luna"), token, false)
	if status != 429 {
		t.Fatalf("key budget not enforced before next call: %d", status)
	}
	status, _, body = f.request(t, "POST", "/v1/decisions", decisionBody("budgeted"), "master-only", false)
	if status != 200 {
		t.Fatalf("pool first: %d %s", status, body)
	}
	if math.Abs(f.router.BudgetManager().CurrentSpend("capped")-0.107) > 1e-10 {
		t.Fatal("pool spend not credited")
	}
	status, _, _ = f.request(t, "POST", "/v1/decisions", decisionBody("budgeted"), "master-only", false)
	if status != 429 {
		t.Fatalf("pool budget not enforced: %d", status)
	}
	status, _, body = f.request(t, "GET", "/admin/logs?request_type=decision&limit=1&offset=1&session_id=session:decision", "", "", true)
	if status != 200 {
		t.Fatalf("filtered logs: %d %s", status, body)
	}
	var page struct {
		Total int   `json:"total"`
		Logs  []any `json:"logs"`
	}
	json.Unmarshal(body, &page)
	if page.Total != 3 || len(page.Logs) != 1 {
		t.Fatalf("filter count and pagination: %s", body)
	}
	status, _, body = f.request(t, "GET", "/admin/logs?request_type=other", "", "", true)
	json.Unmarshal(body, &page)
	if status != 200 || page.Total != 0 {
		t.Fatalf("hide decisions: %d %s", status, body)
	}
}

func TestDecisionModalitiesAndFallback(t *testing.T) {
	f := newDecisionFixture(t)
	image := `{"model":"jev","input":[{"role":"user","content":[{"type":"input_text","text":"photo"},{"type":"input_image","image_url":"data:image/png;base64,eA=="}]}],"questions":[{"name":"target","type":"box","instructions":"locate"}]}`
	status, _, body := f.request(t, "POST", "/v1/decisions", image, "master-only", false)
	if status != 200 || !strings.Contains(string(body), `"box"`) {
		t.Fatalf("Jev image/box: %d %s", status, body)
	}
	var captured map[string]any
	select {
	case captured = <-f.captured:
	default:
		t.Fatal("no upstream image request")
	}
	if captured["assets"] == nil || captured["state"] == nil {
		t.Fatalf("missing media: %+v", captured)
	}
	// A mixed pool can only select the vision-capable decision deployment.
	status, headers, body := f.request(t, "POST", "/v1/decisions", strings.Replace(image, `"model":"jev"`, `"model":"text-only"`, 1), "master-only", false)
	if status != 200 || headers.Get("X-Provider-Used") != "mock/jev" {
		t.Fatalf("capability fallback: %d %s", status, body)
	}
	status, _, body = f.request(t, "POST", "/systemone", `{"model":"clef","state":"clip","audio":["data:audio/mpeg;base64,eA=="],"videos":["data:video/mp4;base64,eA=="],"questions":{"safe":{"type":"noul","instructions":"Safe?"}}}`, "master-only", false)
	if status != 200 || !strings.Contains(string(body), `"noul"`) {
		t.Fatalf("Clef-omni: %d %s", status, body)
	}
	before := f.calls.Load()
	status, _, _ = f.request(t, "POST", "/systemone", `{"model":"clef","state":"photo","images":["http://127.0.0.1/private"],"questions":{"safe":{"type":"noul","instructions":"Safe?"}}}`, "master-only", false)
	if status != 400 || f.calls.Load() != before {
		t.Fatal("remote Cloudflare image was sent upstream")
	}
	status, headers, body = f.request(t, "POST", "/v1/decisions", decisionBody("failover"), "master-only", false)
	if status != 200 {
		t.Fatalf("retry: %d %s", status, body)
	}
	if headers.Get("X-Providers-Tried") != "mock/fail, mock/good" {
		t.Fatal("missing route diagnostics")
	}
	// Generation operations cannot call a decision-only model, even with a key
	// whose allowlist names it. Mixed pools filter decision candidates too.
	blocked, _ := f.key(t, "chat-only", false, []string{"luna", "chat"}, nil, nil)
	before = f.calls.Load()
	status, _, _ = f.request(t, "POST", "/v1/chat/completions", `{"model":"luna","messages":[{"role":"user","content":"hello"}]}`, blocked, false)
	if status < 400 || f.calls.Load() != before {
		t.Fatal("decision permission bypass through chat")
	}
	status, _, body = f.request(t, "POST", "/v1/chat/completions", `{"model":"chat","messages":[{"role":"user","content":"hello"}]}`, blocked, false)
	if status != 200 {
		t.Fatalf("mixed pool generation: %d %s", status, body)
	}
}

func TestDecisionFailuresUnknownUsageAndCapacity(t *testing.T) {
	f := newDecisionFixture(t)
	for _, tc := range []struct {
		alias  string
		status int
	}{{"unknown", 200}, {"zero", 200}, {"error422", 422}, {"error429", 429}, {"malformed", 502}, {"missing", 404}, {"chat", 400}} {
		before := f.calls.Load()
		status, headers, body := f.request(t, "POST", "/decisions", decisionBody(tc.alias), "master-only", false)
		if status != tc.status {
			t.Fatalf("%s: %d %s", tc.alias, status, body)
		}
		if tc.alias == "unknown" && headers.Get("X-Request-Cost") != "" {
			t.Fatal("unknown usage reported as known cost")
		}
		if tc.alias == "error429" && headers.Get("Retry-After") == "" {
			t.Fatal("upstream Retry-After lost")
		}
		if tc.alias == "error422" && f.calls.Load() != before+1 {
			t.Fatal("invalid request retried")
		}
	}
	logs, _, err := f.store.GetLogs(context.Background(), 50, 0, storage.LogsFilter{RequestType: "decision"})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if l.Model == "unknown" && (l.UsageKnown || l.InputTokens != 0) {
			t.Fatalf("unknown usage fabricated: %+v", l)
		}
		if l.Model == "zero" && (!l.UsageKnown || l.InputTokens != 0) {
			t.Fatalf("measured zero lost: %+v", l)
		}
	}
	for i := 0; i < 2; i++ {
		status, _, body := f.request(t, "POST", "/v1/decisions", decisionBody("capacity"), "master-only", false)
		if i == 0 && status != 200 || i == 1 && status != 429 {
			t.Fatalf("deployment RPM: %d %s", status, body)
		}
	}
	one := 1
	token, _ := f.key(t, "rpm", true, nil, &one, nil)
	for i := 0; i < 2; i++ {
		status, _, _ := f.request(t, "POST", "/v1/decisions", decisionBody("luna"), token, false)
		if i == 0 && status != 200 || i == 1 && status != 429 {
			t.Fatalf("key RPM: %d", status)
		}
	}
	before := f.calls.Load()
	status, _, _ := f.request(t, "POST", "/v1/decisions", decisionBody("luna")+` {"trailing":true}`, "master-only", false)
	if status != 400 || f.calls.Load() != before {
		t.Fatal("trailing JSON accepted")
	}
}
