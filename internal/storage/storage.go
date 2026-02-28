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
