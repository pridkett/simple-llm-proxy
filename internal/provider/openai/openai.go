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

// New creates a new OpenAI provider. The returned provider embeds
// openaicompat.BaseProvider — all HTTP, streaming, and embeddings logic
// is handled by the shared base.
func New(opts provider.ProviderOptions) provider.Provider {
	baseURL := defaultBaseURL
	if opts.APIBase != "" {
		baseURL = strings.TrimSuffix(opts.APIBase, "/")
	}

	apiKey := opts.APIKey
	return &openaicompat.BaseProvider{
		ProviderName: "openai",
		BaseURL:      baseURL,
		Client:       &http.Client{},
		Auth: func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		},
		ExtraHeaders: opts.ExtraHeaders,
		DoneSentinel: "[DONE]",
	}
}

func init() {
	provider.Register("openai", func(opts provider.ProviderOptions) provider.Provider {
		return New(opts)
	})
}
