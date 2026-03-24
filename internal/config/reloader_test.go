package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfigFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing config file: %v", err)
	}
	return path
}

const baseConfig = `
model_list:
  - model_name: gpt-4
    litellm_params:
      model: openai/gpt-4
      api_key: key-1
    rpm: 100

router_settings:
  routing_strategy: simple-shuffle
  num_retries: 2
  allowed_fails: 3
  cooldown_time: 30s

general_settings:
  master_key: master-1
  port: 8080
`

const updatedConfig = `
model_list:
  - model_name: gpt-4
    litellm_params:
      model: openai/gpt-4
      api_key: key-2
  - model_name: claude-3
    litellm_params:
      model: anthropic/claude-3-sonnet
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

func TestNewReloader(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, baseConfig)

	reloader, err := NewReloader(path)
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}

	cfg := reloader.Config()
	if cfg == nil {
		t.Fatal("Config() returned nil")
	}
	if len(cfg.ModelList) != 1 {
		t.Errorf("Expected 1 model, got %d", len(cfg.ModelList))
	}
	if cfg.ModelList[0].ModelName != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%s'", cfg.ModelList[0].ModelName)
	}
}

func TestNewReloader_InvalidPath(t *testing.T) {
	_, err := NewReloader("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("Expected error for invalid path, got nil")
	}
}

func TestReloader_Config_ReturnsCurrent(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, baseConfig)

	reloader, err := NewReloader(path)
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}

	cfg1 := reloader.Config()
	cfg2 := reloader.Config()
	if cfg1 != cfg2 {
		t.Error("Config() should return the same pointer before reload")
	}
}

func TestReloader_Reload(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, baseConfig)

	reloader, err := NewReloader(path)
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}

	cfgBefore := reloader.Config()
	if len(cfgBefore.ModelList) != 1 {
		t.Fatalf("Expected 1 model before reload, got %d", len(cfgBefore.ModelList))
	}

	// Write new config
	if err := os.WriteFile(path, []byte(updatedConfig), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	newCfg, err := reloader.Reload()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// Returned config should have new values
	if len(newCfg.ModelList) != 2 {
		t.Errorf("Expected 2 models after reload, got %d", len(newCfg.ModelList))
	}
	if newCfg.RouterSettings.RoutingStrategy != "round-robin" {
		t.Errorf("Expected routing strategy 'round-robin', got '%s'", newCfg.RouterSettings.RoutingStrategy)
	}

	// Config() should also return the new config
	cfgAfter := reloader.Config()
	if cfgAfter == cfgBefore {
		t.Error("Config() should return a new pointer after reload")
	}
	if len(cfgAfter.ModelList) != 2 {
		t.Errorf("Expected 2 models from Config() after reload, got %d", len(cfgAfter.ModelList))
	}
}

func TestReloader_Reload_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, baseConfig)

	reloader, err := NewReloader(path)
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}

	cfgBefore := reloader.Config()

	// Write syntactically invalid YAML (unclosed flow sequence)
	if err := os.WriteFile(path, []byte("model_list: [unclosed"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = reloader.Reload()
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}

	// Config should be unchanged after a failed reload
	cfgAfter := reloader.Config()
	if cfgAfter != cfgBefore {
		t.Error("Config() should be unchanged after a failed reload")
	}
}

func TestReloader_Config_IsGetter(t *testing.T) {
	dir := t.TempDir()
	path := writeConfigFile(t, dir, baseConfig)

	reloader, err := NewReloader(path)
	if err != nil {
		t.Fatalf("NewReloader: %v", err)
	}

	// Config method can be used as a func() *Config getter
	getCfg := reloader.Config
	if getCfg() == nil {
		t.Error("Config getter returned nil")
	}
	if getCfg().RouterSettings.RoutingStrategy != "simple-shuffle" {
		t.Errorf("Unexpected routing strategy: %s", getCfg().RouterSettings.RoutingStrategy)
	}
}
