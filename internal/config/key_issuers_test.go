package config

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestKeyIssuerConfig(t *testing.T) {
	token := KeyIssuerTokenPrefix + strings.Repeat("a", 64)
	t.Setenv("PROXY_TEST_ISSUER_TOKEN", token)
	const yaml = `
key_issuers:
  framework:
    token: os.environ/PROXY_TEST_ISSUER_TOKEN
    application_id: 7
    allowed_models: [game-sfx, game-music]
    max_ttl: 30m
    max_budget: 2.50
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	issuer := cfg.KeyIssuers["framework"]
	if issuer.ApplicationID != 7 || issuer.MaxTTL != 30*time.Minute || issuer.TokenHash != sha256.Sum256([]byte(token)) ||
		issuer.MaxBudget == nil || *issuer.MaxBudget != 2.5 {
		t.Fatal("issuer policy was not parsed")
	}
	encoded, err := json.Marshal(cfg)
	if err != nil || strings.Contains(string(encoded), token) || strings.Contains(string(encoded), "KeyIssuers") {
		t.Fatal("issuer settings were serialized")
	}
	for _, tc := range []struct{ name, input, message string }{
		{"inline token", strings.Replace(yaml, "os.environ/PROXY_TEST_ISSUER_TOKEN", token, 1), "environment variable"},
		{"missing env", strings.Replace(yaml, "PROXY_TEST_ISSUER_TOKEN", "PROXY_TEST_UNSET_ISSUER_TOKEN", 1), "64 hex characters"},
		{"invalid lifetime", strings.Replace(yaml, "30m", "0s", 1), "max_ttl"},
		{"missing application", strings.Replace(yaml, "application_id: 7", "application_id: 0", 1), "positive application_id"},
		{"fractional application", strings.Replace(yaml, "application_id: 7", "application_id: 7.5", 1), "positive application_id"},
		{"duplicate models", strings.Replace(yaml, "game-sfx, game-music", "game-sfx, game-sfx", 1), "distinct models"},
		{"unlimited rate", yaml + "    max_rpm: 0\n", "finite and positive"},
		{"malformed policy", yaml + "    typo: " + token + "\n", "check field names and types"},
		{"duplicate token", yaml + strings.Replace(strings.SplitN(yaml, "\n", 3)[2], "framework:", "second:", 1), "assigned to another issuer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.message) || strings.Contains(err.Error(), token) {
				t.Fatalf("unexpected validation result for %s", tc.name)
			}
		})
	}
}
