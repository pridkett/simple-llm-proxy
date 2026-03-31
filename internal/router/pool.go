package router

import "github.com/pwagstro/simple_llm_proxy/internal/provider"

// Pool groups a set of model deployments under a shared routing strategy
// with an optional daily budget cap. Pools are built from the provider_pools
// YAML config section during Router.New() and Router.Reload().
type Pool struct {
	Name      string
	Strategy  Strategy
	Members   []*provider.Deployment
	Weights   map[string]int // DeploymentKey() -> weight
	ModelName string         // The virtual model name this pool serves
}
