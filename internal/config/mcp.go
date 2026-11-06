package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type MCPSettings struct {
	CredentialKey [32]byte             `yaml:"-" json:"-"`
	Servers       map[string]MCPServer `yaml:"servers" json:"-"`
}

type MCPServer struct {
	URL              string   `yaml:"url"`
	Scope            string   `yaml:"credential_scope"`
	Header           string   `yaml:"credential_header"`
	SharedKey        string   `yaml:"shared_key" json:"-"`
	AllowedAppKeyIDs []int64  `yaml:"allowed_app_key_ids"`
	AllowedUserIDs   []string `yaml:"allowed_user_ids"`
}

var mcpNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
var mcpHeaderPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

func parseMCPSettings(value any, cfg *Config) error {
	if _, ok := value.(map[string]any); !ok {
		return fmt.Errorf("mcp must be a mapping")
	}
	var input struct {
		CredentialKey string               `yaml:"credential_key"`
		Servers       map[string]MCPServer `yaml:"servers"`
	}
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("invalid mcp settings")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(encoded))
	decoder.KnownFields(true)
	if err := decoder.Decode(&input); err != nil {
		// YAML type errors can echo values, including accidental inline credentials.
		return fmt.Errorf("invalid mcp settings: check field names and types")
	}

	if len(input.Servers) > 0 && strings.TrimSpace(cfg.GeneralSettings.DatabaseURL) == "" {
		return fmt.Errorf("mcp servers require database_url")
	}
	cfg.MCP.Servers = input.Servers
	personal := false
	for name, server := range input.Servers {
		if !mcpNamePattern.MatchString(name) {
			return fmt.Errorf("invalid MCP server name")
		}
		target, err := url.Parse(server.URL)
		if err != nil || target.Scheme != "https" || target.Hostname() == "" ||
			target.User != nil || target.Opaque != "" || target.Fragment != "" ||
			target.RawQuery != "" || target.ForceQuery {
			return fmt.Errorf("mcp server %q requires an HTTPS URL without credentials, query, or fragment", name)
		}
		switch server.Scope {
		case "none":
			if server.Header != "" || server.SharedKey != "" {
				return fmt.Errorf("mcp server %q with scope none cannot configure credentials", name)
			}
		case "shared":
			if !envVarPattern.MatchString(server.SharedKey) {
				return fmt.Errorf("mcp server %q shared_key must reference an environment variable", name)
			}
			server.SharedKey = expandEnvVar(server.SharedKey)
			if !ValidMCPKey(server.SharedKey) {
				return fmt.Errorf("mcp server %q shared key is missing or invalid", name)
			}
		case "user":
			if server.SharedKey != "" || len(server.AllowedAppKeyIDs) != 0 {
				return fmt.Errorf("mcp server %q with scope user only accepts user grants and credentials", name)
			}
		default:
			return fmt.Errorf("mcp server %q credential_scope must be none, shared, or user", name)
		}
		if server.Scope != "none" {
			if server.Header == "" {
				server.Header = "Authorization"
			}
			server.Header = http.CanonicalHeaderKey(server.Header)
			if !mcpHeaderPattern.MatchString(server.Header) ||
				(server.Header != "Authorization" && server.Header != "Api-Key" &&
					(!strings.HasPrefix(server.Header, "X-") || strings.HasPrefix(server.Header, "X-Forwarded-"))) {
				return fmt.Errorf("mcp server %q has an unsupported credential_header", name)
			}
		}
		for _, id := range server.AllowedAppKeyIDs {
			if id <= 0 {
				return fmt.Errorf("mcp server %q app key IDs must be positive", name)
			}
		}
		for _, id := range server.AllowedUserIDs {
			if strings.TrimSpace(id) == "" {
				return fmt.Errorf("mcp server %q user IDs must be nonempty", name)
			}
		}
		personal = personal || server.Scope == "user" || len(server.AllowedUserIDs) > 0
		cfg.MCP.Servers[name] = server
	}
	if personal && cfg.OIDCSettings.IssuerURL == "" {
		return fmt.Errorf("mcp user access requires OIDC")
	}
	if personal || input.CredentialKey != "" {
		if !envVarPattern.MatchString(input.CredentialKey) {
			return fmt.Errorf("mcp credential_key must reference an environment variable")
		}
		key, err := base64.StdEncoding.DecodeString(expandEnvVar(input.CredentialKey))
		if err != nil || len(key) != len(cfg.MCP.CredentialKey) {
			return fmt.Errorf("mcp credential_key must contain 32 base64-encoded bytes")
		}
		copy(cfg.MCP.CredentialKey[:], key)
	}
	return nil
}

func ValidMCPKey(key string) bool {
	if len(key) == 0 || len(key) > 8192 {
		return false
	}
	for _, c := range key {
		if c < '!' || c > '~' {
			return false
		}
	}
	return true
}
