package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const KeyIssuerTokenPrefix = "sk-issuer-"

// KeyIssuer is the administrator-defined scope of a service credential.
type KeyIssuer struct {
	ApplicationID  int64         `yaml:"application_id"`
	AllowedModels  []string      `yaml:"allowed_models"`
	MaxTTL         time.Duration `yaml:"-"`
	MaxRPM         *int          `yaml:"max_rpm"`
	MaxRPD         *int          `yaml:"max_rpd"`
	MaxBudget      *float64      `yaml:"max_budget"`
	AllowDecisions bool          `yaml:"allow_decisions"`
	TokenHash      [32]byte      `yaml:"-" json:"-"`
}

var issuerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

func parseKeyIssuers(value any, cfg *Config) error {
	fieldsByID, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("key_issuers must be a mapping")
	}
	var entries map[string]struct {
		KeyIssuer `yaml:",inline"`
		Token     string `yaml:"token"`
		MaxTTL    string `yaml:"max_ttl"`
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("invalid key_issuers settings")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&entries); err != nil {
		return fmt.Errorf("invalid key_issuers settings: check field names and types")
	}
	if len(entries) > 0 && strings.TrimSpace(cfg.GeneralSettings.DatabaseURL) == "" {
		return fmt.Errorf("key_issuers require database_url")
	}

	cfg.KeyIssuers = make(map[string]KeyIssuer, len(entries))
	seen := make(map[[32]byte]bool, len(entries))
	for id, entry := range entries {
		if !issuerIDPattern.MatchString(id) {
			return fmt.Errorf("invalid key issuer ID")
		}
		fields, _ := fieldsByID[id].(map[string]any)
		policy := entry.KeyIssuer
		ttl, err := time.ParseDuration(entry.MaxTTL)
		if err != nil || ttl < time.Millisecond {
			return fmt.Errorf("key issuer %q max_ttl must be a duration of at least 1ms", id)
		}
		policy.MaxTTL = ttl
		if !isYAMLInteger(fields["application_id"]) || policy.ApplicationID <= 0 || !ValidModelAllowlist(policy.AllowedModels) {
			return fmt.Errorf("key issuer %q requires a positive application_id and a nonempty allowlist of distinct models", id)
		}
		if (fields["max_rpm"] != nil && !isYAMLInteger(fields["max_rpm"])) ||
			(fields["max_rpd"] != nil && !isYAMLInteger(fields["max_rpd"])) {
			return fmt.Errorf("key issuer %q request limits must be integers", id)
		}
		if (policy.MaxRPM != nil && (*policy.MaxRPM <= 0 || *policy.MaxRPM > math.MaxInt32)) ||
			(policy.MaxRPD != nil && (*policy.MaxRPD <= 0 || *policy.MaxRPD > math.MaxInt32)) ||
			(policy.MaxBudget != nil && (*policy.MaxBudget <= 0 || math.IsNaN(*policy.MaxBudget) || math.IsInf(*policy.MaxBudget, 0))) {
			return fmt.Errorf("key issuer %q limits must be finite and positive, and request limits must fit in 32 bits", id)
		}
		if !envVarPattern.MatchString(entry.Token) {
			return fmt.Errorf("key issuer %q token must reference an environment variable", id)
		}
		token := expandEnvVar(entry.Token)
		value, ok := strings.CutPrefix(token, KeyIssuerTokenPrefix)
		if _, err := hex.DecodeString(value); !ok || len(value) != 64 || err != nil {
			return fmt.Errorf("key issuer %q token must use sk-issuer- followed by 64 hex characters", id)
		}
		if token == cfg.GeneralSettings.MasterKey {
			return fmt.Errorf("key issuer %q token must differ from the master key", id)
		}
		policy.TokenHash = sha256.Sum256([]byte(token))
		if seen[policy.TokenHash] {
			return fmt.Errorf("key issuer %q token is also assigned to another issuer", id)
		}
		seen[policy.TokenHash] = true
		cfg.KeyIssuers[id] = policy
	}
	return nil
}

func isYAMLInteger(value any) bool {
	switch value.(type) {
	case int, int64, uint64:
		return true
	default:
		return false
	}
}

func ValidModelAllowlist(models []string) bool {
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		if model == "" || strings.TrimSpace(model) != model || seen[model] {
			return false
		}
		seen[model] = true
	}
	return len(models) > 0
}
