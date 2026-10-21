package accounting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

func beginTest(policy string) (*Collector, context.Context) {
	c := New("request", nil)
	cm := costmap.New()
	cm.SetCustomSpec("m", costmap.ModelSpec{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, CacheReadInputTokenCost: 0.0001, CacheCreationInputTokenCost: 0.00125})
	return c, c.Begin(context.Background(), "openai", "m", "m", "d", "pool", policy, cm)
}
func TestReportedCostPrecedence(t *testing.T) {
	for _, policy := range []string{"prefer_upstream", "estimate_only", "require_upstream"} {
		t.Run(policy, func(t *testing.T) {
			c, ctx := beginTest(policy)
			ObserveHeaders(ctx, http.Header{"X-Litellm-Response-Cost": []string{"0"}, "X-Litellm-Total-Tokens": []string{"12"}})
			ObserveUsage(ctx, &model.Usage{PromptTokens: 10, CompletionTokens: 2})
			EndAttempt(ctx, nil)
			r := c.Snapshot(true)
			want := "0"
			source := "upstream"
			if policy == "estimate_only" {
				want = "0.014"
				source = "estimate"
			}
			if r.Cost.Amount == nil || *r.Cost.Amount != want || r.Cost.Source != source || *r.Cost.EstimatedAmount != "0.014" || r.Coverage != "complete" {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestInvalidAndMissingEvidence(t *testing.T) {
	for _, v := range []string{"NaN", "Inf", "-1", "junk", "1e999999999", "0.0000000000000000000000000000000000001"} {
		t.Run(v, func(t *testing.T) {
			c, ctx := beginTest("")
			ObserveHeaders(ctx, http.Header{"X-Litellm-Response-Cost": []string{v}})
			ObserveUsage(ctx, &model.Usage{PromptTokens: 1})
			r := c.Snapshot(true)
			if r.Cost.Source != "estimate" || len(r.Attempts[0].Warnings) == 0 {
				t.Fatalf("%+v", r)
			}
		})
	}
	c, ctx := beginTest("require_upstream")
	ObserveUsage(ctx, &model.Usage{PromptTokens: 1})
	r := c.Snapshot(true)
	if r.Cost.Amount != nil || r.Coverage != "unknown" {
		t.Fatalf("%+v", r)
	}
	c, ctx = beginTest("")
	ObserveHeaders(ctx, http.Header{"X-Litellm-Response-Cost": []string{".25"}, "X-Litellm-Total-Tokens": []string{"100"}})
	r = c.Snapshot(true)
	if r.Usage.InputTokens != nil || *r.Usage.TotalTokens != 100 || *r.Cost.Amount != "0.25" {
		t.Fatalf("%+v", r)
	}
}
func TestCacheAndReasoningNormalization(t *testing.T) {
	c, ctx := beginTest("")
	var u model.Usage
	json.Unmarshal([]byte(`{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":10}}`), &u)
	ObserveUsage(ctx, &u)
	r := c.Snapshot(true)
	if *r.Cost.Amount != "0.104" || *r.Usage.TotalTokens != 120 || *r.Usage.OutputDetails.ReasoningTokens != 10 {
		t.Fatalf("%+v", r)
	}
	cm := costmap.New()
	cm.SetCustomSpec("m", costmap.ModelSpec{InputCostPerToken: 0.001, OutputCostPerToken: 0.002, CacheReadInputTokenCost: 0.0001, CacheCreationInputTokenCost: 0.00125})
	c = New("anthropic", nil)
	ctx = c.Begin(context.Background(), "anthropic", "m", "m", "d", "", "", cm)
	ObserveUsage(ctx, &model.Usage{PromptTokens: 60, CompletionTokens: 20, CacheReadTokens: 40, CacheWriteTokens: 10})
	r = c.Snapshot(true)
	if *r.Usage.InputTokens != 110 || *r.Usage.TotalTokens != 130 || *r.Cost.Amount != "0.1165" {
		t.Fatalf("%+v", r)
	}
}
func TestAttemptsRetainFailedSpend(t *testing.T) {
	c, ctx := beginTest("")
	ObserveHeaders(ctx, http.Header{"X-Litellm-Response-Cost": []string{"0.2"}})
	EndAttempt(ctx, errors.New("failed"))
	ctx = c.Begin(context.Background(), "openai", "m", "m", "d2", "pool2", "", nil)
	ObserveHeaders(ctx, http.Header{"X-Litellm-Response-Cost": []string{"0.1"}})
	EndAttempt(ctx, nil)
	r := c.Snapshot(true)
	if *r.Cost.Amount != "0.3" || r.Attempts[0].Status != "failed" || r.Coverage != "complete" {
		t.Fatalf("%+v", r)
	}
	c.Begin(context.Background(), "openai", "m", "m", "d3", "", "", nil)
	r = c.Snapshot(true)
	if *r.Cost.Amount != "0.3" || r.Coverage != "partial" || r.Usage.TotalTokens != nil {
		t.Fatalf("%+v", r)
	}
}
func TestOpenRouterBodyAndHeaderPrecedence(t *testing.T) {
	c := New("r", nil)
	ctx := c.Begin(context.Background(), "openrouter", "m", "m", "d", "", "", nil)
	ObserveJSON(ctx, []byte(`{"usage":{"cost":0.000415}}`))
	r := c.Snapshot(true)
	if r.Usage.TotalTokens != nil || *r.Cost.Amount != "0.000415" {
		t.Fatalf("%+v", r)
	}
	ObserveHeaders(ctx, http.Header{"X-Litellm-Response-Cost": []string{"0.01"}})
	ObserveJSON(ctx, []byte(`{"usage":{"cost":0.02,"prompt_tokens":1,"completion_tokens":2}}`))
	r = c.Snapshot(true)
	if *r.Cost.Amount != "0.01" {
		t.Fatalf("%+v", r)
	}
}
func TestDecimalExactArithmetic(t *testing.T) {
	for _, v := range []string{"0", "0.000000000000001", "1e-12", "1.50"} {
		if _, err := Decimal(v); err != nil {
			t.Fatal(v, err)
		}
	}
	if got := sum("0.1", "0.2"); got != "0.3" {
		t.Fatal(got)
	}
	if got := multiply("0.000000000001", 1000000000); got != "0.001" {
		t.Fatal(got)
	}
}

func TestPartialTokenFieldsRemainUnknown(t *testing.T) {
	c := New("r", nil)
	ctx := c.Begin(context.Background(), "openrouter", "m", "m", "d", "", "", nil)
	var usage model.Usage
	json.Unmarshal([]byte(`{"cost":0.25}`), &usage)
	ObserveUsage(ctx, &usage)
	rec := c.Snapshot(true)
	if rec.Usage.InputTokens != nil || rec.Usage.OutputTokens != nil || rec.Usage.TotalTokens != nil || *rec.Cost.Amount != "0.25" {
		t.Fatalf("%+v", rec)
	}
	c, ctx = beginTest("")
	json.Unmarshal([]byte(`{"prompt_tokens":10}`), &usage)
	ObserveUsage(ctx, &usage)
	rec = c.Snapshot(true)
	if rec.Usage.OutputTokens != nil || rec.Cost.Amount != nil {
		t.Fatalf("missing output should not be a known zero: %+v", rec)
	}
}
func TestMissingPricesAreNotFree(t *testing.T) {
	var spec costmap.ModelSpec
	json.Unmarshal([]byte(`{"input_cost_per_token":0.001}`), &spec)
	cm := costmap.New()
	cm.SetCustomSpec("m", spec)
	c := New("r", nil)
	ctx := c.Begin(context.Background(), "openai", "m", "m", "d", "", "", cm)
	ObserveUsage(ctx, &model.Usage{PromptTokens: 10, CompletionTokens: 5})
	if rec := c.Snapshot(true); rec.Cost.Amount != nil {
		t.Fatalf("missing output rate: %+v", rec)
	}
	json.Unmarshal([]byte(`{"input_cost_per_token":0,"output_cost_per_token":0}`), &spec)
	cm.SetCustomSpec("m", spec)
	c = New("r", nil)
	ctx = c.Begin(context.Background(), "openai", "m", "m", "d", "", "", cm)
	ObserveUsage(ctx, &model.Usage{PromptTokens: 10, CompletionTokens: 5})
	if rec := c.Snapshot(true); rec.Cost.Amount == nil || *rec.Cost.Amount != "0" {
		t.Fatalf("known free rates: %+v", rec)
	}
}

func TestNativeAnthropicUsageSurvivesTranslation(t *testing.T) {
	c := New("r", nil)
	ctx := c.Begin(context.Background(), "anthropic", "m", "m", "d", "", "", nil)
	ObserveJSON(ctx, []byte(`{"message":{"usage":{"input_tokens":10,"cache_read_input_tokens":50}}}`))
	ObserveJSON(ctx, []byte(`{"usage":{"output_tokens":3}}`))
	ObserveUsage(ctx, &model.Usage{PromptTokens: 10, CompletionTokens: 3, CacheReadTokens: 50})
	rec := c.Snapshot(true)
	var native map[string]int
	json.Unmarshal(rec.Attempts[0].NativeUsage, &native)
	if native["input_tokens"] != 10 || native["cache_read_input_tokens"] != 50 || native["output_tokens"] != 3 || *rec.Usage.InputTokens != 60 {
		t.Fatalf("native=%v record=%+v", native, rec)
	}
}
