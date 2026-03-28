package sqlite

import (
	"context"
	"testing"
)

// TestMigrate verifies that migrate() creates all expected tables and indexes.
// This test runs after ALL migrations are applied (via newTestStorage → Initialize).
func TestMigrate(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Verify all tables exist after migrations
	expectedTables := []string{
		"api_keys",
		"usage_logs",
		"cost_overrides",
		"users",
		"teams",
		"team_members",
		"applications",
		"sessions",
		"key_allowed_models",
		// New v1.1 tables (added by this plan)
		"provider_pools",
		"routing_rules",
		"webhook_subscriptions",
		"notification_events",
		"webhook_deliveries",
		"pool_budget_state",
		"sticky_routing_sessions",
	}

	for _, table := range expectedTables {
		var name string
		err := s.db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found after migrations: %v", table, err)
		}
	}

	// Verify usage_logs has the new column names (not prompt_tokens/completion_tokens)
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info(usage_logs)")
	if err != nil {
		t.Fatalf("PRAGMA table_info(usage_logs): %v", err)
	}
	defer rows.Close()

	colNames := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue *string
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			t.Fatalf("scanning column info: %v", err)
		}
		colNames[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating column info: %v", err)
	}

	requiredCols := []string{
		"id", "request_id", "api_key_id", "model", "provider", "endpoint",
		"input_tokens", "output_tokens", "total_cost", "status_code", "latency_ms",
		"request_time", "is_streaming", "cache_read_tokens", "cache_write_tokens", "deployment_key",
	}
	for _, col := range requiredCols {
		if !colNames[col] {
			t.Errorf("usage_logs missing column %q after migration", col)
		}
	}

	// Old column names must be gone
	for _, old := range []string{"prompt_tokens", "completion_tokens"} {
		if colNames[old] {
			t.Errorf("usage_logs still has old column %q — migration 14 (DROP) may not have run", old)
		}
	}
}

// TestMigrationIdempotency verifies that running migrate() a second time
// does not fail or produce duplicate schema entries.
func TestMigrationIdempotency(t *testing.T) {
	s := newTestStorage(t) // first migrate() call happens here
	ctx := context.Background()

	// Second call must succeed without error
	if err := s.migrate(ctx); err != nil {
		t.Fatalf("second migrate() call failed (not idempotent): %v", err)
	}

	// schema_migrations should have exactly 29 rows (versions 1–29)
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("counting schema_migrations: %v", err)
	}
	if count != 29 {
		t.Errorf("expected 29 rows in schema_migrations, got %d", count)
	}
}
