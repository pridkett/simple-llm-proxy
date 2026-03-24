package router

import (
	"fmt"
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
	settings    config.RouterSettings
}

// New creates a new router from config.
func New(cfg *config.Config) (*Router, error) {
	r := &Router{
		deployments: make(map[string][]*provider.Deployment),
		settings:    cfg.RouterSettings,
		cooldown:    NewCooldownManager(cfg.RouterSettings.CooldownTime, cfg.RouterSettings.AllowedFails),
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

	// Filter out deployments in cooldown or already tried
	healthy := make([]*provider.Deployment, 0, len(deployments))
	for _, d := range deployments {
		if !r.cooldown.InCooldown(d) && !tried[d] {
			healthy = append(healthy, d)
		}
	}

	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy deployment available for %s", modelName)
	}

	return r.strategy.Select(healthy), nil
}

// ReportSuccess reports a successful request.
func (r *Router) ReportSuccess(d *provider.Deployment) {
	r.cooldown.ReportSuccess(d)
}

// ReportFailure reports a failed request.
func (r *Router) ReportFailure(d *provider.Deployment) {
	r.cooldown.ReportFailure(d)
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
	return result
}
