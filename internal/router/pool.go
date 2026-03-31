package router

import (
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// Pool groups deployments under a named routing unit with a per-pool
// strategy. Pools are built from the provider_pools config section and
// allow different models to share a routing strategy (e.g., weighted
// round-robin across OpenAI and Anthropic deployments of the same
// virtual model).
type Pool struct {
	Name      string
	Strategy  Strategy
	Members   []*provider.Deployment
	Weights   map[string]int // DeploymentKey() -> weight
	ModelName string         // The virtual model name this pool serves (first member model name)
}
