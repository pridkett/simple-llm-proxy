package config

import "time"

// Config represents the complete proxy configuration.
type Config struct {
	ModelList       []ModelConfig   `yaml:"model_list"`
	RouterSettings  RouterSettings  `yaml:"router_settings"`
	GeneralSettings GeneralSettings `yaml:"general_settings"`
	LogSettings     LogSettings     `yaml:"log_settings"`
	OIDCSettings    OIDCSettings    `yaml:"oidc_settings"`
	ProviderPools   []ProviderPool  `yaml:"provider_pools"`
	Webhooks        []WebhookConfig `yaml:"webhooks"`
}

// OIDCSettings configures the OIDC provider (PocketID).
type OIDCSettings struct {
	IssuerURL    string `yaml:"issuer_url"`    // PocketID base URL, e.g. https://pocketid.example.com
	ClientID     string `yaml:"client_id"`     // supports os.environ/VAR_NAME
	ClientSecret string `yaml:"client_secret"` // supports os.environ/VAR_NAME
	RedirectURL  string `yaml:"redirect_url"`  // MUST be real server path, NOT hash route
	AdminGroup   string `yaml:"admin_group"`   // PocketID group name for proxy admins (default: "admin")
	DevMode      bool   `yaml:"dev_mode"`      // When true, Cookie.Secure=false for local HTTP dev (default: false)
}

// LogSettings controls logging behavior.
type LogSettings struct {
	Level      string `yaml:"level"`        // trace, debug, info, warn, error (default: info)
	Format     string `yaml:"format"`       // console or json (default: console)
	FilePath   string `yaml:"file_path"`    // optional JSON log file path
	MaxSizeMB  int    `yaml:"max_size_mb"`  // max MB before rotation (default: 100)
	MaxBackups int    `yaml:"max_backups"`  // rotated files to keep (default: 3)
	MaxAgeDays int    `yaml:"max_age_days"` // days before deletion (default: 28)
	Compress   bool   `yaml:"compress"`     // gzip rotated files (default: false)
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
	Model        string            `yaml:"model"`                    // provider/model format
	APIKey       string            `yaml:"api_key"`                  // supports os.environ/VAR
	APIBase      string            `yaml:"api_base,omitempty"`
	ExtraHeaders map[string]string `yaml:"extra_headers,omitempty"`  // additional HTTP headers (e.g., OpenRouter HTTP-Referer)
	ExtraParams  map[string]any    `yaml:"extra_params,omitempty"`   // provider-specific config (e.g., Gemini safety_settings, MiniMax xml_tool_calls)
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
	// BodySnippetLimit is the maximum number of bytes captured from request/response bodies
	// for observability snippets. 0 disables body capture entirely. Default: 500.
	BodySnippetLimit int `yaml:"body_snippet_limit"`
	// LogRetentionDays is the number of days to retain usage_logs rows.
	// The retention cleanup goroutine deletes rows older than this. Default: 30.
	LogRetentionDays int `yaml:"log_retention_days"`
}

// ProviderPool defines a named group of model deployments with shared routing strategy
// and optional daily budget cap. Configured via the provider_pools: YAML section.
type ProviderPool struct {
	Name           string       `yaml:"name"`
	Strategy       string       `yaml:"strategy"`         // weighted-round-robin | round-robin | shuffle; default: inherits router_settings
	BudgetCapDaily float64      `yaml:"budget_cap_daily"` // 0 = unlimited
	Members        []PoolMember `yaml:"members"`
}

// PoolMember is a model_list reference within a provider pool with an optional routing weight.
type PoolMember struct {
	ModelName string `yaml:"model_name"` // references a model_list entry by model_name
	Weight    int    `yaml:"weight"`     // default: 1 when not specified
}

// WebhookConfig defines a YAML-configured outbound webhook.
// YAML webhooks are held in memory only — never written to the webhook_subscriptions DB table.
type WebhookConfig struct {
	URL     string   `yaml:"url"`
	Events  []string `yaml:"events"`
	Secret  string   `yaml:"secret"` // supports os.environ/VAR_NAME expansion
	Enabled bool     `yaml:"enabled"`
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
			Port:             8080,
			DatabaseURL:      "./proxy.db",
			BodySnippetLimit: 500,
			LogRetentionDays: 30,
		},
		LogSettings: LogSettings{
			Level:      "info",
			Format:     "console",
			MaxSizeMB:  100,
			MaxBackups: 3,
			MaxAgeDays: 28,
		},
		OIDCSettings: OIDCSettings{
			AdminGroup: "admin",
		},
	}
}

// ParsedModel contains the parsed provider and model name.
type ParsedModel struct {
	Provider  string
	ModelName string
}
