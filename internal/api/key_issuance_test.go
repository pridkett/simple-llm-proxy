package api_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

const issuancePath = "/v1/key-issuance/keys"

func TestHeadlessAppKeyIssuance(t *testing.T) {
	f := newMigrationFixture(t)
	team := f.create(t, "/admin/teams", `{"name":"agent-framework"}`)
	appID := int64(f.create(t, "/admin/applications", fmt.Sprintf(`{"name":"game","team_id":%.0f}`, team["id"]))["id"].(float64))
	otherAppID := int64(f.create(t, "/admin/applications", fmt.Sprintf(`{"name":"other","team_id":%.0f}`, team["id"]))["id"].(float64))
	adminKey := f.create(t, fmt.Sprintf("/admin/applications/%d/keys", appID), `{"name":"manually issued"}`)
	issuerToken := "sk-issuer-" + strings.Repeat("a", 64)
	peerToken := "sk-issuer-" + strings.Repeat("b", 64)
	otherToken := "sk-issuer-" + strings.Repeat("c", 64)
	t.Setenv("PROXY_TEST_GAME_ISSUER", issuerToken)
	t.Setenv("PROXY_TEST_PEER_ISSUER", peerToken)
	t.Setenv("PROXY_TEST_OTHER_ISSUER", otherToken)
	config, err := os.ReadFile(f.configPath)
	if err != nil {
		t.Fatal(err)
	}
	config = append(config, fmt.Sprintf(`
key_issuers:
  game:
    token: os.environ/PROXY_TEST_GAME_ISSUER
    application_id: %d
    allowed_models: [openai, openrouter]
    max_ttl: 30m
    max_rpm: 20
    max_rpd: 100
    max_budget: 2.50
  peer:
    token: os.environ/PROXY_TEST_PEER_ISSUER
    application_id: %d
    allowed_models: [openai]
    max_ttl: 30m
  other:
    token: os.environ/PROXY_TEST_OTHER_ISSUER
    application_id: %d
    allowed_models: [openai]
    max_ttl: 30m
`, appID, appID, otherAppID)...)
	if err := os.WriteFile(f.configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.reloader.Reload(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Truncate(time.Millisecond).Add(15 * time.Minute)
	body := fmt.Sprintf(`{"name":"agent-run-42","expires_at":%q,"allowed_models":["openai"],"max_rpd":50,"soft_budget":1}`, deadline.Format(time.RFC3339Nano))
	for _, tc := range []struct {
		token, input string
		status       int
		admin        bool
	}{
		{"", body, 401, false},
		{"master-only", body, 401, false},
		{adminKey["key"].(string), body, 401, false},
		{"", body, 401, true},
		{issuerToken, strings.Replace(body, `"allowed_models":["openai"]`, `"allowed_models":[]`, 1), 400, false},
		{issuerToken, strings.Replace(body, `"allowed_models":["openai"]`, `"allowed_models":["anthropic"]`, 1), 403, false},
		{issuerToken, strings.Replace(body, `"allowed_models":["openai"]`, `"allowed_models":["openai","openai"]`, 1), 400, false},
		{issuerToken, strings.Replace(body, deadline.Format(time.RFC3339Nano), time.Now().Add(time.Hour).Format(time.RFC3339Nano), 1), 403, false},
		{issuerToken, strings.Replace(body, deadline.Format(time.RFC3339Nano), time.Now().Add(-time.Minute).Format(time.RFC3339Nano), 1), 400, false},
		{issuerToken, strings.Replace(body, fmt.Sprintf("%q", deadline.Format(time.RFC3339Nano)), "null", 1), 400, false},
		{issuerToken, strings.Replace(body, `"max_rpd":50`, `"max_rpd":0`, 1), 400, false},
		{issuerToken, strings.Replace(body, `"max_rpd":50`, `"max_rpd":101`, 1), 403, false},
		{issuerToken, strings.Replace(body, `"max_rpd":50`, `"max_rpm":21`, 1), 403, false},
		{issuerToken, strings.Replace(body, `"max_rpd":50`, `"max_budget":3`, 1), 403, false},
		{issuerToken, strings.Replace(body, `"max_rpd":50`, `"max_budget":0.5`, 1), 403, false},
		{issuerToken, strings.Replace(body, `"max_rpd":50`, `"allow_decisions":true`, 1), 403, false},
		{issuerToken, strings.Replace(body, `"max_rpd":50`, `"application_id":12345`, 1), 400, false},
		{issuerToken, body + "{}", 400, false},
	} {
		status, _, response := f.request(t, http.MethodPost, issuancePath, tc.input, tc.token, tc.admin)
		if status != tc.status || strings.Contains(string(response), issuerToken) {
			t.Errorf("issuance request returned HTTP %d, want %d", status, tc.status)
		}
	}
	listed, err := f.store.ListAPIKeys(t.Context(), appID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("invalid issuance requests created keys: %d, %v", len(listed), err)
	}

	key := issueTestKey(t, f, issuerToken, body)
	if key.ApplicationID != appID || key.IssuerID != "game" || key.ExpiresAt == nil || !key.ExpiresAt.Equal(deadline) ||
		key.MaxRPM == nil || *key.MaxRPM != 20 || key.MaxRPD == nil || *key.MaxRPD != 50 ||
		key.MaxBudget == nil || *key.MaxBudget != 2.5 || key.SoftBudget == nil || *key.SoftBudget != 1 || key.AllowDecisions {
		t.Fatal("issued key did not preserve requested scope and inherited limits")
	}
	stored, err := f.store.GetAPIKeyByID(t.Context(), key.ID)
	hash := sha256.Sum256([]byte(key.Token))
	if err != nil || stored == nil || stored.KeyHash != hex.EncodeToString(hash[:]) || !strings.HasPrefix(key.Token, "sk-app-") {
		t.Fatal("issued key was not stored as a standard hashed app key")
	}
	if status, _, response := f.request(t, http.MethodGet, fmt.Sprintf("/admin/applications/%d/keys", appID), "", "", true); status != 200 ||
		strings.Contains(string(response), key.Token) || strings.Contains(string(response), stored.KeyHash) {
		t.Fatal("listing issued keys exposed credentials or failed")
	}
	for _, tc := range []struct {
		method, path, input, token string
		status                     int
	}{
		{http.MethodGet, "/v1/models", "", issuerToken, 401},
		{http.MethodPost, fmt.Sprintf("/admin/applications/%d/keys", appID), body, issuerToken, 401},
		{http.MethodPost, issuancePath, body, key.Token, 401},
		{http.MethodGet, "/v1/models/openrouter", "", key.Token, 404},
		{http.MethodPatch, "/v1/models/openai/costs", `{}`, key.Token, 403},
		{http.MethodPost, "/v1/chat/completions", `{"model":"openrouter","messages":[{"role":"user","content":"hi"}]}`, key.Token, 403},
	} {
		if status, _, _ := f.request(t, tc.method, tc.path, tc.input, tc.token, false); status != tc.status {
			t.Errorf("%s %s returned HTTP %d, want %d", tc.method, tc.path, status, tc.status)
		}
	}
	status, _, models := f.request(t, http.MethodGet, "/v1/models", "", key.Token, false)
	if status != 200 || !strings.Contains(string(models), `"id":"openai"`) || strings.Contains(string(models), `"id":"openrouter"`) {
		t.Fatal("issued key model list was outside its scope")
	}
	status, headers, _ := f.request(t, http.MethodPost, "/v1/chat/completions", `{"model":"openai","messages":[{"role":"user","content":"hi"}]}`, key.Token, false)
	if status != 200 {
		t.Fatalf("issued key generation returned HTTP %d", status)
	}
	record := f.log(t, headers.Get("X-Request-ID"))
	if record.APIKeyID == nil || *record.APIKeyID != key.ID || record.AccountingID == "" {
		t.Fatal("generation accounting was not attributed to the issued key")
	}

	other := issueTestKey(t, f, otherToken, body)
	if other.ApplicationID != otherAppID {
		t.Fatal("issuer application scope was not used")
	}
	path := fmt.Sprintf("%s/%d", issuancePath, key.ID)
	for _, tc := range []struct{ token, path string }{
		{peerToken, path},
		{otherToken, path},
		{issuerToken, fmt.Sprintf("%s/%d", issuancePath, other.ID)},
		{issuerToken, fmt.Sprintf("%s/%.0f", issuancePath, adminKey["id"])},
	} {
		if status, _, _ := f.request(t, http.MethodDelete, tc.path, "", tc.token, false); status != 404 {
			t.Fatalf("out-of-scope revocation returned HTTP %d", status)
		}
	}
	otherReplica := keystore.New(time.Hour)
	if cached, err := otherReplica.Get(t.Context(), key.Token, f.store); err != nil || cached == nil || !cached.Key.IsActive {
		t.Fatal("active key could not be read by another replica")
	}
	rotatedToken := "sk-issuer-" + strings.Repeat("d", 64)
	t.Setenv("PROXY_TEST_GAME_ISSUER", rotatedToken)
	rejectedConfig := strings.Replace(string(config), "openai/test", "unregistered/test", 1)
	if err := os.WriteFile(f.configPath, []byte(rejectedConfig), 0600); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := f.request(t, http.MethodPost, "/admin/reload", "", "", true); status != 500 {
		t.Fatalf("rejected configuration returned HTTP %d", status)
	}
	if status, _, _ := f.request(t, http.MethodDelete, path, "", rotatedToken, false); status != 401 {
		t.Fatalf("failed reload published new issuer credential: HTTP %d", status)
	}
	if err := os.WriteFile(f.configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := f.request(t, http.MethodPost, "/admin/reload", "", "", true); status != 200 {
		t.Fatalf("issuer rotation reload returned HTTP %d", status)
	}
	if status, _, _ := f.request(t, http.MethodDelete, path, "", issuerToken, false); status != 401 {
		t.Fatalf("rotated-out credential returned HTTP %d", status)
	}
	for range 2 {
		if status, _, _ := f.request(t, http.MethodDelete, path, "", rotatedToken, false); status != 204 {
			t.Fatalf("scoped revocation returned HTTP %d", status)
		}
	}
	cached, err := otherReplica.Get(t.Context(), key.Token, f.store)
	if err != nil || cached == nil || cached.Key.IsActive || cached.Key.IssuerRevokedAt == nil {
		t.Fatal("revocation was not visible across replicas or its audit timestamp was lost")
	}
	if status, _, _ := f.request(t, http.MethodGet, "/v1/models", "", key.Token, false); status != 401 {
		t.Fatalf("revoked key returned HTTP %d", status)
	}
	if _, err := f.store.DB().ExecContext(t.Context(), "UPDATE api_keys SET expires_at_ms = ? WHERE id = ?", time.Now().Add(-time.Minute).UnixMilli(), other.ID); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := f.request(t, http.MethodGet, "/v1/models", "", other.Token, false); status != 401 {
		t.Fatalf("expired key returned HTTP %d", status)
	}
	if status, _, _ := f.request(t, http.MethodDelete, fmt.Sprintf("%s/%d", issuancePath, other.ID), "", otherToken, false); status != 204 {
		t.Fatalf("revocation of expired owned key returned HTTP %d", status)
	}
}

type issuedTestKey struct {
	storage.APIKey
	Token string `json:"key"`
}

func issueTestKey(t *testing.T, f *migrationFixture, token, body string) issuedTestKey {
	t.Helper()
	status, header, result := f.request(t, http.MethodPost, issuancePath, body, token, false)
	var key issuedTestKey
	if status != 201 || header.Get("Cache-Control") != "no-store" || json.Unmarshal(result, &key) != nil ||
		key.ID == 0 || key.Token == "" || strings.Contains(string(result), "key_hash") || strings.Contains(string(result), token) {
		t.Fatalf("invalid issuance response, HTTP %d", status)
	}
	return key
}
