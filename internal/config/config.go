package config

import "time"

// Config represents the complete proxy configuration.
type Config struct {
	ModelList       []ModelConfig    `yaml:"model_list"`
	RouterSettings  RouterSettings   `yaml:"router_settings"`
	GeneralSettings GeneralSettings  `yaml:"general_settings"`
}

// ModelConfig represents a model deployment configuration.
type ModelConfig struct {
	ModelName     string        `yaml:"model_name"`
	LiteLLMParams LiteLLMParams `yaml:"litellm_params"`
	RPM           int           `yaml:"rpm,omitempty"`
	TPM           int           `yaml:"tpm,omitempty"`
}

// LiteLLMParams contains provider-specific parameters.
type LiteLLMParams struct {
	Model   string `yaml:"model"`   // provider/model format
	APIKey  string `yaml:"api_key"` // supports os.environ/VAR
	APIBase string `yaml:"api_base,omitempty"`
}

// RouterSettings contains load balancing configuration.
type RouterSettings struct {
	RoutingStrategy string        `yaml:"routing_strategy"` // simple-shuffle or round-robin
	NumRetries      int           `yaml:"num_retries"`
	AllowedFails    int           `yaml:"allowed_fails"`
	CooldownTime    time.Duration `yaml:"cooldown_time"`
}

// GeneralSettings contains general server configuration.
type GeneralSettings struct {
	MasterKey   string `yaml:"master_key"`
	DatabaseURL string `yaml:"database_url"`
	Port        int    `yaml:"port"`
}

// Defaults returns a config with sensible defaults.
func Defaults() *Config {
	return &Config{
		RouterSettings: RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		GeneralSettings: GeneralSettings{
			Port:        8080,
			DatabaseURL: "./proxy.db",
		},
	}
}

// ParsedModel contains the parsed provider and model name.
type ParsedModel struct {
	Provider  string
	ModelName string
}
