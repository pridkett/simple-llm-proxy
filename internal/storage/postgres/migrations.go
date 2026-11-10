package postgres

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlstore"
)

//go:embed schema.sql
var baseline string

// PostgreSQL has its own append-only migration sequence. The initial schema
// describes current state; it does not replay SQLite's destructive history.
var migrations = []string{baseline, `
 CREATE TABLE request_accounting (
 id TEXT PRIMARY KEY, api_key_id BIGINT, request_id TEXT NOT NULL,
 state TEXT NOT NULL, record_json TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
 );
 CREATE INDEX idx_request_accounting_request ON request_accounting(request_id);
 CREATE INDEX idx_request_accounting_created ON request_accounting(created_at);
 ALTER TABLE responses_jobs ADD COLUMN accounting_id TEXT NOT NULL DEFAULT '';
 ALTER TABLE usage_logs ADD COLUMN accounting_id TEXT NOT NULL DEFAULT '';
 ALTER TABLE usage_logs ADD COLUMN cost_source TEXT NOT NULL DEFAULT '';
 ALTER TABLE usage_logs ADD COLUMN cost_known BOOLEAN;
 ALTER TABLE usage_logs ADD COLUMN cost_coverage TEXT NOT NULL DEFAULT '';
 `,
	// Decision access is opt-in for existing and newly created app keys.
	`ALTER TABLE api_keys ADD COLUMN allow_decisions BOOLEAN NOT NULL DEFAULT FALSE;
     CREATE INDEX idx_usage_logs_endpoint_time ON usage_logs(endpoint, request_time DESC);`,
	sqlstore.MCPConnectionsSchema,
	sqlstore.MCPOAuthSchema,
	// Client-supplied end-user attribution (X-User-ID), parallel to session_id.
	`ALTER TABLE usage_logs ADD COLUMN client_user_id TEXT NOT NULL DEFAULT '';
     CREATE INDEX idx_usage_logs_client_user_time ON usage_logs(client_user_id, request_time DESC) WHERE client_user_id <> '';
     ALTER TABLE responses_jobs ADD COLUMN client_user_id TEXT NOT NULL DEFAULT '';`,
	// Spend snapshots were written for every key every 30s even when idle and
	// are never read back; keep only the latest per key.
	`DELETE FROM usage_logs WHERE model = '_flush' AND id NOT IN (SELECT MAX(id) FROM usage_logs WHERE model = '_flush' GROUP BY api_key_id);`,
	`ALTER TABLE api_keys ADD COLUMN expires_at_ms BIGINT;`,
}

func (s *Storage) migrate(ctx context.Context) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL migration: %w", err)
	}
	defer tx.Rollback()
	// Lock before bootstrapping metadata: concurrent CREATE TABLE IF NOT EXISTS
	// alone does not serialize catalog creation on a brand-new database.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(736768014)`); err != nil {
		return fmt.Errorf("lock PostgreSQL migrations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("bootstrap PostgreSQL migrations: %w", err)
	}
	for i, query := range migrations {
		version := i + 1
		var applied bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return fmt.Errorf("check PostgreSQL migration %d: %w", version, err)
		}
		if applied {
			continue
		}
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("PostgreSQL migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, version); err != nil {
			return fmt.Errorf("record PostgreSQL migration %d: %w", version, err)
		}
	}
	return tx.Commit()
}
