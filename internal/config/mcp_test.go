package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestMCPConfiguration(t *testing.T) {
	t.Setenv("TEST_MCP_SHARED_KEY", "a-secret")
	t.Setenv("TEST_MCP_CREDENTIAL_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	const valid = `
oidc_settings:
  issuer_url: https://identity.example.test
mcp:
  credential_key: os.environ/TEST_MCP_CREDENTIAL_KEY
  servers:
    personal:
      url: https://personal.example.test/mcp
      credential_scope: user
      allowed_user_ids: [subject]
    shared:
      url: https://shared.example.test/mcp
      credential_scope: shared
      credential_header: x-api-key
      shared_key: os.environ/TEST_MCP_SHARED_KEY
      allowed_app_key_ids: [42]
`
	cfg, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCP.Servers["shared"].Header != "X-Api-Key" ||
		cfg.MCP.Servers["shared"].SharedKey != "a-secret" ||
		cfg.MCP.Servers["personal"].Header != "Authorization" ||
		cfg.MCP.CredentialKey[0] != 'k' {
		t.Fatal("MCP defaults or environment expansion were not applied")
	}
	for _, test := range []struct{ original, replacement string }{
		{"https://shared.example.test/mcp", "http://shared.example.test/mcp"},
		{"https://shared.example.test/mcp", "https://name:secret@shared.example.test/mcp"},
		{"https://shared.example.test/mcp", "https://shared.example.test/mcp?key=secret"},
		{"x-api-key", "Mcp-Session-Id"},
		{"os.environ/TEST_MCP_SHARED_KEY", "inline-secret"},
		{"allowed_user_ids: [subject]", "allowed_app_key_ids: [42]"},
		{"allowed_user_ids: [subject]", "allowed_usr_ids: [subject]"},
		{"allowed_app_key_ids: [42]", "allowed_app_key_ids: [0]"},
	} {
		t.Run(test.replacement, func(t *testing.T) {
			input := strings.Replace(valid, test.original, test.replacement, 1)
			if _, err := Parse([]byte(input)); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected a redacted validation error, got %v", err)
			}
		})
	}
}
