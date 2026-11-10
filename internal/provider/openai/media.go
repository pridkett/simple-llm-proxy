package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func (p *Provider) mediaHeaders(dst, src http.Header) {
	for _, name := range []string{"Accept", "OpenAI-Safety-Identifier"} {
		if value := src.Get(name); value != "" {
			dst.Set(name, value)
		}
	}
	for name, value := range p.ExtraHeaders {
		dst.Set(name, value)
	}
}

func (p *Provider) ForwardMedia(ctx context.Context, path, contentType string, body io.Reader, headers http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	p.Auth(req)
	p.mediaHeaders(req.Header, headers)
	// An upstream redirect must not replay a media upload or its credentials.
	client := *p.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := provider.Do(&client, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, p.mediaError(ctx, resp)
	}
	return resp, nil
}

func (p *Provider) ConnectRealtime(ctx context.Context, path string, query url.Values, headers http.Header) (*websocket.Conn, error) {
	target, err := url.Parse(p.BaseURL + path)
	if err != nil {
		return nil, err
	}
	switch target.Scheme {
	case "http":
		target.Scheme = "ws"
	case "https":
		target.Scheme = "wss"
	case "ws", "wss":
	default:
		return nil, fmt.Errorf("unsupported Realtime upstream scheme")
	}
	target.RawQuery = query.Encode()
	req := &http.Request{Header: make(http.Header)}
	p.Auth(req)
	p.mediaHeaders(req.Header, headers)
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, target.String(), req.Header)
	if err != nil && resp != nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		return nil, p.mediaError(ctx, resp)
	}
	return conn, err
}

func (p *Provider) mediaError(ctx context.Context, resp *http.Response) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return fmt.Errorf("unexpected upstream redirect")
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	accounting.ObserveJSON(ctx, body)
	return provider.NewHTTPError(p.ProviderName, resp.StatusCode, resp.Header, body)
}
