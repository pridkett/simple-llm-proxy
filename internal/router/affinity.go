package router

import "github.com/pwagstro/simple_llm_proxy/internal/provider"

// AffinityStrategy wraps an inner Strategy to prefer deployments from a
// specific provider. When healthy deployments exist for the preferred
// provider, the inner strategy selects among them exclusively. When no
// preferred deployments are available (all in cooldown or filtered out),
// the inner strategy falls back to the full deployment list.
//
// This is composable: the inner strategy can be a WeightedRoundRobin,
// RoundRobin, Shuffle, or any other Strategy implementation.
type AffinityStrategy struct {
	preferred string   // provider name (e.g., "openai")
	inner     Strategy // fallback strategy when preferred is unhealthy
}

// NewAffinityStrategy creates a new AffinityStrategy that prefers
// deployments from the named provider and delegates selection to the
// inner strategy.
func NewAffinityStrategy(preferred string, inner Strategy) *AffinityStrategy {
	return &AffinityStrategy{
		preferred: preferred,
		inner:     inner,
	}
}

// Select filters deployments for the preferred provider and delegates
// to the inner strategy. If no preferred deployments exist, the inner
// strategy selects from the full list (graceful fallback).
func (a *AffinityStrategy) Select(deployments []*provider.Deployment) *provider.Deployment {
	if len(deployments) == 0 {
		return nil
	}

	// Filter for preferred provider
	preferred := make([]*provider.Deployment, 0, len(deployments))
	for _, d := range deployments {
		if d.ProviderName == a.preferred {
			preferred = append(preferred, d)
		}
	}

	// If preferred deployments exist, use only those
	if len(preferred) > 0 {
		return a.inner.Select(preferred)
	}

	// Fallback: use all available deployments
	return a.inner.Select(deployments)
}

// PreferredProvider returns the name of the preferred provider.
func (a *AffinityStrategy) PreferredProvider() string {
	return a.preferred
}
