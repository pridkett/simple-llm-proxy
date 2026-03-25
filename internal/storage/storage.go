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
