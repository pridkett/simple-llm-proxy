// Package vllm implements the vLLM provider as a thin wrapper around
// openaicompat.BaseProvider. vLLM is a high-throughput self-hosted LLM
// inference engine that exposes an OpenAI-compatible API. There is no
// default base URL — the user must provide api_base in their config.
package vllm

import (
	"net/http"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/provider/openaicompat"
)

// New creates a new vLLM provider.
// vLLM requires an explicit APIBase (no default URL). When APIKey is empty,
// no Authorization header is sent.
func New(opts provider.ProviderOptions) provider.Provider {
	baseURL := strings.TrimSuffix(opts.APIBase, "/")
	return &openaicompat.BaseProvider{
		ProviderName: "vllm",
		BaseURL:      baseURL,
		Client:       &http.Client{},
		Auth: func(req *http.Request) {
			if opts.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+opts.APIKey)
			}
		},
		ExtraHeaders: opts.ExtraHeaders,
		DoneSentinel: "[DONE]",
	}
}

func init() {
	provider.Register("vllm", func(opts provider.ProviderOptions) provider.Provider {
		return New(opts)
	})
}
