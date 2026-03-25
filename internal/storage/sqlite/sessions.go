package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SessionStore implements the scs.CtxStore interface using modernc.org/sqlite.
// It does NOT use scs/sqlite3store (which requires CGO via mattn/go-sqlite3).
// This keeps the project CGO-free per ADR 003 §2.
type SessionStore struct {
	DB *sql.DB
}

// FindCtx returns the session data for the given token.
// Returns (nil, false, nil) if the token is not found or the session has expired.
func (s *SessionStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	var data []byte
	err := s.DB.QueryRowContext(ctx,
		"SELECT data FROM sessions WHERE token = ? AND expiry > datetime('now')",
		token,
	).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// CommitCtx stores or replaces the session data for the given token with the given expiry.
func (s *SessionStore) CommitCtx(ctx context.Context, token string, b []byte, expiry time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		"INSERT OR REPLACE INTO sessions (token, data, expiry) VALUES (?, ?, ?)",
		token, b, expiry.UTC().Format("2006-01-02 15:04:05"),
	)
	return err
}

// DeleteCtx removes the session with the given token.
func (s *SessionStore) DeleteCtx(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM sessions WHERE token = ?", token)
	return err
}

// Find is the synchronous wrapper for FindCtx, required by some scs versions.
func (s *SessionStore) Find(token string) ([]byte, bool, error) {
	return s.FindCtx(context.Background(), token)
}

// Commit is the synchronous wrapper for CommitCtx, required by some scs versions.
func (s *SessionStore) Commit(token string, b []byte, expiry time.Time) error {
	return s.CommitCtx(context.Background(), token, b, expiry)
}

// Delete is the synchronous wrapper for DeleteCtx, required by some scs versions.
func (s *SessionStore) Delete(token string) error {
	return s.DeleteCtx(context.Background(), token)
}
