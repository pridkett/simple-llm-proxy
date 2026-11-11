package elevenlabs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

type audioProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func init() {
	provider.Register("elevenlabs", func(opts provider.ProviderOptions) provider.Provider {
		baseURL := strings.TrimRight(opts.APIBase, "/")
		if baseURL == "" {
			baseURL = "https://api.elevenlabs.io/v1"
		}
		return &audioProvider{
			baseURL: baseURL,
			apiKey:  opts.APIKey,
			client: &http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			},
		}
	})
}

func (*audioProvider) Name() string { return "elevenlabs" }

var errAudioOnly = errors.New("ElevenLabs deployment supports audio generation only")

func (*audioProvider) ChatCompletion(context.Context, *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return nil, errAudioOnly
}
func (*audioProvider) ChatCompletionStream(context.Context, *model.ChatCompletionRequest) (provider.Stream, error) {
	return nil, errAudioOnly
}
func (*audioProvider) Embeddings(context.Context, *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, errAudioOnly
}
func (*audioProvider) SupportsEmbeddings() bool { return false }

func (p *audioProvider) GenerateAudio(ctx context.Context, model, outputFormat string, body io.Reader) (*http.Response, error) {
	path := config.ElevenLabsAudioPath(model)
	if path == "" {
		return nil, errors.New("unsupported ElevenLabs audio model")
	}
	target := p.baseURL + path
	if outputFormat != "" {
		target += "?" + url.Values{"output_format": {outputFormat}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
	if err != nil {
		return nil, errors.New("invalid ElevenLabs upstream URL")
	}
	req.Header.Set("xi-api-key", p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, errors.New("ElevenLabs request failed")
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		switch {
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			return nil, errors.New("unexpected ElevenLabs redirect")
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, errors.New("ElevenLabs rejected the upstream credential or its permissions")
		default:
			return nil, provider.NewHTTPError(p.Name(), resp.StatusCode, resp.Header, nil,
				fmt.Errorf("ElevenLabs generation returned HTTP %d", resp.StatusCode))
		}
	}
	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(contentType, "audio/") && contentType != "application/octet-stream" {
		resp.Body.Close()
		return nil, errors.New("ElevenLabs returned an unexpected audio content type")
	}
	return resp, nil
}
