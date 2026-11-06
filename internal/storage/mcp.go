package storage

import "context"

type MCPConnection struct {
	UserID       string
	Ciphertext   []byte
	TokenExpires int64
}

// MCPStore manages encrypted MCP connections and hashed gateway tokens.
type MCPStore interface {
	GetMCPConnection(ctx context.Context, binding, userID string) (*MCPConnection, error)
	GetMCPConnectionByToken(ctx context.Context, binding, hash string, now int64) (*MCPConnection, error)
	SetMCPKey(ctx context.Context, binding, userID string, ciphertext []byte) error
	SetMCPToken(ctx context.Context, binding, userID, hash string, expires int64, needsKey bool) (bool, error)
	RevokeMCPToken(ctx context.Context, binding, userID string) error
	DeleteMCPConnection(ctx context.Context, binding, userID string) error
}
