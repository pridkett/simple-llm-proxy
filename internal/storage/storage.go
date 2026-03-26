package storage

import (
	"context"
	"time"
)

// Storage defines the interface for persistence.
type Storage interface {
	// Initialize sets up the storage (creates tables, etc).
	Initialize(ctx context.Context) error

	// Close closes the storage connection.
	Close() error

	// LogRequest logs a request for usage tracking.
	LogRequest(ctx context.Context, log *RequestLog) error

	// GetLogs returns paginated request logs ordered by most recent first.
	// Returns the logs, total count, and any error.
	GetLogs(ctx context.Context, limit, offset int) ([]*RequestLog, int, error)

	// UpsertCostMapKey sets a cost map key override for the given proxy model name.
	// Clears any existing CustomSpec for that model.
	UpsertCostMapKey(ctx context.Context, modelName, costMapKey string) error

	// UpsertCustomCostSpec stores a custom cost spec (JSON-encoded ModelSpec) for the given
	// proxy model name. Clears any existing CostMapKey for that model.
	UpsertCustomCostSpec(ctx context.Context, modelName, specJSON string) error

	// GetCostOverride returns the override for the given proxy model name.
	// Returns (nil, nil) if no override exists.
	GetCostOverride(ctx context.Context, modelName string) (*CostOverride, error)

	// DeleteCostOverride removes any cost override (key or custom spec) for the given
	// proxy model name. A no-op if no override exists.
	DeleteCostOverride(ctx context.Context, modelName string) error

	// ListCostOverrides returns all stored cost overrides ordered by model name.
	ListCostOverrides(ctx context.Context) ([]*CostOverride, error)

	// --- Identity CRUD ---

	// UpsertUser creates or updates a user record. The user.ID field must be the
	// OIDC sub claim — the stable identity from PocketID. On update, created_at is
	// preserved and last_seen is refreshed.
	UpsertUser(ctx context.Context, user *User) error

	// GetUser returns the user with the given id (OIDC sub claim).
	// Returns (nil, nil) if no user exists with that id.
	GetUser(ctx context.Context, id string) (*User, error)

	// ListUsers returns all users ordered by email.
	ListUsers(ctx context.Context) ([]*User, error)

	// CreateTeam creates a new team with the given name.
	CreateTeam(ctx context.Context, name string) (*Team, error)

	// DeleteTeam deletes the team with the given id.
	// ON DELETE CASCADE removes all team_members and applications for this team.
	DeleteTeam(ctx context.Context, id int64) error

	// ListTeams returns all teams ordered by name.
	ListTeams(ctx context.Context) ([]*Team, error)

	// AddTeamMember adds a user to a team with the given role.
	// Role must be one of "admin", "member", or "viewer".
	AddTeamMember(ctx context.Context, teamID int64, userID string, role string) error

	// RemoveTeamMember removes a user from a team.
	RemoveTeamMember(ctx context.Context, teamID int64, userID string) error

	// UpdateTeamMemberRole updates the role of a team member.
	// Role must be one of "admin", "member", or "viewer".
	UpdateTeamMemberRole(ctx context.Context, teamID int64, userID string, role string) error

	// ListTeamMembers returns all members of a team, joined with user info.
	ListTeamMembers(ctx context.Context, teamID int64) ([]*TeamMember, error)

	// ListMyTeams returns all teams the given user belongs to, joined with team info.
	ListMyTeams(ctx context.Context, userID string) ([]*TeamMember, error)

	// CreateApplication creates a new application scoped to the given team.
	CreateApplication(ctx context.Context, teamID int64, name string) (*Application, error)

	// DeleteApplication deletes the application with the given id.
	DeleteApplication(ctx context.Context, id int64) error

	// ListApplications returns all applications for the given team, ordered by name.
	ListApplications(ctx context.Context, teamID int64) ([]*Application, error)

	// CleanExpiredSessions deletes all sessions whose expiry time is in the past.
	// Should be called periodically (e.g., every hour) to prevent unbounded growth.
	CleanExpiredSessions(ctx context.Context) error

	// --- API Key CRUD ---

	// CreateAPIKey creates a new API key for the given application.
	// keyPrefix is the first 8 hex chars (display only). keyHash is SHA-256 hex (lookup).
	// allowedModels is stored in key_allowed_models; empty slice = all models allowed.
	// Returns the created key record (full plaintext key is NOT stored or returned here).
	CreateAPIKey(ctx context.Context, appID int64, name, keyPrefix, keyHash string, maxRPM, maxRPD *int, maxBudget, softBudget *float64, allowedModels []string) (*APIKey, error)

	// GetAPIKeyByHash looks up a key by its SHA-256 hash. Returns (nil, nil) if not found.
	GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error)

	// ListAPIKeys returns all keys for an application, ordered by created_at DESC.
	ListAPIKeys(ctx context.Context, appID int64) ([]*APIKey, error)

	// RevokeAPIKey marks the key with the given id as inactive (is_active = FALSE).
	// Does not delete the key record — revoked keys remain visible in ListAPIKeys.
	RevokeAPIKey(ctx context.Context, id int64) error

	// GetKeyAllowedModels returns the model names in the allowlist for the given key.
	// Returns an empty slice if no allowlist entries exist (all models allowed).
	GetKeyAllowedModels(ctx context.Context, keyID int64) ([]string, error)

	// RecordKeySpend adds the given cost to usage_logs for the given key.
	// This is a direct INSERT — the spend accumulator (in-memory) is the hot-path; this is the flush mechanism.
	RecordKeySpend(ctx context.Context, keyID int64, cost float64) error

	// GetKeySpendTotals returns the total cost per api_key_id from usage_logs.
	// Used at startup to initialize the in-memory spend accumulator.
	GetKeySpendTotals(ctx context.Context) (map[int64]float64, error)
}

// User represents a proxy user populated from OIDC claims.
// ID is the OIDC subject identifier (sub claim) — NOT an internal UUID.
// Using the sub claim directly avoids fragile account reconciliation.
type User struct {
	ID        string    `json:"id"`       // OIDC sub claim — the stable identity from PocketID
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

// Team represents a named group that owns applications.
type Team struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// TeamMember represents a user's membership in a team with a role.
type TeamMember struct {
	TeamID    int64  `json:"team_id"`
	UserID    string `json:"user_id"` // OIDC sub of the user
	Role      string `json:"role"`    // "admin", "member", or "viewer"
	// Joined fields for convenience:
	UserEmail string `json:"user_email"`
	UserName  string `json:"user_name"`
	TeamName  string `json:"team_name"`
}

// Application represents an app scoped to a team.
type Application struct {
	ID        int64     `json:"id"`
	TeamID    int64     `json:"team_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// APIKey represents a per-application API key with enforcement limits.
// Full plaintext key is NEVER stored — only the prefix (first 8 hex chars) and SHA-256 hash.
type APIKey struct {
	ID            int64    `json:"id"`
	ApplicationID int64    `json:"application_id"`
	Name          string   `json:"name"`
	KeyPrefix     string   `json:"key_prefix"` // first 8 chars after "sk-app-"
	KeyHash       string   `json:"-"`          // SHA-256 hex — never serialized to JSON
	MaxRPM        *int     `json:"max_rpm"`    // nil = unlimited
	MaxRPD        *int     `json:"max_rpd"`    // nil = unlimited
	MaxBudget     *float64 `json:"max_budget"` // nil = unlimited (hard cap)
	SoftBudget    *float64 `json:"soft_budget"` // nil = no alert threshold
	IsActive      bool     `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
}

// APIKeyAllowedModel represents a single model entry in a key's allowlist.
type APIKeyAllowedModel struct {
	KeyID     int64  `json:"key_id"`
	ModelName string `json:"model_name"`
}

// CostOverride records a user-supplied mapping or custom spec for a proxy model name.
// Exactly one of CostMapKey or CustomSpec will be non-nil.
type CostOverride struct {
	ModelName  string
	CostMapKey *string   // if set: use this key for LiteLLM cost map lookup
	CustomSpec *string   // if set: JSON-encoded ModelSpec for fully custom costs
	UpdatedAt  time.Time
}

// RequestLog represents a logged request.
type RequestLog struct {
	RequestID        string
	APIKeyID         *int64  // nil when authenticated via master key
	Model            string
	Provider         string
	Endpoint         string
	PromptTokens     int
	CompletionTokens int
	TotalCost        float64
	StatusCode       int
	LatencyMS        int64
	RequestTime      time.Time
}
