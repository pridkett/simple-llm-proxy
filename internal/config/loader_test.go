package config

import (
	"os"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	yaml := `
model_list:
  - model_name: gpt-4
    litellm_params:
      model: openai/gpt-4
      api_key: test-key
    rpm: 100

  - model_name: claude-3
    litellm_params:
      model: anthropic/claude-3-sonnet
      api_key: another-key
    rpm: 50

router_settings:
  routing_strategy: round-robin
  num_retries: 3
  allowed_fails: 5
  cooldown_time: 60s

general_settings:
  master_key: my-secret-key
  database_url: ./test.db
  port: 9090
`

	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Check model_list
	if len(cfg.ModelList) != 2 {
		t.Errorf("Expected 2 models, got %d", len(cfg.ModelList))
	}

	if cfg.ModelList[0].ModelName != "gpt-4" {
		t.Errorf("Expected model name 'gpt-4', got '%s'", cfg.ModelList[0].ModelName)
	}

	if cfg.ModelList[0].LiteLLMParams.Model != "openai/gpt-4" {
		t.Errorf("Expected model 'openai/gpt-4', got '%s'", cfg.ModelList[0].LiteLLMParams.Model)
	}

	if cfg.ModelList[0].RPM != 100 {
		t.Errorf("Expected RPM 100, got %d", cfg.ModelList[0].RPM)
	}

	// Check router_settings
	if cfg.RouterSettings.RoutingStrategy != "round-robin" {
		t.Errorf("Expected routing strategy 'round-robin', got '%s'", cfg.RouterSettings.RoutingStrategy)
	}

	if cfg.RouterSettings.NumRetries != 3 {
		t.Errorf("Expected num_retries 3, got %d", cfg.RouterSettings.NumRetries)
	}

	if cfg.RouterSettings.CooldownTime != 60*time.Second {
		t.Errorf("Expected cooldown_time 60s, got %v", cfg.RouterSettings.CooldownTime)
	}

	// Check general_settings
	if cfg.GeneralSettings.MasterKey != "my-secret-key" {
		t.Errorf("Expected master key 'my-secret-key', got '%s'", cfg.GeneralSettings.MasterKey)
	}

	if cfg.GeneralSettings.Port != 9090 {
		t.Errorf("Expected port 9090, got %d", cfg.GeneralSettings.Port)
	}
}

func TestParseEnvExpansion(t *testing.T) {
	os.Setenv("TEST_API_KEY", "secret-from-env")
	os.Setenv("TEST_MASTER_KEY", "master-from-env")
	defer os.Unsetenv("TEST_API_KEY")
	defer os.Unsetenv("TEST_MASTER_KEY")

	yaml := `
model_list:
  - model_name: test-model
    litellm_params:
      model: openai/gpt-4
      api_key: os.environ/TEST_API_KEY

general_settings:
  master_key: os.environ/TEST_MASTER_KEY
`

	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if cfg.ModelList[0].LiteLLMParams.APIKey != "secret-from-env" {
		t.Errorf("Expected expanded API key 'secret-from-env', got '%s'", cfg.ModelList[0].LiteLLMParams.APIKey)
	}

	if cfg.GeneralSettings.MasterKey != "master-from-env" {
		t.Errorf("Expected expanded master key 'master-from-env', got '%s'", cfg.GeneralSettings.MasterKey)
	}
}

func TestParseModelString(t *testing.T) {
	tests := []struct {
		input    string
		provider string
		model    string
	}{
		{"openai/gpt-4", "openai", "gpt-4"},
		{"anthropic/claude-3-sonnet", "anthropic", "claude-3-sonnet"},
		{"gpt-4", "openai", "gpt-4"}, // Default to openai
		{"azure/gpt-4-turbo", "azure", "gpt-4-turbo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed := ParseModelString(tt.input)
			if parsed.Provider != tt.provider {
				t.Errorf("Expected provider '%s', got '%s'", tt.provider, parsed.Provider)
			}
			if parsed.ModelName != tt.model {
				t.Errorf("Expected model '%s', got '%s'", tt.model, parsed.ModelName)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	if cfg.RouterSettings.RoutingStrategy != "simple-shuffle" {
		t.Errorf("Expected default strategy 'simple-shuffle', got '%s'", cfg.RouterSettings.RoutingStrategy)
	}

	if cfg.RouterSettings.NumRetries != 2 {
		t.Errorf("Expected default num_retries 2, got %d", cfg.RouterSettings.NumRetries)
	}

	if cfg.GeneralSettings.Port != 8080 {
		t.Errorf("Expected default port 8080, got %d", cfg.GeneralSettings.Port)
	}
}

func TestParseProviderPools(t *testing.T) {
	yaml := `
model_list:
  - model_name: gpt-4-primary
    litellm_params:
      model: openai/gpt-4
      api_key: key-a
  - model_name: gpt-4-fallback
    litellm_params:
      model: openai/gpt-4
      api_key: key-b

provider_pools:
  - name: gpt-4
    strategy: weighted-round-robin
    budget_cap_daily: 50.00
    members:
      - model_name: gpt-4-primary
        weight: 80
      - model_name: gpt-4-fallback
        weight: 20
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(cfg.ProviderPools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(cfg.ProviderPools))
	}

	pool := cfg.ProviderPools[0]
	if pool.Name != "gpt-4" {
		t.Errorf("expected pool name 'gpt-4', got %q", pool.Name)
	}
	if pool.Strategy != "weighted-round-robin" {
		t.Errorf("expected strategy 'weighted-round-robin', got %q", pool.Strategy)
	}
	if pool.BudgetCapDaily != 50.00 {
		t.Errorf("expected BudgetCapDaily 50.00, got %f", pool.BudgetCapDaily)
	}
	if len(pool.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(pool.Members))
	}
	if pool.Members[0].ModelName != "gpt-4-primary" {
		t.Errorf("expected first member 'gpt-4-primary', got %q", pool.Members[0].ModelName)
	}
	if pool.Members[0].Weight != 80 {
		t.Errorf("expected first member weight 80, got %d", pool.Members[0].Weight)
	}
	if pool.Members[1].Weight != 20 {
		t.Errorf("expected second member weight 20, got %d", pool.Members[1].Weight)
	}
}

func TestParseProviderPoolsAbsent(t *testing.T) {
	yaml := `
model_list:
  - model_name: gpt-4
    litellm_params:
      model: openai/gpt-4
      api_key: key-a
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.ProviderPools != nil {
		t.Errorf("expected nil ProviderPools when section absent, got %v", cfg.ProviderPools)
	}
}

func TestParsePoolMemberDefaultWeight(t *testing.T) {
	yaml := `
model_list:
  - model_name: gpt-4-primary
    litellm_params:
      model: openai/gpt-4
      api_key: key-a

provider_pools:
  - name: gpt-4
    members:
      - model_name: gpt-4-primary
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.ProviderPools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(cfg.ProviderPools))
	}
	if cfg.ProviderPools[0].Members[0].Weight != 1 {
		t.Errorf("expected default weight 1, got %d", cfg.ProviderPools[0].Members[0].Weight)
	}
}

func TestParseWebhooks(t *testing.T) {
	os.Setenv("TEST_WEBHOOK_SECRET", "test-secret-value")
	defer os.Unsetenv("TEST_WEBHOOK_SECRET")

	yaml := `
webhooks:
  - url: https://example.com/webhook
    events: [budget_exhausted, provider_failover]
    secret: os.environ/TEST_WEBHOOK_SECRET
    enabled: true
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(cfg.Webhooks) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(cfg.Webhooks))
	}

	wh := cfg.Webhooks[0]
	if wh.URL != "https://example.com/webhook" {
		t.Errorf("expected URL 'https://example.com/webhook', got %q", wh.URL)
	}
	if len(wh.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(wh.Events))
	}
	if wh.Secret != "test-secret-value" {
		t.Errorf("expected secret 'test-secret-value' (expanded), got %q", wh.Secret)
	}
	if !wh.Enabled {
		t.Errorf("expected enabled=true")
	}
}

func TestParseWebhooksAbsent(t *testing.T) {
	yaml := `
model_list:
  - model_name: gpt-4
    litellm_params:
      model: openai/gpt-4
      api_key: key-a
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Webhooks != nil {
		t.Errorf("expected nil Webhooks when section absent, got %v", cfg.Webhooks)
	}
}
