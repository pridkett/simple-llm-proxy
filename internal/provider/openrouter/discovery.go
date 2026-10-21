// Package openrouter implements the OpenRouter provider.
// This file adds model discovery: fetching all available models from the
// OpenRouter /models endpoint and expanding wildcard config entries.
package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
)

// modelListResponse is the OpenAI-compatible response from GET /models.
type modelListResponse struct {
	Data []modelEntry `json:"data"`
}

// modelEntry represents a single model in the OpenRouter model list.
type modelEntry struct {
	ID string `json:"id"`
}

// httpClient is an interface for HTTP requests, allowing test injection.
type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// defaultClient is the production HTTP client with a reasonable timeout.
var defaultClient httpClient = &http.Client{Timeout: 30 * time.Second}

// DiscoverModels fetches the full model list from OpenRouter and returns
// expanded ModelConfig entries, one per discovered model. Each entry inherits
// the API key, extra headers, RPM, and TPM from the wildcard template.
//
// apiBase should be the base URL (e.g., "https://openrouter.ai/api/v1").
// If empty, the default OpenRouter base URL is used.
func DiscoverModels(ctx context.Context, apiKey, apiBase string, template config.ModelConfig) ([]config.ModelConfig, error) {
	return discoverModelsWithClient(ctx, defaultClient, apiKey, apiBase, template)
}

// discoverModelsWithClient is the internal implementation that accepts an HTTP client
// for testing.
func discoverModelsWithClient(ctx context.Context, client httpClient, apiKey, apiBase string, template config.ModelConfig) ([]config.ModelConfig, error) {
	base := defaultBaseURL
	if apiBase != "" {
		base = strings.TrimSuffix(apiBase, "/")
	}

	url := base + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating model list request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching model list from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("model list request failed (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading model list response: %w", err)
	}

	var modelList modelListResponse
	if err := json.Unmarshal(body, &modelList); err != nil {
		return nil, fmt.Errorf("parsing model list response: %w", err)
	}

	configs := make([]config.ModelConfig, 0, len(modelList.Data))
	for _, m := range modelList.Data {
		if m.ID == "" {
			continue
		}

		mc := config.ModelConfig{
			ModelName: m.ID,
			LiteLLMParams: config.LiteLLMParams{
				Model:        "openrouter/" + m.ID,
				APIKey:       template.LiteLLMParams.APIKey,
				APIBase:      template.LiteLLMParams.APIBase,
				ExtraHeaders: template.LiteLLMParams.ExtraHeaders,
				ExtraParams:  template.LiteLLMParams.ExtraParams,
			},
			RPM: template.RPM,
			TPM: template.TPM,
		}
		configs = append(configs, mc)
	}

	return configs, nil
}

// IsWildcard returns true if the model config uses a wildcard pattern
// for OpenRouter model discovery (e.g., model: "openrouter/*").
func IsWildcard(mc config.ModelConfig) bool {
	model := mc.LiteLLMParams.Model
	return model == "openrouter/*"
}
