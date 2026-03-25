package sqlite

import (
	"context"
	"fmt"
)

// migrate runs database migrations.
func (s *Storage) migrate(ctx context.Context) error {
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
	}

	for i, migration := range migrations {
		if _, err := s.db.ExecContext(ctx, migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}

	return nil
}
