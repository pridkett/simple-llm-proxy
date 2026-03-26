package sqlite

import (
	"context"
	"fmt"
)

// migrate runs database migrations that have not yet been applied.
// The schema_migrations table is bootstrapped first so it can track all versions.
func (s *Storage) migrate(ctx context.Context) error {
	const bootstrap = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	if _, err := s.db.ExecContext(ctx, bootstrap); err != nil {
		return fmt.Errorf("bootstrap schema_migrations: %w", err)
	}

	migrations := []string{
		// Migration 1: Create api_keys table (future-ready)
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key_hash TEXT UNIQUE NOT NULL,
			key_alias TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME,
			is_active BOOLEAN DEFAULT TRUE,
			max_rpm INTEGER,
			max_tpm INTEGER,
			max_budget REAL,
			allowed_models TEXT
		)`,

		// Migration 2: Create usage_logs table
		`CREATE TABLE IF NOT EXISTS usage_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			api_key_id INTEGER,
			model TEXT NOT NULL,
			provider TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			prompt_tokens INTEGER DEFAULT 0,
			completion_tokens INTEGER DEFAULT 0,
			total_cost REAL DEFAULT 0,
			status_code INTEGER NOT NULL,
			latency_ms INTEGER NOT NULL,
			request_time DATETIME NOT NULL,
			FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
		)`,

		// Migration 3: Create indexes for usage_logs
		`CREATE INDEX IF NOT EXISTS idx_usage_logs_request_time ON usage_logs(request_time)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_logs_model ON usage_logs(model)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_logs_api_key_id ON usage_logs(api_key_id)`,

		// Migration 4: Create migrations tracking table
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Migration 5: Create cost_overrides table for model cost map mappings
		`CREATE TABLE IF NOT EXISTS cost_overrides (
			model_name   TEXT PRIMARY KEY,
			cost_map_key TEXT,
			custom_spec  TEXT,
			updated_at   DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,

		// Migration 6: users table — id IS the OIDC sub claim (TEXT PK, not UUID)
		`CREATE TABLE IF NOT EXISTS users (
			id         TEXT PRIMARY KEY,
			email      TEXT NOT NULL,
			name       TEXT NOT NULL,
			is_admin   BOOLEAN NOT NULL DEFAULT FALSE,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			last_seen  DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,

		// Migration 7: teams table
		`CREATE TABLE IF NOT EXISTS teams (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT NOT NULL UNIQUE,
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,

		// Migration 8: team_members — ON DELETE CASCADE on BOTH FK sides
		// If a team is deleted, its memberships are removed.
		// If a user is deleted, their memberships are removed.
		`CREATE TABLE IF NOT EXISTS team_members (
			team_id INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
			user_id TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role    TEXT    NOT NULL CHECK(role IN ('admin','member','viewer')),
			PRIMARY KEY (team_id, user_id)
		)`,

		// Migration 9: applications — ON DELETE CASCADE (team deleted removes its apps)
		`CREATE TABLE IF NOT EXISTS applications (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			team_id    INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
			name       TEXT    NOT NULL,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			UNIQUE(team_id, name)
		)`,

		// Migration 10: sessions table — backing store for SCS session manager
		`CREATE TABLE IF NOT EXISTS sessions (
			token  TEXT PRIMARY KEY,
			data   BLOB     NOT NULL,
			expiry DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expiry)`,

		// Migration 11: Drop placeholder api_keys table and recreate with full Phase 2 schema.
		// Safe: no production key data exists. DROP removes the old schema completely.
		// SQLite FK note (D-04): usage_logs.api_key_id was defined as a FK in the original placeholder
		// migration. SQLite stores FK constraints as text metadata only — they are not re-validated
		// after a DROP+CREATE of the referenced table. After this migration, usage_logs.api_key_id
		// continues to reference the new api_keys(id) without requiring an explicit ALTER TABLE.
		// This is safe because: (1) SQLite deferred FK enforcement validates at insert time, not schema
		// creation time; (2) no usage_logs rows with api_key_id values exist yet (Phase 1 had no keys).
		`DROP TABLE IF EXISTS api_keys`,
		`CREATE TABLE api_keys (
			id             INTEGER  PRIMARY KEY AUTOINCREMENT,
			application_id INTEGER  NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
			name           TEXT     NOT NULL,
			key_prefix     TEXT     NOT NULL,
			key_hash       TEXT     NOT NULL UNIQUE,
			max_rpm        INTEGER,
			max_rpd        INTEGER,
			max_budget     REAL,
			soft_budget    REAL,
			is_active      BOOLEAN  NOT NULL DEFAULT TRUE,
			created_at     DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,

		// Migration 12: Model allowlists — one row per (key, model) pair.
		// Empty set for a key means all models are allowed.
		`CREATE TABLE IF NOT EXISTS key_allowed_models (
			key_id     INTEGER NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
			model_name TEXT    NOT NULL,
			PRIMARY KEY (key_id, model_name)
		)`,

		// Migration 13: Indexes for key lookup hot paths.
		`CREATE INDEX IF NOT EXISTS idx_api_keys_application_id ON api_keys(application_id)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash)`,
	}

	for i, sql := range migrations {
		version := i + 1
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if count > 0 {
			continue // already applied
		}
		if _, err := s.db.ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("migration %d failed: %w", version, err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			return fmt.Errorf("record migration %d: %w", version, err)
		}
	}

	return nil
}
