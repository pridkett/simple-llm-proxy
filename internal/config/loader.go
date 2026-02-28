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
	}

	if v, ok := m["rpm"].(int); ok {
		mc.RPM = v
	}
	if v, ok := m["tpm"].(int); ok {
		mc.TPM = v
	}

	return mc, nil
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
