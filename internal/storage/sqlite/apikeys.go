package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// CreateAPIKey creates a new key record and its allowlist entries in a transaction.
func (s *Storage) CreateAPIKey(ctx context.Context, appID int64, name, keyPrefix, keyHash string, maxRPM, maxRPD *int, maxBudget, softBudget *float64, allowedModels []string) (*storage.APIKey, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create api key begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	key := &storage.APIKey{}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO api_keys (application_id, name, key_prefix, key_hash, max_rpm, max_rpd, max_budget, soft_budget)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, application_id, name, key_prefix, key_hash, max_rpm, max_rpd, max_budget, soft_budget, is_active, created_at
	`, appID, name, keyPrefix, keyHash, maxRPM, maxRPD, maxBudget, softBudget).
		Scan(&key.ID, &key.ApplicationID, &key.Name, &key.KeyPrefix, &key.KeyHash,
			&key.MaxRPM, &key.MaxRPD, &key.MaxBudget, &key.SoftBudget, &key.IsActive, &key.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create api key insert: %w", err)
	}

	for _, model := range allowedModels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO key_allowed_models (key_id, model_name) VALUES (?, ?)`, key.ID, model); err != nil {
			return nil, fmt.Errorf("create api key allowlist: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("create api key commit: %w", err)
	}
	return key, nil
}

// GetAPIKeyByHash looks up a key by SHA-256 hash. Returns (nil, nil) if not found.
func (s *Storage) GetAPIKeyByHash(ctx context.Context, keyHash string) (*storage.APIKey, error) {
	key := &storage.APIKey{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, application_id, name, key_prefix, key_hash, max_rpm, max_rpd, max_budget, soft_budget, is_active, created_at
		FROM api_keys WHERE key_hash = ?
	`, keyHash).Scan(&key.ID, &key.ApplicationID, &key.Name, &key.KeyPrefix, &key.KeyHash,
		&key.MaxRPM, &key.MaxRPD, &key.MaxBudget, &key.SoftBudget, &key.IsActive, &key.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get api key by hash: %w", err)
	}
	return key, nil
}

// ListAPIKeys returns all keys for an application, newest first.
func (s *Storage) ListAPIKeys(ctx context.Context, appID int64) ([]*storage.APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, application_id, name, key_prefix, key_hash, max_rpm, max_rpd, max_budget, soft_budget, is_active, created_at
		FROM api_keys WHERE application_id = ? ORDER BY created_at DESC
	`, appID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	keys := make([]*storage.APIKey, 0)
	for rows.Next() {
		key := &storage.APIKey{}
		if err := rows.Scan(&key.ID, &key.ApplicationID, &key.Name, &key.KeyPrefix, &key.KeyHash,
			&key.MaxRPM, &key.MaxRPD, &key.MaxBudget, &key.SoftBudget, &key.IsActive, &key.CreatedAt); err != nil {
			return nil, fmt.Errorf("list api keys scan: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// RevokeAPIKey marks the key as inactive. Record is preserved for audit.
func (s *Storage) RevokeAPIKey(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE api_keys SET is_active = FALSE WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("revoke api key: key %d not found", id)
	}
	return nil
}

// GetKeyAllowedModels returns the model names in the key's allowlist.
// Empty slice means all models are allowed.
func (s *Storage) GetKeyAllowedModels(ctx context.Context, keyID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT model_name FROM key_allowed_models WHERE key_id = ?`, keyID)
	if err != nil {
		return nil, fmt.Errorf("get key allowed models: %w", err)
	}
	defer rows.Close()

	models := make([]string, 0)
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, fmt.Errorf("get key allowed models scan: %w", err)
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

// UpdateKeyAllowedModels replaces the allowlist for the given key in a transaction.
// Delete-then-insert ensures the allowlist is always consistent.
func (s *Storage) UpdateKeyAllowedModels(ctx context.Context, keyID int64, models []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update key allowed models begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM key_allowed_models WHERE key_id = ?`, keyID); err != nil {
		return fmt.Errorf("update key allowed models delete: %w", err)
	}
	for _, model := range models {
		if _, err := tx.ExecContext(ctx, `INSERT INTO key_allowed_models (key_id, model_name) VALUES (?, ?)`, keyID, model); err != nil {
			return fmt.Errorf("update key allowed models insert: %w", err)
		}
	}
	return tx.Commit()
}

// UpdateAPIKey updates the mutable key fields and replaces the allowed model list
// in a single transaction. Key hash, prefix, and is_active are unchanged.
func (s *Storage) UpdateAPIKey(ctx context.Context, keyID int64, name string, maxRPM, maxRPD *int, maxBudget, softBudget *float64, allowedModels []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update api key begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET name=?, max_rpm=?, max_rpd=?, max_budget=?, soft_budget=?
		WHERE id=?
	`, name, maxRPM, maxRPD, maxBudget, softBudget, keyID); err != nil {
		return fmt.Errorf("update api key: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM key_allowed_models WHERE key_id=?`, keyID); err != nil {
		return fmt.Errorf("update api key delete models: %w", err)
	}
	for _, model := range allowedModels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO key_allowed_models (key_id, model_name) VALUES (?, ?)`, keyID, model); err != nil {
			return fmt.Errorf("update api key insert model: %w", err)
		}
	}
	return tx.Commit()
}

// RecordKeySpend is a no-op stub — spend recording is handled by the extended
// logRequest() in internal/api/handler/chat.go (Plan 04). The accumulator
// flushes via direct usage_logs INSERTs using the existing LogRequest path.
func (s *Storage) RecordKeySpend(ctx context.Context, keyID int64, cost float64) error {
	return nil
}

// GetKeySpendTotals returns the total cost per api_key_id from usage_logs.
// Used at startup to initialize the in-memory spend accumulator.
func (s *Storage) GetKeySpendTotals(ctx context.Context) (map[int64]float64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT api_key_id, SUM(total_cost)
		FROM usage_logs
		WHERE api_key_id IS NOT NULL
		GROUP BY api_key_id
	`)
	if err != nil {
		return nil, fmt.Errorf("get key spend totals: %w", err)
	}
	defer rows.Close()

	totals := make(map[int64]float64)
	for rows.Next() {
		var keyID int64
		var total float64
		if err := rows.Scan(&keyID, &total); err != nil {
			return nil, fmt.Errorf("get key spend totals scan: %w", err)
		}
		totals[keyID] = total
	}
	return totals, rows.Err()
}

// FlushKeySpend inserts a synthetic usage_log flush entry for the key.
// Strategy: append-only flush record. On restart, InitFromStorage sums all rows
// (including flush rows) to restore the accumulator accurately.
func (s *Storage) FlushKeySpend(ctx context.Context, keyID int64, total float64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_logs (api_key_id, total_cost, model, provider, endpoint, request_time, request_id, status_code, latency_ms)
		VALUES (?, ?, '_flush', '_flush', '_flush', datetime('now'),
		        'flush-' || cast(strftime('%s','now') as text), 0, 0)
	`, keyID, total)
	if err != nil {
		return fmt.Errorf("flush key spend: %w", err)
	}
	return nil
}
