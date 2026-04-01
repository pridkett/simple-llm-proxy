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

		// Migrations 14-29: v1.1 schema additions

		// Migration 14: Drop usage_logs to remove old column names (prompt_tokens, completion_tokens).
		// v1.0 history is intentionally discarded — no production usage data exists yet.
		`DROP TABLE IF EXISTS usage_logs`,

		// Migration 15: Recreate usage_logs with renamed columns (input_tokens, output_tokens)
		// and new columns (is_streaming, cache_read_tokens, cache_write_tokens, deployment_key).
		`CREATE TABLE usage_logs (
    id                 INTEGER  PRIMARY KEY AUTOINCREMENT,
    request_id         TEXT     NOT NULL,
    api_key_id         INTEGER,
    model              TEXT     NOT NULL,
    provider           TEXT     NOT NULL,
    endpoint           TEXT     NOT NULL,
    input_tokens       INTEGER  NOT NULL DEFAULT 0,
    output_tokens      INTEGER  NOT NULL DEFAULT 0,
    total_cost         REAL              DEFAULT 0,
    status_code        INTEGER  NOT NULL,
    latency_ms         INTEGER  NOT NULL,
    request_time       DATETIME NOT NULL,
    is_streaming       BOOLEAN  NOT NULL DEFAULT 0,
    cache_read_tokens  INTEGER  NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER  NOT NULL DEFAULT 0,
    deployment_key     TEXT,
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
)`,

		// Migration 16: Recreate usage_logs indexes (dropped with the table in migration 14).
		`CREATE INDEX IF NOT EXISTS idx_usage_logs_request_time ON usage_logs(request_time)`,

		// Migration 17
		`CREATE INDEX IF NOT EXISTS idx_usage_logs_model ON usage_logs(model)`,

		// Migration 18
		`CREATE INDEX IF NOT EXISTS idx_usage_logs_api_key_id ON usage_logs(api_key_id)`,

		// Migration 19: provider_pools — named pools with routing strategy and daily budget cap.
		// Per SCHEMA-01. Rows populated by Phase 7 pool routing; table dormant until then.
		`CREATE TABLE IF NOT EXISTS provider_pools (
    id               INTEGER  PRIMARY KEY AUTOINCREMENT,
    name             TEXT     NOT NULL UNIQUE,
    strategy         TEXT     NOT NULL DEFAULT 'weighted-round-robin',
    budget_cap_daily REAL,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now'))
)`,

		// Migration 20: routing_rules — per-pool, per-model weight overrides.
		// Per SCHEMA-02. UNIQUE(pool_name, model_name) enforces one weight entry per member.
		`CREATE TABLE IF NOT EXISTS routing_rules (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    pool_name  TEXT     NOT NULL,
    model_name TEXT     NOT NULL,
    weight     INTEGER  NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(pool_name, model_name)
)`,

		// Migration 21: webhook_subscriptions — UI-created webhooks only.
		// Per SCHEMA-03. YAML-defined webhooks are held in memory and never written here.
		`CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    url        TEXT     NOT NULL,
    events     TEXT     NOT NULL,
    secret     TEXT,
    enabled    BOOLEAN  NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
)`,

		// Migration 22: notification_events — routing event log, retained 30 days.
		// Per SCHEMA-04. payload is a JSON blob (TEXT).
		`CREATE TABLE IF NOT EXISTS notification_events (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    event_type TEXT     NOT NULL,
    payload    TEXT     NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
)`,

		// Migration 23: Index notification_events(created_at) for TTL cleanup queries.
		`CREATE INDEX IF NOT EXISTS idx_notification_events_created_at ON notification_events(created_at)`,

		// Migration 24: Index notification_events(event_type) for event-type fan-out queries.
		`CREATE INDEX IF NOT EXISTS idx_notification_events_event_type ON notification_events(event_type)`,

		// Migration 25: webhook_deliveries — delivery queue with retry tracking.
		// Per SCHEMA-05. FKs to webhook_subscriptions and notification_events (parent tables created above).
		`CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id              INTEGER  PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER  NOT NULL REFERENCES webhook_subscriptions(id),
    event_id        INTEGER  NOT NULL REFERENCES notification_events(id),
    attempt_count   INTEGER  NOT NULL DEFAULT 0,
    last_attempt_at DATETIME,
    status          TEXT     NOT NULL DEFAULT 'pending',
    response_code   INTEGER,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
)`,

		// Migration 26: Index webhook_deliveries(subscription_id) for per-subscription delivery lookup.
		`CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_subscription_id ON webhook_deliveries(subscription_id)`,

		// Migration 27: pool_budget_state — per-pool daily spend accumulator.
		// Per SCHEMA-07. reset_date is DATE string 'YYYY-MM-DD' (UTC). Reset handled by Phase 8.
		`CREATE TABLE IF NOT EXISTS pool_budget_state (
    pool_name   TEXT  PRIMARY KEY,
    spend_today REAL  NOT NULL DEFAULT 0,
    reset_date  DATE  NOT NULL
)`,

		// Migration 28: sticky_routing_sessions — client-to-deployment mapping for session affinity.
		// Per SCHEMA-08. last_used_at indexed for expiry cleanup (sessions inactive >1 hour expire in Phase 7).
		`CREATE TABLE IF NOT EXISTS sticky_routing_sessions (
    session_key    TEXT     PRIMARY KEY,
    pool_name      TEXT     NOT NULL,
    deployment_key TEXT     NOT NULL,
    last_used_at   DATETIME NOT NULL DEFAULT (datetime('now'))
)`,

		// Migration 29: Index sticky_routing_sessions(last_used_at) for expiry cleanup.
		`CREATE INDEX IF NOT EXISTS idx_sticky_routing_sessions_last_used_at ON sticky_routing_sessions(last_used_at)`,

		// Migration 30: Drop webhook_deliveries to fix missing ON DELETE CASCADE
		// and NOT NULL constraint on subscription_id. No production data exists.
		`DROP TABLE IF EXISTS webhook_deliveries`,

		// Migration 31: Recreate webhook_deliveries with ON DELETE CASCADE on both FKs
		// and nullable subscription_id for YAML webhook delivery tracking.
		`CREATE TABLE webhook_deliveries (
    id              INTEGER  PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER  REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_id        INTEGER  NOT NULL REFERENCES notification_events(id) ON DELETE CASCADE,
    attempt_count   INTEGER  NOT NULL DEFAULT 0,
    last_attempt_at DATETIME,
    status          TEXT     NOT NULL DEFAULT 'pending',
    response_code   INTEGER,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
)`,

		// Migration 32: Recreate the index dropped with the table in migration 30.
		`CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_subscription_id ON webhook_deliveries(subscription_id)`,
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
