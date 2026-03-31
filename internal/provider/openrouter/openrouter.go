// Package openrouter implements the OpenRouter provider as a thin wrapper
// around openaicompat.BaseProvider. OpenRouter proxies requests to various
// LLM providers via a unified OpenAI-compatible API at openrouter.ai.
package openrouter

import (
	"net/http"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/provider/openaicompat"
)

const defaultBaseURL = "https://openrouter.ai/api/v1"

// New creates a new OpenRouter provider.
// OpenRouter uses Bearer auth and supports ExtraHeaders for HTTP-Referer and X-Title.
func New(opts provider.ProviderOptions) provider.Provider {
	baseURL := defaultBaseURL
	if opts.APIBase != "" {
		baseURL = strings.TrimSuffix(opts.APIBase, "/")
	}
	return &openaicompat.BaseProvider{
		ProviderName: "openrouter",
		BaseURL:      baseURL,
		Client:       &http.Client{},
		Auth: func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+opts.APIKey)
		},
		ExtraHeaders: opts.ExtraHeaders,
		DoneSentinel: "[DONE]",
	}
}

func init() {
	provider.Register("openrouter", func(opts provider.ProviderOptions) provider.Provider {
		return New(opts)
	})
}
