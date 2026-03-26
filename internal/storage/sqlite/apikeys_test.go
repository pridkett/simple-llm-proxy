package sqlite

import (
	"context"
	"testing"
)

// TestAPIKeySchema verifies that api_keys and key_allowed_models tables exist after migration.
func TestAPIKeySchema(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	expectedTables := []string{"api_keys", "key_allowed_models"}
	for _, table := range expectedTables {
		var name string
		err := s.db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found after migration: %v", table, err)
		}
	}
}

// TestCreateAPIKey verifies creating a key with allowlist entries.
func TestCreateAPIKey(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Set up prerequisite: team + application
	team, err := s.CreateTeam(ctx, "test-team")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	app, err := s.CreateApplication(ctx, team.ID, "test-app")
	if err != nil {
		t.Fatalf("create application: %v", err)
	}

	maxRPM := 100
	maxRPD := 1000
	maxBudget := 10.0

	key, err := s.CreateAPIKey(ctx, app.ID, "my-key", "abc12345", "sha256hashvalue",
		&maxRPM, &maxRPD, &maxBudget, nil, []string{"gpt-4", "claude-3"})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	if key.ID == 0 {
		t.Error("expected non-zero key ID")
	}
	if key.ApplicationID != app.ID {
		t.Errorf("application_id: got %d, want %d", key.ApplicationID, app.ID)
	}
	if key.Name != "my-key" {
		t.Errorf("name: got %q, want %q", key.Name, "my-key")
	}
	if key.KeyPrefix != "abc12345" {
		t.Errorf("key_prefix: got %q, want %q", key.KeyPrefix, "abc12345")
	}
	if key.KeyHash != "sha256hashvalue" {
		t.Errorf("key_hash: got %q, want %q", key.KeyHash, "sha256hashvalue")
	}
	if key.MaxRPM == nil || *key.MaxRPM != maxRPM {
		t.Errorf("max_rpm: got %v, want %d", key.MaxRPM, maxRPM)
	}
	if key.MaxBudget == nil || *key.MaxBudget != maxBudget {
		t.Errorf("max_budget: got %v, want %f", key.MaxBudget, maxBudget)
	}
	if key.SoftBudget != nil {
		t.Errorf("soft_budget: expected nil, got %v", key.SoftBudget)
	}
	if !key.IsActive {
		t.Error("expected is_active = true")
	}
}

// TestGetAPIKeyByHash verifies lookup by hash.
func TestGetAPIKeyByHash(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, _ := s.CreateTeam(ctx, "team1")
	app, _ := s.CreateApplication(ctx, team.ID, "app1")

	_, err := s.CreateAPIKey(ctx, app.ID, "k1", "prefix01", "hashvalue1", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// Found case
	found, err := s.GetAPIKeyByHash(ctx, "hashvalue1")
	if err != nil {
		t.Fatalf("get api key by hash: %v", err)
	}
	if found == nil {
		t.Fatal("expected key, got nil")
	}
	if found.KeyHash != "hashvalue1" {
		t.Errorf("hash: got %q, want %q", found.KeyHash, "hashvalue1")
	}

	// Not found case — must return (nil, nil)
	notFound, err := s.GetAPIKeyByHash(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("get api key by hash (not found): %v", err)
	}
	if notFound != nil {
		t.Errorf("expected nil for missing key, got %+v", notFound)
	}
}

// TestListAPIKeys verifies listing keys for an application.
func TestListAPIKeys(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, _ := s.CreateTeam(ctx, "team2")
	app, _ := s.CreateApplication(ctx, team.ID, "app2")

	_, err := s.CreateAPIKey(ctx, app.ID, "key-a", "prefixaa", "hash-a", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create key-a: %v", err)
	}
	_, err = s.CreateAPIKey(ctx, app.ID, "key-b", "prefixbb", "hash-b", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create key-b: %v", err)
	}

	keys, err := s.ListAPIKeys(ctx, app.ID)
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

// TestRevokeAPIKey verifies is_active is set to FALSE.
func TestRevokeAPIKey(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, _ := s.CreateTeam(ctx, "team3")
	app, _ := s.CreateApplication(ctx, team.ID, "app3")

	key, err := s.CreateAPIKey(ctx, app.ID, "to-revoke", "prefixrc", "hash-revoke", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	if err := s.RevokeAPIKey(ctx, key.ID); err != nil {
		t.Fatalf("revoke api key: %v", err)
	}

	found, err := s.GetAPIKeyByHash(ctx, "hash-revoke")
	if err != nil {
		t.Fatalf("get revoked key: %v", err)
	}
	if found == nil {
		t.Fatal("revoked key should still exist, got nil")
	}
	if found.IsActive {
		t.Error("expected is_active = false after revoke")
	}

	// Revoking non-existent key should return error
	err = s.RevokeAPIKey(ctx, 99999)
	if err == nil {
		t.Error("expected error revoking non-existent key, got nil")
	}
}

// TestGetKeyAllowedModels verifies the allowlist returns the correct model names.
func TestGetKeyAllowedModels(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	team, _ := s.CreateTeam(ctx, "team4")
	app, _ := s.CreateApplication(ctx, team.ID, "app4")

	// Key with allowlist
	key, err := s.CreateAPIKey(ctx, app.ID, "k-models", "prefixmd", "hash-models", nil, nil, nil, nil,
		[]string{"gpt-4", "gpt-3.5-turbo"})
	if err != nil {
		t.Fatalf("create key with models: %v", err)
	}

	models, err := s.GetKeyAllowedModels(ctx, key.ID)
	if err != nil {
		t.Fatalf("get key allowed models: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d: %v", len(models), models)
	}

	// Key with no allowlist — should return empty slice
	keyEmpty, err := s.CreateAPIKey(ctx, app.ID, "k-empty", "prefixem", "hash-empty", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create key without models: %v", err)
	}
	emptyModels, err := s.GetKeyAllowedModels(ctx, keyEmpty.ID)
	if err != nil {
		t.Fatalf("get allowed models for empty key: %v", err)
	}
	if len(emptyModels) != 0 {
		t.Errorf("expected empty model list, got %d", len(emptyModels))
	}
}

// TestRecordKeySpend verifies the stub returns nil (no-op).
func TestRecordKeySpend(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.RecordKeySpend(ctx, 1, 0.50)
	if err != nil {
		t.Errorf("RecordKeySpend stub: expected nil, got %v", err)
	}
}
