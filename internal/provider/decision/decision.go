// Package decision implements native Decisions and System One transports.
// Configuration selects the wire protocol independently of the provider prefix.
package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

const MaxBodyBytes = 32 << 20

type Provider struct {
	name   string
	opts   provider.ProviderOptions
	config *config.DecisionConfig
	url    string
	client *http.Client
}

func New(name string, opts provider.ProviderOptions, cfg *config.DecisionConfig) (*Provider, error) {
	d, err := config.NormalizeDecision(cfg)
	if err != nil || d == nil {
		return nil, fmt.Errorf("invalid decision configuration: %v", err)
	}
	base := strings.TrimRight(opts.APIBase, "/")
	if base == "" {
		switch name {
		case "openai":
			base = "https://api.openai.com/v1"
		case "typesafe":
			base = "https://api.typesafe.ai/v1"
		default:
			return nil, fmt.Errorf("api_base is required for decision provider %q", name)
		}
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("decision api_base must be an HTTP(S) URL without credentials, query, or fragment")
	}
	path := d.Path
	if path == "" {
		if d.Protocol == "cloudflare" {
			return nil, fmt.Errorf("cloudflare decision.path must identify the Workers AI model, e.g. /@cf/cloudflare/clef")
		}
		path = "/" + d.Protocol
	}
	// Do not follow redirects with deployment credentials to another origin.
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Provider{name: name, opts: opts, config: d, url: base + path, client: client}, nil
}

func (p *Provider) Name() string             { return p.name }
func (p *Provider) SupportsEmbeddings() bool { return false }
func (p *Provider) ChatCompletion(context.Context, *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return nil, unsupported()
}
func (p *Provider) ChatCompletionStream(context.Context, *model.ChatCompletionRequest) (provider.Stream, error) {
	return nil, unsupported()
}
func (p *Provider) Embeddings(context.Context, *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, unsupported()
}
func unsupported() error {
	return provider.NewHTTPError("decision", 400, nil, nil, fmt.Errorf("decision-only model: use /v1/decisions or /v1/systemone"))
}

// Prepare checks capabilities and translation before routing consumes quota.
func Prepare(req *model.DecisionRequest, cfg *config.DecisionConfig, actualModel string) (map[string]json.RawMessage, error) {
	for _, modality := range req.Modalities {
		if !slices.Contains(cfg.Modalities, modality) {
			return nil, fmt.Errorf("model does not support %s decision input", modality)
		}
	}
	for _, q := range req.Questions {
		if !slices.Contains(cfg.QuestionTypes, q.Type) {
			return nil, fmt.Errorf("model does not support %s questions", q.Type)
		}
	}
	format := cfg.Protocol
	if format == "cloudflare" {
		format = model.SystemOneFormat
	}
	style := "assets"
	if cfg.Protocol == "cloudflare" {
		style = "images"
	}
	wire, err := req.Wire(format, actualModel, style)
	if err != nil {
		return nil, err
	}
	if cfg.Protocol == "cloudflare" {
		// Workers AI accepts embedded media only. Never fetch user URLs in the proxy.
		for _, field := range []string{"images", "audio", "videos"} {
			if raw := wire[field]; raw != nil {
				var refs []string
				if json.Unmarshal(raw, &refs) != nil {
					return nil, fmt.Errorf("%s must be an array", field)
				}
				for _, ref := range refs {
					if !strings.HasPrefix(ref, "data:") {
						return nil, fmt.Errorf("Cloudflare %s must be embedded data URLs", field)
					}
				}
			}
		}
		if wire["assets"] != nil {
			return nil, fmt.Errorf("Cloudflare does not support TypeSafe assets; use images, audio, or videos")
		}
	}
	return wire, nil
}

func (p *Provider) Decide(ctx context.Context, req *model.DecisionRequest) (*model.DecisionResponse, error) {
	wire, err := Prepare(req, p.config, req.Model)
	if err != nil {
		return nil, provider.NewHTTPError(p.name, 400, nil, nil, err)
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	if len(body) > MaxBodyBytes {
		return nil, provider.NewHTTPError(p.name, 400, nil, nil, fmt.Errorf("decision request exceeds %d bytes", MaxBodyBytes))
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.opts.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.opts.APIKey)
	}
	for key, value := range p.opts.ExtraHeaders {
		httpReq.Header.Set(key, value)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("decision request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading decision response: %w", err)
	}
	if len(data) > MaxBodyBytes {
		return nil, fmt.Errorf("decision response exceeds size limit")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, provider.NewHTTPError(p.name, resp.StatusCode, resp.Header, data)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return nil, fmt.Errorf("invalid decision response object")
	}
	format := p.config.Protocol
	if format == "cloudflare" {
		var envelope struct {
			Success *bool                      `json:"success"`
			Result  map[string]json.RawMessage `json:"result"`
		}
		if json.Unmarshal(data, &envelope) != nil || envelope.Success == nil || !*envelope.Success || envelope.Result == nil {
			return nil, fmt.Errorf("Cloudflare returned an unsuccessful or invalid decision envelope")
		}
		fields = envelope.Result
		format = model.SystemOneFormat
	}
	return req.ResponseFromWire(fields, format)
}
