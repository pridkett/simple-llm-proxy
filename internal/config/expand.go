package config

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
)

// ModelDiscoverer is a function that fetches available models from a provider
// and returns expanded ModelConfig entries. It is called during wildcard
// expansion to discover models at startup.
type ModelDiscoverer func(ctx context.Context, apiKey, apiBase string, template ModelConfig) ([]ModelConfig, error)

// WildcardChecker is a function that returns true if a ModelConfig uses a
// wildcard pattern that should trigger model discovery.
type WildcardChecker func(mc ModelConfig) bool

// DiscoveryProvider bundles a wildcard checker with a discovery function for a
// specific provider (e.g., OpenRouter).
type DiscoveryProvider struct {
	// Prefix is the provider prefix to match (e.g., "openrouter").
	Prefix string
	// IsWildcard returns true if the model config uses a wildcard pattern.
	IsWildcard WildcardChecker
	// Discover fetches and returns expanded model configs.
	Discover ModelDiscoverer
}

// ExpandWildcards processes the config's ModelList and expands any wildcard
// entries using the registered discovery providers. Non-wildcard entries are
// passed through unchanged. This must be called with a context (for HTTP
// timeouts) after Parse() but before the router is created.
func ExpandWildcards(ctx context.Context, cfg *Config, providers []DiscoveryProvider) error {
	if len(providers) == 0 {
		return nil
	}

	expanded := make([]ModelConfig, 0, len(cfg.ModelList))

	for _, mc := range cfg.ModelList {
		matched := false
		for _, dp := range providers {
			if dp.IsWildcard(mc) {
				matched = true
				apiKey := mc.LiteLLMParams.APIKey
				apiBase := mc.LiteLLMParams.APIBase

				log.Info().
					Str("provider", dp.Prefix).
					Str("api_base", apiBase).
					Msg("discovering models via wildcard")

				models, err := dp.Discover(ctx, apiKey, apiBase, mc)
				if err != nil {
					return fmt.Errorf("expanding wildcard for %s: %w", dp.Prefix, err)
				}

				log.Info().
					Str("provider", dp.Prefix).
					Int("count", len(models)).
					Msg("discovered models")

				expanded = append(expanded, models...)
				break
			}
		}
		if !matched {
			expanded = append(expanded, mc)
		}
	}

	cfg.ModelList = expanded
	return nil
}

