package router

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// mockProvider is a no-op provider used in tests.
type mockProvider struct{ name string }

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) ChatCompletion(_ context.Context, _ *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return nil, nil
}
func (m *mockProvider) ChatCompletionStream(_ context.Context, _ *model.ChatCompletionRequest) (provider.Stream, error) {
	return nil, nil
}
func (m *mockProvider) Embeddings(_ context.Context, _ *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, nil
}
func (m *mockProvider) SupportsEmbeddings() bool { return false }

func init() {
	provider.Register("mock", func(opts provider.ProviderOptions) provider.Provider {
		return &mockProvider{name: "mock"}
	})
}

func TestShuffleStrategy(t *testing.T) {
	s := NewShuffle()

	deployments := []*provider.Deployment{
		{ModelName: "model1"},
		{ModelName: "model2"},
		{ModelName: "model3"},
	}

	// Test that it returns a deployment
	d := s.Select(deployments)
	if d == nil {
		t.Error("Expected a deployment, got nil")
	}

	// Test empty slice
	d = s.Select([]*provider.Deployment{})
	if d != nil {
		t.Error("Expected nil for empty slice")
	}

	// Test single deployment
	single := []*provider.Deployment{{ModelName: "only"}}
	d = s.Select(single)
	if d.ModelName != "only" {
		t.Errorf("Expected 'only', got '%s'", d.ModelName)
	}
}

func TestRoundRobinStrategy(t *testing.T) {
	r := NewRoundRobin()

	deployments := []*provider.Deployment{
		{ModelName: "model1"},
		{ModelName: "model2"},
		{ModelName: "model3"},
	}

	// Should cycle through deployments
	seen := make(map[string]int)
	for i := 0; i < 9; i++ {
		d := r.Select(deployments)
		seen[d.ModelName]++
	}

	// Each should be selected 3 times
	for name, count := range seen {
		if count != 3 {
			t.Errorf("Expected %s to be selected 3 times, got %d", name, count)
		}
	}
}

func makeMockConfig(models []string, strategy string) *config.Config {
	mc := make([]config.ModelConfig, 0, len(models))
	for _, name := range models {
		mc = append(mc, config.ModelConfig{
			ModelName: name,
			LiteLLMParams: config.LiteLLMParams{
				Model:  "mock/" + name,
				APIKey: "test-key",
			},
		})
	}
	return &config.Config{
		ModelList: mc,
		RouterSettings: config.RouterSettings{
			RoutingStrategy: strategy,
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
	}
}

func TestRouterReload_UpdatesDeployments(t *testing.T) {
	cfg := makeMockConfig([]string{"model-a"}, "simple-shuffle")
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// model-a should be available
	if _, err := r.GetDeployment("model-a"); err != nil {
		t.Errorf("Expected model-a before reload: %v", err)
	}
	// model-b should not exist yet
	if _, err := r.GetDeployment("model-b"); err == nil {
		t.Error("Expected error for model-b before reload")
	}

	// Reload with model-b, removing model-a
	newCfg := makeMockConfig([]string{"model-b"}, "simple-shuffle")
	if err := r.Reload(newCfg); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// model-b should now be available
	if _, err := r.GetDeployment("model-b"); err != nil {
		t.Errorf("Expected model-b after reload: %v", err)
	}
	// model-a should be gone
	if _, err := r.GetDeployment("model-a"); err == nil {
		t.Error("Expected error for model-a after reload")
	}
}

func TestRouterReload_UpdatesSettings(t *testing.T) {
	cfg := makeMockConfig([]string{"model-a"}, "simple-shuffle")
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if r.Settings().RoutingStrategy != "simple-shuffle" {
		t.Errorf("Expected simple-shuffle, got %s", r.Settings().RoutingStrategy)
	}
	if r.NumRetries() != 2 {
		t.Errorf("Expected 2 retries, got %d", r.NumRetries())
	}

	newCfg := makeMockConfig([]string{"model-a"}, "round-robin")
	newCfg.RouterSettings.NumRetries = 5
	if err := r.Reload(newCfg); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if r.Settings().RoutingStrategy != "round-robin" {
		t.Errorf("Expected round-robin after reload, got %s", r.Settings().RoutingStrategy)
	}
	if r.NumRetries() != 5 {
		t.Errorf("Expected 5 retries after reload, got %d", r.NumRetries())
	}
}

func TestRouterReload_ResetsCooldown(t *testing.T) {
	cfg := makeMockConfig([]string{"model-a"}, "simple-shuffle")
	cfg.RouterSettings.AllowedFails = 1
	cfg.RouterSettings.CooldownTime = 10 * time.Minute

	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	d, _ := r.GetDeployment("model-a")
	r.ReportFailure(d)
	// Should be in cooldown now (allowed_fails=1)
	status := r.GetStatus()
	if len(status) == 0 || status[0].HealthyDeployments != 0 {
		t.Error("Expected deployment in cooldown before reload")
	}

	// Reload resets cooldown state
	newCfg := makeMockConfig([]string{"model-a"}, "simple-shuffle")
	newCfg.RouterSettings.AllowedFails = 3
	newCfg.RouterSettings.CooldownTime = 30 * time.Second
	if err := r.Reload(newCfg); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	status = r.GetStatus()
	if len(status) == 0 || status[0].HealthyDeployments != 1 {
		t.Error("Expected deployment healthy after reload")
	}
}

func TestRouterReload_EmptyModels(t *testing.T) {
	cfg := makeMockConfig([]string{"model-a"}, "simple-shuffle")
	r, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Reload with no models
	newCfg := makeMockConfig([]string{}, "simple-shuffle")
	if err := r.Reload(newCfg); err != nil {
		t.Fatalf("Reload with empty models: %v", err)
	}

	if _, err := r.GetDeployment("model-a"); err == nil {
		t.Error("Expected error for model-a after reload with empty config")
	}
}

func TestValidatePoolMembers(t *testing.T) {
	t.Run("valid pool member reference succeeds", func(t *testing.T) {
		cfg := makeMockConfig([]string{"gpt-4-primary", "gpt-4-fallback"}, "simple-shuffle")
		cfg.ProviderPools = []config.ProviderPool{
			{
				Name:     "gpt-4",
				Strategy: "weighted-round-robin",
				Members: []config.PoolMember{
					{ModelName: "gpt-4-primary", Weight: 80},
					{ModelName: "gpt-4-fallback", Weight: 20},
				},
			},
		}
		_, err := New(cfg)
		if err != nil {
			t.Errorf("expected no error for valid pool members, got: %v", err)
		}
	})

	t.Run("unknown pool member causes startup failure", func(t *testing.T) {
		cfg := makeMockConfig([]string{"gpt-4-primary"}, "simple-shuffle")
		cfg.ProviderPools = []config.ProviderPool{
			{
				Name:     "gpt-4",
				Strategy: "weighted-round-robin",
				Members: []config.PoolMember{
					{ModelName: "gpt-4-primary", Weight: 80},
					{ModelName: "gpt-4-fallback", Weight: 20}, // not in model_list
				},
			},
		}
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for unknown pool member, got nil")
		}
		if !strings.Contains(err.Error(), "not found in model_list") {
			t.Errorf("expected error to contain 'not found in model_list', got: %v", err)
		}
		if !strings.Contains(err.Error(), "gpt-4-fallback") {
			t.Errorf("expected error to name the missing model, got: %v", err)
		}
	})

	t.Run("no provider_pools is valid (backward compat)", func(t *testing.T) {
		cfg := makeMockConfig([]string{"gpt-4"}, "simple-shuffle")
		// cfg.ProviderPools is nil — must not error
		_, err := New(cfg)
		if err != nil {
			t.Errorf("expected no error when provider_pools absent, got: %v", err)
		}
	})
}

func TestValidateWebhookStartup(t *testing.T) {
	t.Run("valid webhook config succeeds", func(t *testing.T) {
		cfg := makeMockConfig([]string{"gpt-4"}, "simple-shuffle")
		cfg.Webhooks = []config.WebhookConfig{
			{
				URL:     "https://example.com/webhook",
				Events:  []string{"budget_exhausted"},
				Enabled: true,
			},
		}
		_, err := New(cfg)
		if err != nil {
			t.Errorf("expected no error for valid webhook, got: %v", err)
		}
	})

	t.Run("empty url is rejected", func(t *testing.T) {
		cfg := makeMockConfig([]string{"gpt-4"}, "simple-shuffle")
		cfg.Webhooks = []config.WebhookConfig{
			{
				URL:    "", // empty
				Events: []string{"budget_exhausted"},
			},
		}
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for empty webhook url, got nil")
		}
		if !strings.Contains(err.Error(), "url is required") {
			t.Errorf("expected error to contain 'url is required', got: %v", err)
		}
	})

	t.Run("empty events slice is rejected", func(t *testing.T) {
		cfg := makeMockConfig([]string{"gpt-4"}, "simple-shuffle")
		cfg.Webhooks = []config.WebhookConfig{
			{
				URL:    "https://example.com/webhook",
				Events: []string{}, // empty
			},
		}
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for empty webhook events, got nil")
		}
		if !strings.Contains(err.Error(), "events is required and must not be empty") {
			t.Errorf("expected error to contain 'events is required', got: %v", err)
		}
	})

	t.Run("no webhooks is valid (backward compat)", func(t *testing.T) {
		cfg := makeMockConfig([]string{"gpt-4"}, "simple-shuffle")
		// cfg.Webhooks is nil — must not error
		_, err := New(cfg)
		if err != nil {
			t.Errorf("expected no error when webhooks absent, got: %v", err)
		}
	})
}

func TestCooldownManager(t *testing.T) {
	cm := NewCooldownManager(100*time.Millisecond, 2)

	d := &provider.Deployment{ModelName: "test"}

	// Initially not in cooldown
	if cm.InCooldown(d) {
		t.Error("Expected not in cooldown initially")
	}

	// First failure - not in cooldown yet
	cm.ReportFailure(d)
	if cm.InCooldown(d) {
		t.Error("Expected not in cooldown after 1 failure")
	}

	// Second failure - should be in cooldown
	cm.ReportFailure(d)
	if !cm.InCooldown(d) {
		t.Error("Expected in cooldown after 2 failures")
	}

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)
	if cm.InCooldown(d) {
		t.Error("Expected cooldown to expire")
	}

	// Report success resets failures
	cm.ReportFailure(d)
	cm.ReportSuccess(d)
	cm.ReportFailure(d)
	if cm.InCooldown(d) {
		t.Error("Expected not in cooldown after success reset")
	}
}
