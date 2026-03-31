package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
)

// testProvider is a no-op provider used in handler tests.
type testProvider struct{ name string }

func (p *testProvider) Name() string { return p.name }
func (p *testProvider) ChatCompletion(_ context.Context, _ *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return nil, nil
}
func (p *testProvider) ChatCompletionStream(_ context.Context, _ *model.ChatCompletionRequest) (provider.Stream, error) {
	return nil, nil
}
func (p *testProvider) Embeddings(_ context.Context, _ *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, nil
}
func (p *testProvider) SupportsEmbeddings() bool { return false }

func init() {
	// Register mock providers so handler tests can create routers with real config.
	for _, name := range []string{"openai", "anthropic"} {
		n := name
		provider.Register(n, func(opts provider.ProviderOptions) provider.Provider {
			return &testProvider{name: n}
		})
	}
}

// configForTest builds a minimal *config.Config suitable for handler tests.
func configForTest() *config.Config {
	return &config.Config{
		ModelList: []config.ModelConfig{
			{
				ModelName: "gpt-4",
				LiteLLMParams: config.LiteLLMParams{
					Model:  "openai/gpt-4",
					APIKey: "test-key",
				},
				RPM: 100,
			},
		},
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		GeneralSettings: config.GeneralSettings{
			MasterKey:   "master-key",
			DatabaseURL: "./test.db",
			Port:        8080,
		},
	}
}

func TestAdminStatus(t *testing.T) {
	r, err := router.New(&config.Config{
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	startTime := time.Now().Add(-5 * time.Second)
	req := httptest.NewRequest(http.MethodGet, "/admin/status", nil)
	rr := httptest.NewRecorder()

	AdminStatus(r, startTime)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var resp adminStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if resp.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", resp.Status)
	}
	if resp.UptimeSeconds < 5 {
		t.Errorf("Expected uptime >= 5s, got %d", resp.UptimeSeconds)
	}
}

func TestAdminConfig_ReturnsSanitizedConfig(t *testing.T) {
	cfg := configForTest()
	getCfg := func() *config.Config { return cfg }

	req := httptest.NewRequest(http.MethodGet, "/admin/config", nil)
	rr := httptest.NewRecorder()

	AdminConfig(getCfg)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	var resp adminConfigResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(resp.ModelList) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(resp.ModelList))
	}
	if resp.ModelList[0].ModelName != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%s'", resp.ModelList[0].ModelName)
	}
	if !resp.ModelList[0].APIKeySet {
		t.Error("Expected APIKeySet to be true")
	}
	if resp.GeneralSettings.MasterKeySet != true {
		t.Error("Expected MasterKeySet to be true")
	}
	// Master key must not be returned
	raw := rr.Body.String()
	if contains(raw, "master-key") {
		t.Error("Master key must not be returned in config response")
	}
}

func TestAdminConfig_ReflectsReloadedConfig(t *testing.T) {
	cfg := configForTest()
	getCfg := func() *config.Config { return cfg }

	req := httptest.NewRequest(http.MethodGet, "/admin/config", nil)
	rr := httptest.NewRecorder()
	AdminConfig(getCfg)(rr, req)

	var resp1 adminConfigResponse
	json.NewDecoder(rr.Body).Decode(&resp1)
	if len(resp1.ModelList) != 1 {
		t.Fatalf("Expected 1 model initially")
	}

	// Simulate reload: replace the config pointer
	newCfg := configForTest()
	newCfg.ModelList = append(newCfg.ModelList, config.ModelConfig{
		ModelName:     "claude-3",
		LiteLLMParams: config.LiteLLMParams{Model: "anthropic/claude-3-sonnet"},
	})
	cfg = newCfg
	// getCfg closure now returns newCfg

	req2 := httptest.NewRequest(http.MethodGet, "/admin/config", nil)
	rr2 := httptest.NewRecorder()
	AdminConfig(getCfg)(rr2, req2)

	var resp2 adminConfigResponse
	json.NewDecoder(rr2.Body).Decode(&resp2)
	if len(resp2.ModelList) != 2 {
		t.Errorf("Expected 2 models after config update, got %d", len(resp2.ModelList))
	}
}

func TestAdminReload_Success(t *testing.T) {
	// Write an initial config file
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	initialYAML := `
model_list:
  - model_name: gpt-4
    litellm_params:
      model: openai/gpt-4
      api_key: key-1

router_settings:
  routing_strategy: simple-shuffle
  num_retries: 2
  allowed_fails: 3
  cooldown_time: 30s

general_settings:
  master_key: master-1
  port: 8080
`
	if err := os.WriteFile(path, []byte(initialYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reloader, err := config.NewReloader(path)
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}

	r, err := router.New(reloader.Config())
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	// Update file before calling reload
	updatedYAML := `
model_list:
  - model_name: gpt-4
    litellm_params:
      model: openai/gpt-4
      api_key: key-2
  - model_name: gpt-3
    litellm_params:
      model: openai/gpt-3
      api_key: key-3

router_settings:
  routing_strategy: round-robin
  num_retries: 5
  allowed_fails: 1
  cooldown_time: 60s

general_settings:
  master_key: master-1
  port: 8080
`
	if err := os.WriteFile(path, []byte(updatedYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/reload", nil)
	rr := httptest.NewRecorder()

	AdminReload(reloader, r)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp reloadResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", resp.Status)
	}

	// Reloader should now return the new config
	cfg := reloader.Config()
	if len(cfg.ModelList) != 2 {
		t.Errorf("Expected 2 models after reload, got %d", len(cfg.ModelList))
	}
	if cfg.RouterSettings.RoutingStrategy != "round-robin" {
		t.Errorf("Expected round-robin after reload, got %s", cfg.RouterSettings.RoutingStrategy)
	}

	// Router should reflect the new settings
	if r.Settings().NumRetries != 5 {
		t.Errorf("Expected 5 retries after reload, got %d", r.Settings().NumRetries)
	}
}

func TestAdminReload_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	initialYAML := `
model_list: []
router_settings:
  routing_strategy: simple-shuffle
  num_retries: 2
  allowed_fails: 3
  cooldown_time: 30s
general_settings:
  port: 8080
`
	if err := os.WriteFile(path, []byte(initialYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reloader, err := config.NewReloader(path)
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}

	r, err := router.New(reloader.Config())
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	// Overwrite with syntactically invalid YAML (unclosed flow sequence)
	if err := os.WriteFile(path, []byte("model_list: [unclosed"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/reload", nil)
	rr := httptest.NewRecorder()

	AdminReload(reloader, r)(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for invalid file, got %d", rr.Code)
	}
}

// contains is a simple substring check used to verify secrets are not leaked.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
