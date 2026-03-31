package openaicompat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// Callback types for provider customization.

// AuthFunc sets authentication headers on an outgoing HTTP request.
type AuthFunc func(req *http.Request)

// TransformResponseFunc mutates a chat completion response before returning it.
type TransformResponseFunc func(resp *model.ChatCompletionResponse) *model.ChatCompletionResponse

// TransformStreamChunkFunc mutates a streaming chunk before sending it.
type TransformStreamChunkFunc func(chunk *model.StreamChunk) *model.StreamChunk

// ParseErrorFunc parses a non-200/non-429 response body into a Go error.
// statusCode is the HTTP status; body is the raw response bytes.
type ParseErrorFunc func(statusCode int, body []byte) error

// BaseProvider handles the common HTTP request/response/streaming cycle
// for OpenAI-compatible APIs. Providers embed it and supply hooks.
type BaseProvider struct {
	ProviderName         string                   // Name for errors/logs (e.g., "openai")
	BaseURL              string                   // e.g., "https://api.openai.com/v1"
	Client               *http.Client             // HTTP client for requests
	Auth                 AuthFunc                 // Sets auth headers on each request
	TransformResponse    TransformResponseFunc    // Optional: mutate non-streaming response
	TransformStreamChunk TransformStreamChunkFunc // Optional: mutate each stream chunk
	ParseError           ParseErrorFunc           // Optional: custom error parsing
	DoneSentinel         string                   // SSE done marker; "" = EOF-only (no sentinel)
	ExtraHeaders         map[string]string        // Extra headers on every request
}

// Name returns the provider name.
func (p *BaseProvider) Name() string { return p.ProviderName }

// SupportsEmbeddings returns true — all OpenAI-compatible providers support embeddings.
func (p *BaseProvider) SupportsEmbeddings() bool { return true }

// ChatCompletion performs a non-streaming chat completion request.
func (p *BaseProvider) ChatCompletion(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.Auth != nil {
		p.Auth(httpReq)
	}
	for k, v := range p.ExtraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &provider.RateLimitError{
			Provider:   p.ProviderName,
			RetryAfter: provider.ParseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	if resp.StatusCode != http.StatusOK {
		if p.ParseError != nil {
			return nil, p.ParseError(resp.StatusCode, respBody)
		}
		// Default OpenAI-style error parsing
		var apiErr model.APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("%s error: %s", p.ProviderName, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("%s error (status %d): %s", p.ProviderName, resp.StatusCode, string(respBody))
	}

	var result model.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	if p.TransformResponse != nil {
		return p.TransformResponse(&result), nil
	}

	return &result, nil
}

// ChatCompletionStream performs a streaming chat completion request.
func (p *BaseProvider) ChatCompletionStream(ctx context.Context, req *model.ChatCompletionRequest) (provider.Stream, error) {
	// Copy request and ensure stream is enabled
	reqCopy := *req
	reqCopy.Stream = true

	body, err := json.Marshal(reqCopy)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.Auth != nil {
		p.Auth(httpReq)
	}
	for k, v := range p.ExtraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := provider.ParseRetryAfter(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		return nil, &provider.RateLimitError{
			Provider:   p.ProviderName,
			RetryAfter: retryAfter,
		}
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if p.ParseError != nil {
			return nil, p.ParseError(resp.StatusCode, respBody)
		}
		var apiErr model.APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("%s error: %s", p.ProviderName, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("%s error (status %d): %s", p.ProviderName, resp.StatusCode, string(respBody))
	}

	chunks := make(chan *model.StreamChunk)
	errs := make(chan error, 1)
	sentinel := p.DoneSentinel

	go func() {
		defer close(chunks)

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					errs <- err
				}
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")

			if sentinel != "" && data == sentinel {
				return
			}

			var chunk model.StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				errs <- fmt.Errorf("unmarshaling chunk: %w", err)
				return
			}

			if p.TransformStreamChunk != nil {
				chunkPtr := p.TransformStreamChunk(&chunk)
				select {
				case chunks <- chunkPtr:
				case <-ctx.Done():
					return
				}
			} else {
				select {
				case chunks <- &chunk:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return provider.NewStream(chunks, errs, func() { resp.Body.Close() }), nil
}

// Embeddings generates embeddings using an OpenAI-compatible endpoint.
func (p *BaseProvider) Embeddings(ctx context.Context, req *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.Auth != nil {
		p.Auth(httpReq)
	}
	for k, v := range p.ExtraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &provider.RateLimitError{
			Provider:   p.ProviderName,
			RetryAfter: provider.ParseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	if resp.StatusCode != http.StatusOK {
		if p.ParseError != nil {
			return nil, p.ParseError(resp.StatusCode, respBody)
		}
		var apiErr model.APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("%s error: %s", p.ProviderName, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("%s error (status %d): %s", p.ProviderName, resp.StatusCode, string(respBody))
	}

	var result model.EmbeddingsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return &result, nil
}
