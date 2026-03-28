package openai

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

const (
	defaultBaseURL = "https://api.openai.com/v1"
)

// Provider implements the OpenAI provider.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// New creates a new OpenAI provider.
func New(apiKey, apiBase string) provider.Provider {
	baseURL := defaultBaseURL
	if apiBase != "" {
		baseURL = strings.TrimSuffix(apiBase, "/")
	}
	return &Provider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

func (p *Provider) Name() string {
	return "openai"
}

func (p *Provider) SupportsEmbeddings() bool {
	return true
}

func (p *Provider) ChatCompletion(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
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
			Provider:   p.Name(),
			RetryAfter: provider.ParseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode != http.StatusOK {
		var apiErr model.APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil {
			return nil, fmt.Errorf("openai error: %s", apiErr.Error.Message)
		}
		return nil, fmt.Errorf("openai error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result model.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return &result, nil
}

func (p *Provider) ChatCompletionStream(ctx context.Context, req *model.ChatCompletionRequest) (provider.Stream, error) {
	// Ensure stream is set
	reqCopy := *req
	reqCopy.Stream = true

	body, err := json.Marshal(reqCopy)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := provider.ParseRetryAfter(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		return nil, &provider.RateLimitError{
			Provider:   p.Name(),
			RetryAfter: retryAfter,
		}
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		var apiErr model.APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil {
			return nil, fmt.Errorf("openai error: %s", apiErr.Error.Message)
		}
		return nil, fmt.Errorf("openai error (status %d): %s", resp.StatusCode, string(respBody))
	}

	chunks := make(chan *model.StreamChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer resp.Body.Close()

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
			if data == "[DONE]" {
				return
			}

			var chunk model.StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				errs <- fmt.Errorf("unmarshaling chunk: %w", err)
				return
			}

			select {
			case chunks <- &chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return provider.NewStream(chunks, errs, func() { resp.Body.Close() }), nil
}

func (p *Provider) Embeddings(ctx context.Context, req *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr model.APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil {
			return nil, fmt.Errorf("openai error: %s", apiErr.Error.Message)
		}
		return nil, fmt.Errorf("openai error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result model.EmbeddingsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return &result, nil
}

func init() {
	provider.Register("openai", New)
}
