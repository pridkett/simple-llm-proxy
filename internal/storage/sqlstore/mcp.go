package sqlstore

import (
	"context"
	"database/sql"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// MCPConnectionsSchema is shared by the SQLite and PostgreSQL migrations.
const MCPConnectionsSchema = `CREATE TABLE IF NOT EXISTS mcp_connections (
	server_binding TEXT NOT NULL,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	ciphertext BYTEA,
	token_hash TEXT UNIQUE,
	token_expires BIGINT,
	PRIMARY KEY (server_binding, user_id)
)`

func scanMCPConnection(row *sql.Row) (*storage.MCPConnection, error) {
	var c storage.MCPConnection
	var expires sql.NullInt64
	err := row.Scan(&c.UserID, &c.Ciphertext, &expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	c.TokenExpires = expires.Int64
	return &c, err
}

func (s *Storage) GetMCPConnection(ctx context.Context, binding, userID string) (*storage.MCPConnection, error) {
	return scanMCPConnection(s.db.QueryRowContext(ctx, s.bind(`
		SELECT user_id, ciphertext, token_expires FROM mcp_connections
		WHERE server_binding = ? AND user_id = ?`), binding, userID))
}

func (s *Storage) GetMCPConnectionByToken(ctx context.Context, binding, hash string, now int64) (*storage.MCPConnection, error) {
	return scanMCPConnection(s.db.QueryRowContext(ctx, s.bind(`
		SELECT user_id, ciphertext, token_expires FROM mcp_connections
		JOIN users ON users.id = mcp_connections.user_id
		WHERE server_binding = ? AND token_hash = ? AND token_expires > ?`), binding, hash, now))
}

func (s *Storage) SetMCPKey(ctx context.Context, binding, userID string, ciphertext []byte) error {
	_, err := s.db.ExecContext(ctx, s.bind(`
		INSERT INTO mcp_connections (server_binding, user_id, ciphertext) VALUES (?, ?, ?)
		ON CONFLICT (server_binding, user_id) DO UPDATE SET ciphertext = excluded.ciphertext`),
		binding, userID, ciphertext)
	return err
}

func (s *Storage) SetMCPToken(ctx context.Context, binding, userID, hash string, expires int64, needsKey bool) (bool, error) {
	if needsKey {
		result, err := s.db.ExecContext(ctx, s.bind(`
			UPDATE mcp_connections SET token_hash = ?, token_expires = ?
			WHERE server_binding = ? AND user_id = ? AND ciphertext IS NOT NULL`),
			hash, expires, binding, userID)
		if err != nil {
			return false, err
		}
		n, err := result.RowsAffected()
		return n == 1, err
	}
	_, err := s.db.ExecContext(ctx, s.bind(`
		INSERT INTO mcp_connections (server_binding, user_id, token_hash, token_expires) VALUES (?, ?, ?, ?)
		ON CONFLICT (server_binding, user_id) DO UPDATE
		SET token_hash = excluded.token_hash, token_expires = excluded.token_expires`),
		binding, userID, hash, expires)
	return err == nil, err
}

func (s *Storage) RevokeMCPToken(ctx context.Context, binding, userID string) error {
	_, err := s.db.ExecContext(ctx, s.bind(`
		UPDATE mcp_connections SET token_hash = NULL, token_expires = NULL
		WHERE server_binding = ? AND user_id = ?`), binding, userID)
	return err
}

func (s *Storage) DeleteMCPConnection(ctx context.Context, binding, userID string) error {
	_, err := s.db.ExecContext(ctx, s.bind(`
		DELETE FROM mcp_connections WHERE server_binding = ? AND user_id = ?`), binding, userID)
	return err
}
