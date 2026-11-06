package sqlstore

import (
	"context"
	"database/sql"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// An empty owner identifies the explicitly shared upstream account.
const MCPOAuthSchema = `CREATE TABLE IF NOT EXISTS mcp_oauth_credentials (
	server_binding TEXT NOT NULL,
	owner TEXT NOT NULL,
	user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
	ciphertext BYTEA NOT NULL,
	generation TEXT NOT NULL,
	CHECK ((owner = '' AND user_id IS NULL) OR (owner <> '' AND user_id IS NOT NULL AND user_id = owner)),
	PRIMARY KEY (server_binding, owner)
)`

func (s *Storage) GetMCPOAuth(ctx context.Context, binding, owner string) (*storage.MCPOAuthCredential, error) {
	var credential storage.MCPOAuthCredential
	err := s.db.QueryRowContext(ctx, s.bind(`
		SELECT ciphertext, generation FROM mcp_oauth_credentials
		WHERE server_binding = ? AND owner = ?`), binding, owner).Scan(&credential.Ciphertext, &credential.Generation)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &credential, err
}

func (s *Storage) PutMCPOAuth(ctx context.Context, binding, owner string, credential storage.MCPOAuthCredential) error {
	_, err := s.db.ExecContext(ctx, s.bind(`
		INSERT INTO mcp_oauth_credentials (server_binding, owner, user_id, ciphertext, generation) VALUES (?, ?, NULLIF(?, ''), ?, ?)
		ON CONFLICT (server_binding, owner) DO UPDATE SET ciphertext = excluded.ciphertext, generation = excluded.generation`),
		binding, owner, owner, credential.Ciphertext, credential.Generation)
	return err
}

func (s *Storage) RefreshMCPOAuth(ctx context.Context, binding, owner string, old, updated []byte) (bool, error) {
	result, err := s.db.ExecContext(ctx, s.bind(`
		UPDATE mcp_oauth_credentials SET ciphertext = ?
		WHERE server_binding = ? AND owner = ? AND ciphertext = ?`), updated, binding, owner, old)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *Storage) DeleteMCPOAuth(ctx context.Context, binding, owner string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, s.bind(`
		DELETE FROM mcp_oauth_credentials WHERE server_binding = ? AND owner = ?`), binding, owner); err != nil {
		return err
	}
	if owner != "" {
		if _, err := tx.ExecContext(ctx, s.bind(`
			DELETE FROM mcp_connections WHERE server_binding = ? AND user_id = ?`), binding, owner); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Storage) SetMCPOAuthToken(ctx context.Context, binding, userID, hash string, expires int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, s.bind(`
		INSERT INTO mcp_connections (server_binding, user_id, token_hash, token_expires)
		SELECT ?, ?, ?, ? WHERE EXISTS (
			SELECT 1 FROM mcp_oauth_credentials WHERE server_binding = ? AND owner = ?
		)
		ON CONFLICT (server_binding, user_id) DO UPDATE
		SET token_hash = excluded.token_hash, token_expires = excluded.token_expires`),
		binding, userID, hash, expires, binding, userID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
