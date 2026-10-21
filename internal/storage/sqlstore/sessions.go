package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SessionStore implements scs.CtxStore using the selected SQL dialect.
type SessionStore struct {
	DB      *sql.DB
	Dialect Dialect
}

// FindCtx returns the session data for the given token.
// Returns (nil, false, nil) if the token is not found or the session has expired.
func (s *SessionStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	var data []byte
	err := s.DB.QueryRowContext(ctx,
		s.bind("SELECT data FROM sessions WHERE token = ? AND expiry > CURRENT_TIMESTAMP"),
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
	var expiryArg any = expiry.UTC()
	if s.Dialect == SQLite {
		expiryArg = expiry.UTC().Format("2006-01-02 15:04:05")
	}
	_, err := s.DB.ExecContext(ctx,
		s.bind("INSERT INTO sessions (token, data, expiry) VALUES (?, ?, ?) ON CONFLICT(token) DO UPDATE SET data = excluded.data, expiry = excluded.expiry"),
		token, b, expiryArg,
	)
	return err
}

// DeleteCtx removes the session with the given token.
func (s *SessionStore) DeleteCtx(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, s.bind("DELETE FROM sessions WHERE token = ?"), token)
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
