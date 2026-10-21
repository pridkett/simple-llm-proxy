package router

import (
	"context"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestDecisionReloadAndOperationIsolation(t *testing.T) {
	cfg := config.Defaults()
	cfg.ModelList = []config.ModelConfig{{ModelName: "decision", LiteLLMParams: config.LiteLLMParams{Model: "mock/decision", APIBase: "http://localhost:8000/v1"}, Decision: &config.DecisionConfig{Protocol: "systemone", Pricing: &config.DecisionPricing{}}}}
	r, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	deps, exists := r.DecisionDeployments("decision")
	if !exists || len(deps) != 1 || deps[0].Decision.Modalities[0] != "text" {
		t.Fatalf("decision discovery=%+v", deps)
	}
	key := deps[0].DeploymentKey()
	if key == (&provider.Deployment{ProviderName: "mock", ActualModel: "decision", APIBase: "http://localhost:8000/v1"}).DeploymentKey() {
		t.Fatal("generation and decision identities collided")
	}
	called := false
	result := r.Route(context.Background(), "decision", "", func(*provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		called = true
		return &model.ChatCompletionResponse{}, nil, nil
	})
	if result.Error == nil || called {
		t.Fatal("generation route admitted a decision-only model")
	}
	// A failed reload must preserve the live routing table and declarations.
	bad := *cfg
	bad.ModelList = append([]config.ModelConfig{}, cfg.ModelList...)
	bad.ModelList = append(bad.ModelList, config.ModelConfig{ModelName: "decision", LiteLLMParams: config.LiteLLMParams{Model: "mock/generation"}})
	if err := r.Reload(&bad); err == nil {
		t.Fatal("accepted mixed operations under one alias")
	}
	deps, _ = r.DecisionDeployments("decision")
	if len(deps) != 1 || deps[0].DeploymentKey() != key {
		t.Fatal("failed reload mutated live deployments")
	}
	changed := *cfg
	changed.ModelList = append([]config.ModelConfig{}, cfg.ModelList...)
	d := *cfg.ModelList[0].Decision
	d.Protocol = "decisions"
	changed.ModelList[0].Decision = &d
	if err := r.Reload(&changed); err != nil {
		t.Fatal(err)
	}
	deps, _ = r.DecisionDeployments("decision")
	if len(deps) != 1 || deps[0].DeploymentKey() == key {
		t.Fatal("protocol reload did not update identity")
	}
	if cfg.ModelList[0].Decision.Modalities != nil {
		t.Fatal("router normalization mutated caller's configuration")
	}
}

func TestDecisionCapabilityFallbackAcrossPools(t *testing.T) {
	cfg := config.Defaults()
	makeModel := func(name string, modalities []string) config.ModelConfig {
		return config.ModelConfig{ModelName: name, LiteLLMParams: config.LiteLLMParams{Model: "mock/" + name, APIBase: "http://localhost:8000/v1"}, Decision: &config.DecisionConfig{Protocol: "systemone", Modalities: modalities, Pricing: &config.DecisionPricing{}}}
	}
	cfg.ModelList = []config.ModelConfig{makeModel("text", []string{"text"}), makeModel("vision", []string{"text", "image"})}
	cfg.ProviderPools = []config.ProviderPool{
		{Name: "text-first", Members: []config.PoolMember{{ModelName: "text", Weight: 1}}},
		{Name: "vision-fallback", Members: []config.PoolMember{{ModelName: "text", Weight: 1}, {ModelName: "vision", Weight: 1}}},
	}
	r, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// Pin the primary explicitly so the test covers an incompatible first pool.
	r.modelToPool["text"] = r.pools["text-first"]
	result := r.Route(context.Background(), "text", "", func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		if d.ModelName != "vision" {
			t.Fatalf("incompatible deployment attempted: %s", d.ModelName)
		}
		return &model.ChatCompletionResponse{}, nil, nil
	}, RouteOptions{Eligible: func(d *provider.Deployment) bool { return d.ModelName == "vision" }})
	if result.Error != nil || result.PoolName != "vision-fallback" {
		t.Fatalf("capability fallback failed: %+v", result)
	}
}
