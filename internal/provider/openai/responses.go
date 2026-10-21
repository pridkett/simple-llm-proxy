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

// doJSON executes a JSON request against the Responses API and unmarshals a
// successful response into out. Mirrors the error-handling conventions of
// openaicompat.BaseProvider (429 -> RateLimitError, non-200 -> parsed APIError).
func (p *Provider) doJSON(ctx context.Context, method, path string, reqBody any, out any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		body, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, p.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	if reqBody != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if p.Auth != nil {
		p.Auth(httpReq)
	}
	for k, v := range p.ExtraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return &provider.RateLimitError{
			Provider:   p.ProviderName,
			RetryAfter: provider.ParseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr model.APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			return fmt.Errorf("%s error: %s", p.ProviderName, apiErr.Error.Message)
		}
		return fmt.Errorf("%s error (status %d): %s", p.ProviderName, resp.StatusCode, string(respBody))
	}

	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("unmarshaling response: %w", err)
		}
	}
	return nil
}

// CreateResponse implements provider.ResponsesProvider.
func (p *Provider) CreateResponse(ctx context.Context, req *model.ResponsesRequest) (*model.ResponsesResponse, error) {
	reqCopy := *req
	reqCopy.Stream = false

	var result model.ResponsesResponse
	if err := p.doJSON(ctx, http.MethodPost, "/responses", &reqCopy, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetResponse implements provider.ResponsesProvider.
func (p *Provider) GetResponse(ctx context.Context, responseID string) (*model.ResponsesResponse, error) {
	var result model.ResponsesResponse
	if err := p.doJSON(ctx, http.MethodGet, "/responses/"+responseID, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CancelResponse implements provider.ResponsesProvider.
func (p *Provider) CancelResponse(ctx context.Context, responseID string) (*model.ResponsesResponse, error) {
	var result model.ResponsesResponse
	if err := p.doJSON(ctx, http.MethodPost, "/responses/"+responseID+"/cancel", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateResponseStream implements provider.ResponsesProvider.
func (p *Provider) CreateResponseStream(ctx context.Context, req *model.ResponsesRequest) (provider.ResponsesStream, error) {
	reqCopy := *req
	reqCopy.Stream = true

	body, err := json.Marshal(reqCopy)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/responses", bytes.NewReader(body))
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
		var apiErr model.APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("%s error: %s", p.ProviderName, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("%s error (status %d): %s", p.ProviderName, resp.StatusCode, string(respBody))
	}

	events := make(chan *model.ResponsesStreamEvent)
	errs := make(chan error, 1)

	go func() {
		defer close(events)

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
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")

			var event model.ResponsesStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				errs <- fmt.Errorf("unmarshaling event: %w", err)
				return
			}

			select {
			case events <- &event:
			case <-ctx.Done():
				return
			}

			if event.Type == "response.completed" || event.Type == "response.failed" ||
				event.Type == "response.cancelled" || event.Type == "response.incomplete" {
				return
			}
		}
	}()

	return provider.NewResponsesStream(events, errs, func() { resp.Body.Close() }), nil
}
