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

type MCPOAuthCredential struct {
	Ciphertext []byte
	Generation string
}

type MCPOAuthStore interface {
	GetMCPOAuth(ctx context.Context, binding, owner string) (*MCPOAuthCredential, error)
	PutMCPOAuth(ctx context.Context, binding, owner string, credential MCPOAuthCredential) error
	RefreshMCPOAuth(ctx context.Context, binding, owner string, old, updated []byte) (bool, error)
	DeleteMCPOAuth(ctx context.Context, binding, owner string) error
	SetMCPOAuthToken(ctx context.Context, binding, userID, hash string, expires int64) (bool, error)
}
