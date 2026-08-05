package openai

import (
	"net/http"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/provider/openaicompat"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
)

// Provider is the OpenAI provider. It embeds openaicompat.BaseProvider for all
// chat-completions/embeddings HTTP, streaming, and error-handling logic, and adds
// Responses API support directly (provider.ResponsesProvider) since the Responses
// API is OpenAI-specific — no other provider in this repo shares its schema, so
// it is not part of the shared openaicompat base (see ADR 010 D-01).
type Provider struct {
	*openaicompat.BaseProvider
}

// New creates a new OpenAI provider.
func New(opts provider.ProviderOptions) provider.Provider {
	return newProvider(opts)
}

func newProvider(opts provider.ProviderOptions) *Provider {
	baseURL := defaultBaseURL
	if opts.APIBase != "" {
		baseURL = strings.TrimSuffix(opts.APIBase, "/")
	}

	apiKey := opts.APIKey
	return &Provider{
		BaseProvider: &openaicompat.BaseProvider{
			ProviderName: "openai",
			BaseURL:      baseURL,
			Client:       &http.Client{},
			Auth: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			},
			ExtraHeaders: opts.ExtraHeaders,
			DoneSentinel: "[DONE]",
		},
	}
}

func init() {
	provider.Register("openai", func(opts provider.ProviderOptions) provider.Provider {
		return New(opts)
	})
}
