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
	}

	for i, migration := range migrations {
		if _, err := s.db.ExecContext(ctx, migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}

	return nil
}
