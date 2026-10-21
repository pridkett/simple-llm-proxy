package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func accountingRequest(t *testing.T, f *migrationFixture, path, body, key string, inline bool) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest("POST", f.url+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "reusable-client-id")
	if inline {
		req.Header.Set("X-LLM-Include-Accounting", "true")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, b
}
func getAccounting(t *testing.T, f *migrationFixture, id, key string) *accounting.Record {
	t.Helper()
	status, _, body := f.request(t, "GET", "/v1/accounting/"+id, "", key, false)
	if status != 200 {
		t.Fatalf("lookup: %d %s", status, body)
	}
	var rec accounting.Record
	if err := json.Unmarshal(body, &rec); err != nil {
		t.Fatal(err)
	}
	return &rec
}

const accountingReply = `{"id":"c","object":"chat.completion","model":"wire-openai","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`

func TestAccountingUpstreamHeadersAndLookup(t *testing.T) {
	f := newMigrationFixtureWithCostMap(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-litellm-response-cost", "0.000415")
		w.Header().Set("x-litellm-total-tokens", "1874")
		fmt.Fprint(w, accountingReply)
	})
	status, h, b := accountingRequest(t, f, "/v1/chat/completions", `{"model":"openai","messages":[{"role":"user","content":"hi"}]}`, "master-only", true)
	if status != 200 || h.Get("X-LLM-Cost-USD") != "0.000415" || h.Get("X-LLM-Cost-Source") != "upstream" || h.Get("X-Litellm-Total-Tokens") != "1874" || !bytes.Contains(b, []byte(`"proxy_accounting"`)) {
		t.Fatalf("status=%d headers=%v body=%s", status, h, b)
	}
	id := h.Get("X-LLM-Accounting-ID")
	rec := getAccounting(t, f, id, "master-only")
	if rec.State != "final" || rec.RequestID != "reusable-client-id" || rec.Usage.InputTokens != nil || *rec.Usage.TotalTokens != 1874 || *rec.Cost.Amount != "0.000415" {
		t.Fatalf("%+v", rec)
	}
	_, h2, b2 := accountingRequest(t, f, "/v1/chat/completions", `{"model":"openai","messages":[{"role":"user","content":"hi"}]}`, "master-only", false)
	if h2.Get("X-LLM-Accounting-ID") == id || bytes.Contains(b2, []byte(`"proxy_accounting"`)) {
		t.Fatalf("IDs reused or unsolicited extension: %s", b2)
	}
}
func TestAccountingEstimateAndUnknown(t *testing.T) {
	cm := costmap.New()
	cm.SetCustomSpec("openai", costmap.ModelSpec{InputCostPerToken: 0.000001, OutputCostPerToken: 0.000002, CacheReadInputTokenCost: 0.0000001})
	f := newMigrationFixtureWithCostMap(t, cm, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"c","choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":200,"prompt_tokens_details":{"cached_tokens":400},"completion_tokens_details":{"reasoning_tokens":100}}}`)
	})
	_, h, _ := accountingRequest(t, f, "/v1/chat/completions", `{"model":"openai","messages":[{"role":"user","content":"hi"}]}`, "master-only", false)
	rec := getAccounting(t, f, h.Get("X-LLM-Accounting-ID"), "master-only")
	if rec.Cost.Amount == nil || *rec.Cost.Amount != "0.00104" || rec.Cost.Source != "estimate" || *rec.Usage.TotalTokens != 1200 || *rec.Usage.OutputDetails.ReasoningTokens != 100 {
		t.Fatalf("%+v", rec)
	}
	f2 := newMigrationFixture(t)
	_, h, _ = accountingRequest(t, f2, "/v1/chat/completions", `{"model":"openai","messages":[{"role":"user","content":"hi"}]}`, "master-only", false)
	rec = getAccounting(t, f2, h.Get("X-LLM-Accounting-ID"), "master-only")
	if rec.Cost.Amount != nil || h.Get("X-LLM-Cost-USD") != "" || rec.Coverage != "unknown" {
		t.Fatalf("%+v", rec)
	}
}
func TestAccountingErrorStillCostsMoney(t *testing.T) {
	f := newMigrationFixtureWithCostMap(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-litellm-response-cost", "0.25")
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"charged failure"}}`)
	})
	status, h, _ := accountingRequest(t, f, "/v1/chat/completions", `{"model":"openai","messages":[{"role":"user","content":"hi"}]}`, "master-only", false)
	if status != 400 || h.Get("X-LLM-Cost-USD") != "0.25" {
		t.Fatalf("%d %v", status, h)
	}
	rec := getAccounting(t, f, h.Get("X-LLM-Accounting-ID"), "master-only")
	if rec.Attempts[0].Status != "failed" || *rec.Cost.Amount != "0.25" || rec.Usage.TotalTokens != nil {
		t.Fatalf("%+v", rec)
	}
	logs, _, err := f.store.GetLogs(context.Background(), 10, 0, storage.LogsFilter{})
	if err != nil || len(logs) != 1 || logs[0].TotalCost != 0.25 || logs[0].CostKnown == nil || !*logs[0].CostKnown {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
}
func TestAccountingStreamingOptIn(t *testing.T) {
	f := newMigrationFixtureWithCostMap(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-litellm-response-cost", "0.15")
		fmt.Fprint(w, "data: {\"id\":\"c\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3,\"total_tokens\":13}}\n\ndata: [DONE]\n\n")
	})
	for _, inline := range []bool{false, true} {
		_, h, b := accountingRequest(t, f, "/v1/chat/completions", `{"model":"openai","messages":[{"role":"user","content":"hi"}],"stream":true}`, "master-only", inline)
		if h.Get("X-LLM-Accounting-State") != "pending" || h.Get("X-LLM-Cost-USD") != "" || bytes.Contains(b, []byte(`"proxy_accounting"`)) != inline {
			t.Fatalf("%v %s", h, b)
		}
		rec := getAccounting(t, f, h.Get("X-LLM-Accounting-ID"), "master-only")
		if rec.State != "final" || *rec.Cost.Amount != "0.15" || *rec.Usage.TotalTokens != 13 {
			t.Fatalf("%+v", rec)
		}
		if inline && bytes.Index(b, []byte(`"proxy_accounting"`)) > bytes.Index(b, []byte("[DONE]")) {
			t.Fatalf("accounting after DONE: %s", b)
		}
	}
}
func TestAccountingRecordOwnership(t *testing.T) {
	f := newMigrationFixture(t)
	team := f.create(t, "/admin/teams", `{"name":"accounting-team"}`)
	app := f.create(t, "/admin/applications", fmt.Sprintf(`{"team_id":%v,"name":"app"}`, team["id"]))
	path := fmt.Sprintf("/admin/applications/%v/keys", app["id"])
	first := f.create(t, path, `{"name":"first"}`)
	second := f.create(t, path, `{"name":"second"}`)
	token1, _ := first["key"].(string)
	token2, _ := second["key"].(string)
	if token1 == "" || token2 == "" {
		t.Fatalf("key response fields: %v", first)
	}
	status, h, _ := accountingRequest(t, f, "/v1/chat/completions", `{"model":"openai","messages":[{"role":"user","content":"hi"}]}`, token1, false)
	if status != 200 {
		t.Fatal(status)
	}
	id := h.Get("X-LLM-Accounting-ID")
	getAccounting(t, f, id, token1)
	status, _, _ = f.request(t, "GET", "/v1/accounting/"+id, "", token2, false)
	if status != 404 {
		t.Fatalf("other key read: %d", status)
	}
	status, _, _ = f.request(t, "GET", "/v1/accounting/"+id, "", "", false)
	if status != 401 {
		t.Fatalf("unauthenticated read: %d", status)
	}
}

func TestAccountingFailoverIncludesEveryAttempt(t *testing.T) {
	f := newConfiguredMigrationFixture(t, nil, func(cfg *config.Config) {
		cfg.RouterSettings.RoutingStrategy = "round-robin"
		base := cfg.ModelList[0]
		base.ModelName = "retry"
		base.LiteLLMParams.Model = "openai/fail-one"
		second := base
		second.LiteLLMParams.Model = "openai/win"
		cfg.ModelList = []config.ModelConfig{second, base}
	}, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model == "fail-one" {
			w.Header().Set("x-litellm-response-cost", "0.2")
			w.WriteHeader(500)
			fmt.Fprint(w, `{"error":{"message":"charged failure"}}`)
			return
		}
		w.Header().Set("x-litellm-response-cost", "0.1")
		fmt.Fprint(w, accountingReply)
	})
	status, h, b := accountingRequest(t, f, "/v1/chat/completions", `{"model":"retry","messages":[{"role":"user","content":"hi"}]}`, "master-only", false)
	if status != 200 || h.Get("X-LLM-Cost-USD") != "0.3" {
		t.Fatalf("%d %v %s", status, h, b)
	}
	rec := getAccounting(t, f, h.Get("X-LLM-Accounting-ID"), "master-only")
	if len(rec.Attempts) != 2 || rec.Attempts[0].Status != "failed" || rec.Attempts[1].Status != "completed" || rec.Coverage != "complete" {
		t.Fatalf("%+v", rec)
	}
	logs, total, err := f.store.GetLogs(context.Background(), 10, 0, storage.LogsFilter{})
	if err != nil || total != 1 || logs[0].TotalCost != 0.3 {
		t.Fatalf("%+v total=%d err=%v", logs, total, err)
	}
}
func TestAccountingResponsesAndEmbeddings(t *testing.T) {
	f := newMigrationFixtureWithCostMap(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-litellm-response-cost", "0.05")
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			fmt.Fprint(w, `{"object":"list","data":[],"usage":{"prompt_tokens":10,"total_tokens":10}}`)
			return
		}
		var req struct {
			Stream bool `json:"stream"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":3,\"total_tokens\":13}}}\n\n")
			return
		}
		fmt.Fprint(w, `{"id":"r","status":"completed","usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}`)
	})
	for _, tc := range []struct {
		path, body string
		stream     bool
	}{
		{"/v1/embeddings", `{"model":"openai","input":"hi"}`, false},
		{"/v1/responses", `{"model":"openai","input":"hi"}`, false},
		{"/v1/responses", `{"model":"openai","input":"hi","stream":true}`, true},
	} {
		t.Run(tc.body, func(t *testing.T) {
			status, h, b := accountingRequest(t, f, tc.path, tc.body, "master-only", true)
			if status != 200 || !bytes.Contains(b, []byte(`"proxy_accounting"`)) {
				t.Fatalf("%d %s", status, b)
			}
			rec := getAccounting(t, f, h.Get("X-LLM-Accounting-ID"), "master-only")
			if rec.State != "final" || *rec.Cost.Amount != "0.05" {
				t.Fatalf("%+v", rec)
			}
			if tc.stream && !bytes.Contains(b, []byte("event: proxy.accounting")) {
				t.Fatalf("%s", b)
			}
		})
	}
}

func TestAccountingStreamingRejectionHasFinalHeaders(t *testing.T) {
	f := newMigrationFixtureWithCostMap(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-litellm-response-cost", "0.25")
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"charged rejection"}}`)
	})
	status, h, _ := accountingRequest(t, f, "/v1/chat/completions", `{"model":"openai","messages":[{"role":"user","content":"hi"}],"stream":true}`, "master-only", false)
	if status != 400 || h.Get("X-LLM-Accounting-State") != "final" || h.Get("X-LLM-Cost-USD") != "0.25" {
		t.Fatalf("status=%d headers=%v", status, h)
	}
}
func TestAccountingHeaderTokensReconcileTPM(t *testing.T) {
	f := newConfiguredMigrationFixture(t, nil, func(cfg *config.Config) { cfg.ModelList[0].TPM = 100 }, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-litellm-response-cost", "0.01")
		w.Header().Set("x-litellm-total-tokens", "100")
		fmt.Fprint(w, accountingReply)
	})
	body := `{"model":"openai","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`
	status, _, _ := accountingRequest(t, f, "/v1/chat/completions", body, "master-only", false)
	if status != 200 {
		t.Fatal(status)
	}
	status, _, _ = accountingRequest(t, f, "/v1/chat/completions", body, "master-only", false)
	if status != 429 {
		t.Fatalf("reported usage not charged to TPM: %d", status)
	}
}

func TestAccountingRemainsReadableAfterBudgetExhaustion(t *testing.T) {
	f := newMigrationFixtureWithCostMap(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-litellm-response-cost", "0.25")
		fmt.Fprint(w, accountingReply)
	})
	team := f.create(t, "/admin/teams", `{"name":"budget-team"}`)
	app := f.create(t, "/admin/applications", fmt.Sprintf(`{"team_id":%v,"name":"app"}`, team["id"]))
	key := f.create(t, fmt.Sprintf("/admin/applications/%v/keys", app["id"]), `{"name":"budget-key","max_budget":0.1}`)
	token := key["key"].(string)
	body := `{"model":"openai","messages":[{"role":"user","content":"hi"}]}`
	status, h, _ := accountingRequest(t, f, "/v1/chat/completions", body, token, false)
	if status != 200 {
		t.Fatal(status)
	}
	status, _, _ = accountingRequest(t, f, "/v1/chat/completions", body, token, false)
	if status != 429 {
		t.Fatalf("budget not enforced: %d", status)
	}
	rec := getAccounting(t, f, h.Get("X-LLM-Accounting-ID"), token)
	if *rec.Cost.Amount != "0.25" {
		t.Fatalf("%+v", rec)
	}
}
