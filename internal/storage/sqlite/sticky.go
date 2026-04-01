package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// GetStickySession returns the deployment_key for the given session_key and pool.
// Returns ("", nil) if no session exists or if expired (last_used_at > 1 hour ago).
func (s *Storage) GetStickySession(ctx context.Context, sessionKey, poolName string) (string, error) {
	var deploymentKey string
	err := s.db.QueryRowContext(ctx, `
		SELECT deployment_key
		FROM sticky_routing_sessions
		WHERE session_key = ? AND pool_name = ?
		  AND last_used_at > datetime('now', '-1 hour')
	`, sessionKey, poolName).Scan(&deploymentKey)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("getting sticky session: %w", err)
	}
	return deploymentKey, nil
}

// UpsertStickySession creates or updates a sticky session mapping.
func (s *Storage) UpsertStickySession(ctx context.Context, sessionKey, poolName, deploymentKey string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sticky_routing_sessions (session_key, pool_name, deployment_key, last_used_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(session_key) DO UPDATE SET
			pool_name      = excluded.pool_name,
			deployment_key = excluded.deployment_key,
			last_used_at   = datetime('now')
	`, sessionKey, poolName, deploymentKey)
	if err != nil {
		return fmt.Errorf("upserting sticky session: %w", err)
	}
	return nil
}

// DeleteExpiredStickySessions removes sessions where last_used_at < cutoff.
func (s *Storage) DeleteExpiredStickySessions(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM sticky_routing_sessions WHERE last_used_at < ?
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("deleting expired sticky sessions: %w", err)
	}
	return result.RowsAffected()
}

// BulkUpsertStickySessions writes multiple sessions in a single transaction.
func (s *Storage) BulkUpsertStickySessions(ctx context.Context, sessions []storage.StickySession) error {
	if len(sessions) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning sticky session bulk upsert tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO sticky_routing_sessions (session_key, pool_name, deployment_key, last_used_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(session_key) DO UPDATE SET
			pool_name      = excluded.pool_name,
			deployment_key = excluded.deployment_key,
			last_used_at   = excluded.last_used_at
	`)
	if err != nil {
		return fmt.Errorf("preparing sticky session upsert: %w", err)
	}
	defer stmt.Close()

	for _, sess := range sessions {
		_, err := stmt.ExecContext(ctx, sess.SessionKey, sess.PoolName, sess.DeploymentKey, sess.LastUsedAt.UTC())
		if err != nil {
			return fmt.Errorf("upserting sticky session %q: %w", sess.SessionKey, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing sticky session bulk upsert: %w", err)
	}
	return nil
}
