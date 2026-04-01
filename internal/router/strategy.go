package router

import "github.com/pwagstro/simple_llm_proxy/internal/provider"

// Strategy defines the interface for routing strategies.
type Strategy interface {
	// Name returns a human-readable strategy name (e.g., "simple-shuffle", "round-robin").
	Name() string

	// Select chooses a deployment from the available list.
	Select(deployments []*provider.Deployment) *provider.Deployment
}
