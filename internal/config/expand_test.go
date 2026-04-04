package config

import (
	"context"
	"fmt"
	"testing"
)

func TestExpandWildcardsNoProviders(t *testing.T) {
	cfg := &Config{
		ModelList: []ModelConfig{
			{ModelName: "gpt-4", LiteLLMParams: LiteLLMParams{Model: "openai/gpt-4", APIKey: "key"}},
		},
	}

	err := ExpandWildcards(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.ModelList) != 1 {
		t.Errorf("expected 1 model, got %d", len(cfg.ModelList))
	}
}

func TestExpandWildcardsPassthroughNonWildcard(t *testing.T) {
	cfg := &Config{
		ModelList: []ModelConfig{
			{ModelName: "gpt-4", LiteLLMParams: LiteLLMParams{Model: "openai/gpt-4", APIKey: "key"}},
			{ModelName: "claude-3", LiteLLMParams: LiteLLMParams{Model: "anthropic/claude-3", APIKey: "key2"}},
		},
	}

	providers := []DiscoveryProvider{
		{
			Prefix:     "openrouter",
			IsWildcard: func(mc ModelConfig) bool { return mc.LiteLLMParams.Model == "openrouter/*" },
			Discover:   func(ctx context.Context, apiKey, apiBase string, template ModelConfig) ([]ModelConfig, error) { return nil, nil },
		},
	}

	err := ExpandWildcards(context.Background(), cfg, providers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.ModelList) != 2 {
		t.Errorf("expected 2 models unchanged, got %d", len(cfg.ModelList))
	}
	if cfg.ModelList[0].ModelName != "gpt-4" {
		t.Errorf("expected first model 'gpt-4', got %q", cfg.ModelList[0].ModelName)
	}
	if cfg.ModelList[1].ModelName != "claude-3" {
		t.Errorf("expected second model 'claude-3', got %q", cfg.ModelList[1].ModelName)
	}
}

func TestExpandWildcardsExpandsWildcard(t *testing.T) {
	cfg := &Config{
		ModelList: []ModelConfig{
			{ModelName: "gpt-4", LiteLLMParams: LiteLLMParams{Model: "openai/gpt-4", APIKey: "openai-key"}},
			{
				ModelName: "openrouter-wildcard",
				LiteLLMParams: LiteLLMParams{
					Model:  "openrouter/*",
					APIKey: "or-key",
				},
				RPM: 100,
			},
			{ModelName: "claude-3", LiteLLMParams: LiteLLMParams{Model: "anthropic/claude-3", APIKey: "anth-key"}},
		},
	}

	providers := []DiscoveryProvider{
		{
			Prefix:     "openrouter",
			IsWildcard: func(mc ModelConfig) bool { return mc.LiteLLMParams.Model == "openrouter/*" },
			Discover: func(ctx context.Context, apiKey, apiBase string, template ModelConfig) ([]ModelConfig, error) {
				return []ModelConfig{
					{
						ModelName:     "google/gemini-2.5-pro",
						LiteLLMParams: LiteLLMParams{Model: "openrouter/google/gemini-2.5-pro", APIKey: apiKey},
						RPM:           template.RPM,
					},
					{
						ModelName:     "meta/llama-3-70b",
						LiteLLMParams: LiteLLMParams{Model: "openrouter/meta/llama-3-70b", APIKey: apiKey},
						RPM:           template.RPM,
					},
				}, nil
			},
		},
	}

	err := ExpandWildcards(context.Background(), cfg, providers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be: gpt-4, gemini-2.5-pro, llama-3-70b, claude-3
	if len(cfg.ModelList) != 4 {
		t.Fatalf("expected 4 models after expansion, got %d", len(cfg.ModelList))
	}

	expectedNames := []string{
		"gpt-4",
		"google/gemini-2.5-pro",
		"meta/llama-3-70b",
		"claude-3",
	}
	for i, name := range expectedNames {
		if cfg.ModelList[i].ModelName != name {
			t.Errorf("model[%d]: expected %q, got %q", i, name, cfg.ModelList[i].ModelName)
		}
	}

	// Verify discovered models inherited the API key
	if cfg.ModelList[1].LiteLLMParams.APIKey != "or-key" {
		t.Errorf("expected discovered model API key 'or-key', got %q", cfg.ModelList[1].LiteLLMParams.APIKey)
	}
}

func TestExpandWildcardsDiscoverError(t *testing.T) {
	cfg := &Config{
		ModelList: []ModelConfig{
			{
				ModelName: "openrouter-wildcard",
				LiteLLMParams: LiteLLMParams{
					Model:  "openrouter/*",
					APIKey: "or-key",
				},
			},
		},
	}

	providers := []DiscoveryProvider{
		{
			Prefix:     "openrouter",
			IsWildcard: func(mc ModelConfig) bool { return mc.LiteLLMParams.Model == "openrouter/*" },
			Discover: func(ctx context.Context, apiKey, apiBase string, template ModelConfig) ([]ModelConfig, error) {
				return nil, fmt.Errorf("connection refused")
			},
		},
	}

	err := ExpandWildcards(context.Background(), cfg, providers)
	if err == nil {
		t.Fatal("expected error when discovery fails")
	}
}

func TestExpandWildcardsEmptyDiscovery(t *testing.T) {
	cfg := &Config{
		ModelList: []ModelConfig{
			{ModelName: "gpt-4", LiteLLMParams: LiteLLMParams{Model: "openai/gpt-4", APIKey: "key"}},
			{
				ModelName: "openrouter-wildcard",
				LiteLLMParams: LiteLLMParams{
					Model:  "openrouter/*",
					APIKey: "or-key",
				},
			},
		},
	}

	providers := []DiscoveryProvider{
		{
			Prefix:     "openrouter",
			IsWildcard: func(mc ModelConfig) bool { return mc.LiteLLMParams.Model == "openrouter/*" },
			Discover: func(ctx context.Context, apiKey, apiBase string, template ModelConfig) ([]ModelConfig, error) {
				return []ModelConfig{}, nil
			},
		},
	}

	err := ExpandWildcards(context.Background(), cfg, providers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be just gpt-4, wildcard expanded to nothing
	if len(cfg.ModelList) != 1 {
		t.Fatalf("expected 1 model, got %d", len(cfg.ModelList))
	}
	if cfg.ModelList[0].ModelName != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %q", cfg.ModelList[0].ModelName)
	}
}
