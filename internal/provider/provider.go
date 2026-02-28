package provider

import (
	"context"
	"io"

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
