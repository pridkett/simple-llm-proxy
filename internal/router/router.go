package router

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// Router manages model deployments and load balancing.
type Router struct {
	mu          sync.RWMutex
	deployments map[string][]*provider.Deployment // model_name -> deployments
	strategy    Strategy
	cooldown    *CooldownManager
	backoff     *BackoffManager
	settings    config.RouterSettings
}

// New creates a new router from config.
func New(cfg *config.Config) (*Router, error) {
	r := &Router{
		deployments: make(map[string][]*provider.Deployment),
		settings:    cfg.RouterSettings,
		cooldown:    NewCooldownManager(cfg.RouterSettings.CooldownTime, cfg.RouterSettings.AllowedFails),
		backoff:     NewBackoffManager(),
	}

	// Initialize strategy
	switch cfg.RouterSettings.RoutingStrategy {
	case "round-robin":
		r.strategy = NewRoundRobin()
	default:
		r.strategy = NewShuffle()
	}

	// Register deployments from config
	for _, mc := range cfg.ModelList {
		parsed := config.ParseModelString(mc.LiteLLMParams.Model)

		prov, err := provider.Get(parsed.Provider, mc.LiteLLMParams.APIKey, mc.LiteLLMParams.APIBase)
		if err != nil {
			return nil, fmt.Errorf("getting provider for %s: %w", mc.ModelName, err)
		}

		deployment := &provider.Deployment{
			ModelName:    mc.ModelName,
			Provider:     prov,
			ProviderName: parsed.Provider,
			ActualModel:  parsed.ModelName,
			APIKey:       mc.LiteLLMParams.APIKey,
			APIBase:      mc.LiteLLMParams.APIBase,
			RPM:          mc.RPM,
			TPM:          mc.TPM,
		}

		r.deployments[mc.ModelName] = append(r.deployments[mc.ModelName], deployment)
	}

	// Validate provider_pools: all member model_names must exist in model_list.
	// Per D-10: invalid references cause startup failure with a clear error.
	knownModels := make(map[string]bool, len(cfg.ModelList))
	for _, mc := range cfg.ModelList {
		knownModels[mc.ModelName] = true
	}
	for _, pool := range cfg.ProviderPools {
		for _, member := range pool.Members {
			if !knownModels[member.ModelName] {
				return nil, fmt.Errorf("provider_pools[%q].members: model_name %q not found in model_list",
					pool.Name, member.ModelName)
			}
		}
	}

	// Validate webhook configs: url and events must be non-empty.
	// Per D-11: invalid webhook configs cause startup failure.
	for i, wh := range cfg.Webhooks {
		if wh.URL == "" {
			return nil, fmt.Errorf("webhooks[%d]: url is required", i)
		}
		if len(wh.Events) == 0 {
			return nil, fmt.Errorf("webhooks[%d]: events is required and must not be empty", i)
		}
	}

	return r, nil
}

// GetDeployment returns a healthy deployment for the given model.
func (r *Router) GetDeployment(modelName string) (*provider.Deployment, error) {
	r.mu.RLock()
	deployments, ok := r.deployments[modelName]
	r.mu.RUnlock()

	if !ok || len(deployments) == 0 {
		return nil, fmt.Errorf("model not found: %s", modelName)
	}

	// Filter out deployments in cooldown
	healthy := make([]*provider.Deployment, 0, len(deployments))
	for _, d := range deployments {
		if !r.cooldown.InCooldown(d) {
			healthy = append(healthy, d)
		}
	}

	if len(healthy) == 0 {
		// All deployments in cooldown, try the first one anyway
		return deployments[0], nil
	}

	return r.strategy.Select(healthy), nil
}

// GetDeploymentWithRetry attempts to get a deployment, retrying on failure.
func (r *Router) GetDeploymentWithRetry(modelName string, tried map[*provider.Deployment]bool) (*provider.Deployment, error) {
	r.mu.RLock()
	deployments, ok := r.deployments[modelName]
	r.mu.RUnlock()

	if !ok || len(deployments) == 0 {
		return nil, fmt.Errorf("model not found: %s", modelName)
	}

	// Filter out deployments in cooldown, backoff, or already tried
	healthy := make([]*provider.Deployment, 0, len(deployments))
	for _, d := range deployments {
		if !r.cooldown.InCooldown(d) && !r.backoff.InBackoff(d.DeploymentKey()) && !tried[d] {
			healthy = append(healthy, d)
		}
	}

	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy deployment available for %s", modelName)
	}

	return r.strategy.Select(healthy), nil
}

// ReportSuccess reports a successful request.
// Resets both cooldown and backoff state for the deployment.
func (r *Router) ReportSuccess(d *provider.Deployment) {
	r.cooldown.ReportSuccess(d)
	r.backoff.Reset(d.DeploymentKey())
}

// ReportFailure reports a failed request.
func (r *Router) ReportFailure(d *provider.Deployment) {
	r.cooldown.ReportFailure(d)
}

// ReportRateLimit records a 429 response for a deployment.
// Applies full-jitter exponential backoff; does NOT trigger cooldown.
func (r *Router) ReportRateLimit(d *provider.Deployment, retryAfter time.Duration) {
	r.backoff.ReportRateLimit(d.DeploymentKey(), retryAfter)
}

// ListModels returns all available model names.
func (r *Router) ListModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	models := make([]string, 0, len(r.deployments))
	for name := range r.deployments {
		models = append(models, name)
	}
	return models
}

// NumRetries returns the configured number of retries.
func (r *Router) NumRetries() int {
	return r.settings.NumRetries
}

// Settings returns the router settings.
func (r *Router) Settings() config.RouterSettings {
	return r.settings
}

// ModelStatusInfo holds status information for a model and all its deployments.
type ModelStatusInfo struct {
	ModelName          string               `json:"model_name"`
	TotalDeployments   int                  `json:"total_deployments"`
	HealthyDeployments int                  `json:"healthy_deployments"`
	Deployments        []DeploymentInfoItem `json:"deployments"`
}

// DeploymentInfoItem holds status information for a single deployment.
type DeploymentInfoItem struct {
	ProviderName  string     `json:"provider"`
	ActualModel   string     `json:"actual_model"`
	APIBase       string     `json:"api_base,omitempty"`
	Status        string     `json:"status"` // "healthy" or "cooldown"
	FailureCount  int        `json:"failure_count"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
	RPM           int        `json:"rpm,omitempty"`
	TPM           int        `json:"tpm,omitempty"`
}

// Reload updates the router with a new configuration.
// Deployments are rebuilt from the new config and cooldown state is reset.
func (r *Router) Reload(cfg *config.Config) error {
	newDeployments := make(map[string][]*provider.Deployment)
	for _, mc := range cfg.ModelList {
		parsed := config.ParseModelString(mc.LiteLLMParams.Model)
		prov, err := provider.Get(parsed.Provider, mc.LiteLLMParams.APIKey, mc.LiteLLMParams.APIBase)
		if err != nil {
			return fmt.Errorf("getting provider for %s: %w", mc.ModelName, err)
		}
		deployment := &provider.Deployment{
			ModelName:    mc.ModelName,
			Provider:     prov,
			ProviderName: parsed.Provider,
			ActualModel:  parsed.ModelName,
			APIKey:       mc.LiteLLMParams.APIKey,
			APIBase:      mc.LiteLLMParams.APIBase,
			RPM:          mc.RPM,
			TPM:          mc.TPM,
		}
		newDeployments[mc.ModelName] = append(newDeployments[mc.ModelName], deployment)
	}

	// Validate provider_pools and webhooks on reload (same rules as New()).
	knownModels := make(map[string]bool, len(cfg.ModelList))
	for _, mc := range cfg.ModelList {
		knownModels[mc.ModelName] = true
	}
	for _, pool := range cfg.ProviderPools {
		for _, member := range pool.Members {
			if !knownModels[member.ModelName] {
				return fmt.Errorf("provider_pools[%q].members: model_name %q not found in model_list",
					pool.Name, member.ModelName)
			}
		}
	}
	for i, wh := range cfg.Webhooks {
		if wh.URL == "" {
			return fmt.Errorf("webhooks[%d]: url is required", i)
		}
		if len(wh.Events) == 0 {
			return fmt.Errorf("webhooks[%d]: events is required and must not be empty", i)
		}
	}

	var newStrategy Strategy
	switch cfg.RouterSettings.RoutingStrategy {
	case "round-robin":
		newStrategy = NewRoundRobin()
	default:
		newStrategy = NewShuffle()
	}

	r.mu.Lock()
	r.deployments = newDeployments
	r.settings = cfg.RouterSettings
	r.strategy = newStrategy
	r.cooldown = NewCooldownManager(cfg.RouterSettings.CooldownTime, cfg.RouterSettings.AllowedFails)
	r.mu.Unlock()

	return nil
}

// GetStatus returns the current status of all model deployments.
func (r *Router) GetStatus() []ModelStatusInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ModelStatusInfo, 0, len(r.deployments))
	for modelName, deployments := range r.deployments {
		infos := make([]DeploymentInfoItem, 0, len(deployments))
		healthyCount := 0

		for _, d := range deployments {
			s := r.cooldown.GetStatus(d)
			item := DeploymentInfoItem{
				ProviderName: d.ProviderName,
				ActualModel:  d.ActualModel,
				APIBase:      d.APIBase,
				FailureCount: s.FailureCount,
				RPM:          d.RPM,
				TPM:          d.TPM,
			}
			if s.InCooldown {
				item.Status = "cooldown"
				t := s.CooldownUntil
				item.CooldownUntil = &t
			} else {
				item.Status = "healthy"
				healthyCount++
			}
			infos = append(infos, item)
		}

		result = append(result, ModelStatusInfo{
			ModelName:          modelName,
			TotalDeployments:   len(deployments),
			HealthyDeployments: healthyCount,
			Deployments:        infos,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ModelName < result[j].ModelName
	})
	return result
}
