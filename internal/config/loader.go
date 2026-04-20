package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// envVarPattern matches os.environ/VAR_NAME patterns.
var envVarPattern = regexp.MustCompile(`^os\.environ/(\w+)$`)

// Load reads and parses a config file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	return Parse(data)
}

// Parse parses config from YAML bytes.
func Parse(data []byte) (*Config, error) {
	// Start with defaults
	cfg := Defaults()

	// Use a raw map for initial parsing to handle custom types
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}

	// Process model_list
	if modelList, ok := raw["model_list"].([]any); ok {
		cfg.ModelList = make([]ModelConfig, 0, len(modelList))
		for i, item := range modelList {
			model, err := parseModelConfig(item)
			if err != nil {
				return nil, fmt.Errorf("parsing model_list[%d]: %w", i, err)
			}
			cfg.ModelList = append(cfg.ModelList, model)
		}
	}

	// Process router_settings
	if rs, ok := raw["router_settings"].(map[string]any); ok {
		if v, ok := rs["routing_strategy"].(string); ok {
			cfg.RouterSettings.RoutingStrategy = v
		}
		if v, ok := rs["num_retries"].(int); ok {
			cfg.RouterSettings.NumRetries = v
		}
		if v, ok := rs["allowed_fails"].(int); ok {
			cfg.RouterSettings.AllowedFails = v
		}
		if v, ok := rs["cooldown_time"].(string); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, fmt.Errorf("parsing cooldown_time: %w", err)
			}
			cfg.RouterSettings.CooldownTime = d
		}
	}

	// Process general_settings
	if gs, ok := raw["general_settings"].(map[string]any); ok {
		if v, ok := gs["master_key"].(string); ok {
			cfg.GeneralSettings.MasterKey = expandEnvVar(v)
		}
		if v, ok := gs["database_url"].(string); ok {
			cfg.GeneralSettings.DatabaseURL = v
		}
		if v, ok := gs["port"].(int); ok {
			cfg.GeneralSettings.Port = v
		}
		if v, ok := gs["body_snippet_limit"].(int); ok {
			cfg.GeneralSettings.BodySnippetLimit = v
		}
		if v, ok := gs["log_retention_days"].(int); ok {
			cfg.GeneralSettings.LogRetentionDays = v
		}
	}

	// Process log_settings
	if ls, ok := raw["log_settings"].(map[string]any); ok {
		if v, ok := ls["level"].(string); ok {
			cfg.LogSettings.Level = v
		}
		if v, ok := ls["format"].(string); ok {
			cfg.LogSettings.Format = v
		}
		if v, ok := ls["file_path"].(string); ok {
			cfg.LogSettings.FilePath = expandEnvVar(v)
		}
		if v, ok := ls["max_size_mb"].(int); ok {
			cfg.LogSettings.MaxSizeMB = v
		}
		if v, ok := ls["max_backups"].(int); ok {
			cfg.LogSettings.MaxBackups = v
		}
		if v, ok := ls["max_age_days"].(int); ok {
			cfg.LogSettings.MaxAgeDays = v
		}
		if v, ok := ls["compress"].(bool); ok {
			cfg.LogSettings.Compress = v
		}
	}

	// Process oidc_settings
	if os, ok := raw["oidc_settings"].(map[string]any); ok {
		if v, ok := os["issuer_url"].(string); ok {
			cfg.OIDCSettings.IssuerURL = v
		}
		if v, ok := os["client_id"].(string); ok {
			cfg.OIDCSettings.ClientID = expandEnvVar(v)
		}
		if v, ok := os["client_secret"].(string); ok {
			cfg.OIDCSettings.ClientSecret = expandEnvVar(v)
		}
		if v, ok := os["redirect_url"].(string); ok {
			cfg.OIDCSettings.RedirectURL = v
		}
		if v, ok := os["admin_group"].(string); ok {
			cfg.OIDCSettings.AdminGroup = v
		}
		if v, ok := os["dev_mode"].(bool); ok {
			cfg.OIDCSettings.DevMode = v
		}
	}

	// Process provider_pools
	if pools, ok := raw["provider_pools"].([]any); ok {
		cfg.ProviderPools = make([]ProviderPool, 0, len(pools))
		for i, item := range pools {
			pool, err := parseProviderPool(item)
			if err != nil {
				return nil, fmt.Errorf("parsing provider_pools[%d]: %w", i, err)
			}
			cfg.ProviderPools = append(cfg.ProviderPools, pool)
		}
	}

	// Process webhooks
	if webhooks, ok := raw["webhooks"].([]any); ok {
		cfg.Webhooks = make([]WebhookConfig, 0, len(webhooks))
		for i, item := range webhooks {
			wh, err := parseWebhookConfig(item)
			if err != nil {
				return nil, fmt.Errorf("parsing webhooks[%d]: %w", i, err)
			}
			cfg.Webhooks = append(cfg.Webhooks, wh)
		}
	}

	return cfg, nil
}

func parseModelConfig(item any) (ModelConfig, error) {
	m, ok := item.(map[string]any)
	if !ok {
		return ModelConfig{}, fmt.Errorf("expected map, got %T", item)
	}

	mc := ModelConfig{}

	if v, ok := m["model_name"].(string); ok {
		mc.ModelName = v
	} else {
		return mc, fmt.Errorf("model_name is required")
	}

	if params, ok := m["litellm_params"].(map[string]any); ok {
		if v, ok := params["model"].(string); ok {
			mc.LiteLLMParams.Model = v
		}
		if v, ok := params["api_key"].(string); ok {
			mc.LiteLLMParams.APIKey = expandEnvVar(v)
		}
		if v, ok := params["api_base"].(string); ok {
			mc.LiteLLMParams.APIBase = expandEnvVar(v)
		}

		// Parse extra_headers — additional HTTP headers for provider requests.
		// Values support os.environ/VAR expansion for secrets.
		if eh, ok := params["extra_headers"].(map[string]any); ok {
			mc.LiteLLMParams.ExtraHeaders = make(map[string]string, len(eh))
			for k, v := range eh {
				if s, ok := v.(string); ok {
					mc.LiteLLMParams.ExtraHeaders[k] = expandEnvVar(s)
				}
			}
		}

		// Parse extra_params — provider-specific configuration (e.g., Gemini
		// safety_settings, MiniMax xml_tool_calls). Stored as raw map and
		// interpreted by the router when building ProviderOptions.
		if ep, ok := params["extra_params"].(map[string]any); ok {
			mc.LiteLLMParams.ExtraParams = ep
		}
	}

	if v, ok := m["rpm"].(int); ok {
		mc.RPM = v
	}
	if v, ok := m["tpm"].(int); ok {
		mc.TPM = v
	}

	return mc, nil
}

func parseProviderPool(item any) (ProviderPool, error) {
	m, ok := item.(map[string]any)
	if !ok {
		return ProviderPool{}, fmt.Errorf("expected map, got %T", item)
	}

	pool := ProviderPool{}

	if v, ok := m["name"].(string); ok {
		pool.Name = v
	} else {
		return pool, fmt.Errorf("name is required")
	}

	if v, ok := m["strategy"].(string); ok {
		pool.Strategy = v
	}

	if v, ok := m["budget_cap_daily"].(float64); ok {
		pool.BudgetCapDaily = v
	}

	if members, ok := m["members"].([]any); ok {
		pool.Members = make([]PoolMember, 0, len(members))
		for i, memberItem := range members {
			mm, ok := memberItem.(map[string]any)
			if !ok {
				return pool, fmt.Errorf("members[%d]: expected map, got %T", i, memberItem)
			}
			member := PoolMember{Weight: 1} // default weight per D-07 discretion
			if v, ok := mm["model_name"].(string); ok {
				member.ModelName = v
			} else {
				return pool, fmt.Errorf("members[%d]: model_name is required", i)
			}
			if v, ok := mm["weight"].(int); ok {
				member.Weight = v
			}
			pool.Members = append(pool.Members, member)
		}
	}

	return pool, nil
}

func parseWebhookConfig(item any) (WebhookConfig, error) {
	m, ok := item.(map[string]any)
	if !ok {
		return WebhookConfig{}, fmt.Errorf("expected map, got %T", item)
	}

	wh := WebhookConfig{}

	if v, ok := m["url"].(string); ok {
		wh.URL = v
	}

	if events, ok := m["events"].([]any); ok {
		wh.Events = make([]string, 0, len(events))
		for _, e := range events {
			if s, ok := e.(string); ok {
				wh.Events = append(wh.Events, s)
			}
		}
	}

	if v, ok := m["secret"].(string); ok {
		wh.Secret = expandEnvVar(v) // D-08: apply os.environ/VAR expansion to secret
	}

	if v, ok := m["enabled"].(bool); ok {
		wh.Enabled = v
	}

	return wh, nil
}

// expandEnvVar expands os.environ/VAR_NAME patterns to actual values.
func expandEnvVar(s string) string {
	matches := envVarPattern.FindStringSubmatch(s)
	if len(matches) == 2 {
		return os.Getenv(matches[1])
	}
	return s
}

// ParseModelString parses a "provider/model" string.
func ParseModelString(s string) ParsedModel {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) == 2 {
		return ParsedModel{
			Provider:  parts[0],
			ModelName: parts[1],
		}
	}
	// Default to openai if no provider specified
	return ParsedModel{
		Provider:  "openai",
		ModelName: s,
	}
}
