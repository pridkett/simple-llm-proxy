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

// Provider is the OpenAI provider. openaicompat.ResponsesProvider supplies the
// chat-completions/embeddings logic from BaseProvider plus the Responses API
// (provider.ResponsesProvider, ADR 010 D-01).
type Provider struct {
	*openaicompat.ResponsesProvider
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
	return &Provider{ResponsesProvider: &openaicompat.ResponsesProvider{
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
	}}
}

func init() {
	provider.Register("openai", func(opts provider.ProviderOptions) provider.Provider {
		return New(opts)
	})
}
