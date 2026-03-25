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
