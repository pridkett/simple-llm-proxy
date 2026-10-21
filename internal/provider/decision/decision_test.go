package decision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func transportRequest(t *testing.T) *model.DecisionRequest {
	t.Helper()
	var fields map[string]json.RawMessage
	json.Unmarshal([]byte(`{"model":"wire","input":"ticket","prompt_cache_key":"cache-key","questions":[{"type":"predicate","instructions":"Urgent?"}],"extension":{"kept":true}}`), &fields)
	r, err := model.ParseDecisionRequest(fields, model.DecisionsFormat)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDecisionTransportNativeFieldsAndErrors(t *testing.T) {
	var status atomic.Int64
	status.Store(200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/decisions" || r.Header.Get("Authorization") != "Bearer upstream" || r.Header.Get("X-Deployment") != "configured" {
			t.Errorf("bad upstream request: %s %+v", r.URL, r.Header)
		}
		var fields map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&fields)
		if fields["extension"] == nil || fields["prompt_cache_key"] == nil {
			t.Error("native fields were dropped")
		}
		w.Header().Set("Retry-After", "9")
		w.WriteHeader(int(status.Load()))
		if status.Load() != 200 {
			fmt.Fprint(w, `{"error":{"message":"provider rejection"}}`)
			return
		}
		fmt.Fprint(w, `{"model":"wire","answers":[{"type":"predicate","probability":0.7}],"usage":{"input_tokens":0},"extension":{"kept":true}}`)
	}))
	defer server.Close()
	p, err := New("openai", provider.ProviderOptions{APIKey: "upstream", APIBase: server.URL + "/v1/", ExtraHeaders: map[string]string{"X-Deployment": "configured"}}, &config.DecisionConfig{Protocol: "decisions", Pricing: &config.DecisionPricing{}})
	if err != nil {
		t.Fatal(err)
	}
	req := transportRequest(t)
	response, err := p.Decide(context.Background(), req)
	if err != nil || response.Fields["extension"] == nil || response.Usage == nil {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	for _, code := range []int{400, 422, 429, 500} {
		status.Store(int64(code))
		_, err := p.Decide(context.Background(), req)
		var httpErr interface{ HTTPStatus() int }
		if !errors.As(err, &httpErr) || httpErr.HTTPStatus() != code {
			t.Fatalf("status lost: %v", err)
		}
		if code == 429 {
			var limited *provider.RateLimitError
			if !errors.As(err, &limited) || limited.RetryAfter.Seconds() != 9 {
				t.Fatalf("Retry-After lost: %v", err)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Decide(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestDecisionTransportDoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	p, err := New("mock", provider.ProviderOptions{APIKey: "secret", APIBase: redirect.URL}, &config.DecisionConfig{Protocol: "decisions", Pricing: &config.DecisionPricing{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Decide(context.Background(), transportRequest(t)); err == nil || leaked.Load() {
		t.Fatal("redirect was followed or accepted")
	}
}

func TestDecisionTransportConfiguration(t *testing.T) {
	for _, base := range []string{"", "file:///tmp/socket", "https://user:password@example.test", "https://example.test?secret=true"} {
		if _, err := New("custom", provider.ProviderOptions{APIBase: base}, &config.DecisionConfig{Protocol: "systemone", Pricing: &config.DecisionPricing{}}); err == nil {
			t.Fatalf("accepted unsafe or missing origin: %q", base)
		}
	}
	if _, err := New("cloudflare", provider.ProviderOptions{APIBase: "https://example.test/ai/run"}, &config.DecisionConfig{Protocol: "cloudflare", Pricing: &config.DecisionPricing{}}); err == nil {
		t.Fatal("Cloudflare model path must be explicit")
	}
}
