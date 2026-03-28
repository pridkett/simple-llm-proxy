package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

// Provider defines the interface that all LLM providers must implement.
type Provider interface {
	// Name returns the provider name (e.g., "openai", "anthropic").
	Name() string

	// ChatCompletion performs a non-streaming chat completion.
	ChatCompletion(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error)

	// ChatCompletionStream performs a streaming chat completion.
	// Returns a Stream that yields chunks until closed.
	ChatCompletionStream(ctx context.Context, req *model.ChatCompletionRequest) (Stream, error)

	// Embeddings generates embeddings for the given input.
	Embeddings(ctx context.Context, req *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error)

	// SupportsEmbeddings returns true if this provider supports embeddings.
	SupportsEmbeddings() bool
}

// Stream represents a streaming response.
type Stream interface {
	// Recv receives the next chunk. Returns io.EOF when done.
	Recv() (*model.StreamChunk, error)

	// Close closes the stream.
	Close() error
}

// Deployment represents a specific model deployment.
type Deployment struct {
	ModelName    string   // User-facing model name
	Provider     Provider // The provider instance
	ProviderName string   // Provider name (e.g., "openai")
	ActualModel  string   // Actual model name sent to provider
	APIKey       string   // API key for this deployment
	APIBase      string   // Optional custom API base URL
	RPM          int      // Rate limit (requests per minute)
	TPM          int      // Rate limit (tokens per minute)
}

// DeploymentKey returns the stable string identity for this deployment.
// Format: "provider:model:api_base" — stable across config reloads.
// All downstream phases (BackoffManager, PoolBudgetTracker, sticky sessions) key on this string.
// When api_base is empty, the trailing colon is still present: "provider:model:".
// Note: usage_logs.deployment_key column exists in the schema but is populated in Phase 5/6,
// not in this phase. This method is the contract; wiring to the DB column is deferred.
func (d *Deployment) DeploymentKey() string {
	return d.ProviderName + ":" + d.ActualModel + ":" + d.APIBase
}

// RateLimitError is returned by providers when the upstream API responds with HTTP 429.
// RetryAfter is the duration parsed from the Retry-After response header, or 0 if absent.
// Callers (router, handler) check for this type with errors.As to apply backoff
// rather than treating the response as a hard failure.
type RateLimitError struct {
	Provider   string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("%s: rate limited, retry after %s", e.Provider, e.RetryAfter)
	}
	return fmt.Sprintf("%s: rate limited", e.Provider)
}

// ParseRetryAfter parses a Retry-After header value into a duration.
// It handles both integer seconds (most common) and HTTP-date format.
// Returns 0 if the header is absent or unparseable.
func ParseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	// Try integer seconds first (most common)
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	// Try HTTP-date format
	if t, err := http.ParseTime(header); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// streamAdapter wraps a channel-based stream.
type streamAdapter struct {
	chunks <-chan *model.StreamChunk
	errs   <-chan error
	closer func()
}

func NewStream(chunks <-chan *model.StreamChunk, errs <-chan error, closer func()) Stream {
	return &streamAdapter{
		chunks: chunks,
		errs:   errs,
		closer: closer,
	}
}

func (s *streamAdapter) Recv() (*model.StreamChunk, error) {
	select {
	case chunk, ok := <-s.chunks:
		if !ok {
			return nil, io.EOF
		}
		return chunk, nil
	case err := <-s.errs:
		if err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
}

func (s *streamAdapter) Close() error {
	if s.closer != nil {
		s.closer()
	}
	return nil
}
