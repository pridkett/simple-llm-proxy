// Package ollama implements the Ollama provider as a thin wrapper around
// openaicompat.BaseProvider. Ollama is a local LLM runner that exposes an
// OpenAI-compatible API. Auth is optional: when no API key is provided,
// no Authorization header is sent (per D-12).
package ollama

import (
	"net/http"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/provider/openaicompat"
)

const defaultBaseURL = "http://localhost:11434/v1"

// New creates a new Ollama provider.
// When opts.APIKey is empty, no Authorization header is sent.
// When opts.APIKey is non-empty, Bearer auth is used.
func New(opts provider.ProviderOptions) provider.Provider {
	baseURL := defaultBaseURL
	if opts.APIBase != "" {
		baseURL = strings.TrimSuffix(opts.APIBase, "/")
	}

	// No-op auth when no key provided (D-12)
	var auth openaicompat.AuthFunc
	if opts.APIKey != "" {
		auth = func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+opts.APIKey)
		}
	}

	return &openaicompat.BaseProvider{
		ProviderName: "ollama",
		BaseURL:      baseURL,
		Client:       &http.Client{},
		Auth:         auth,
		ExtraHeaders: opts.ExtraHeaders,
		DoneSentinel: "[DONE]",
	}
}

func init() {
	provider.Register("ollama", func(opts provider.ProviderOptions) provider.Provider {
		return New(opts)
	})
}
