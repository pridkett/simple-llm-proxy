package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"golang.org/x/oauth2"
)

var errOAuthProvider = errors.New("upstream OAuth provider request failed")

type oauthProvider struct {
	Issuer           string   `json:"issuer"`
	AuthURL          string   `json:"authorization_endpoint"`
	TokenURL         string   `json:"token_endpoint"`
	AuthMethods      []string `json:"token_endpoint_auth_methods_supported"`
	PKCEMethods      []string `json:"code_challenge_methods_supported"`
	IssuerInResponse bool     `json:"authorization_response_iss_parameter_supported"`
}

type oauthCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenURL     string    `json:"token_url"`
}

func (g *gateway) discoverOAuth(ctx context.Context, settings *config.MCPOAuth) (oauthProvider, []string, error) {
	var resource struct {
		Resource string   `json:"resource"`
		Issuers  []string `json:"authorization_servers"`
		Scopes   []string `json:"scopes_supported"`
	}
	var provider oauthProvider
	if err := g.fetchOAuthJSON(ctx, settings.ResourceMetadataURL, &resource); err != nil ||
		resource.Resource != settings.Resource || !slices.Contains(resource.Issuers, settings.Issuer) {
		return provider, nil, errOAuthProvider
	}
	issuer, _ := url.Parse(settings.Issuer)
	origin := issuer.Scheme + "://" + issuer.Host
	issuerPath := strings.TrimSuffix(issuer.EscapedPath(), "/")
	metadataURLs := []string{
		origin + "/.well-known/oauth-authorization-server" + issuerPath,
		origin + "/.well-known/openid-configuration" + issuerPath,
		origin + issuerPath + "/.well-known/openid-configuration",
	}
	for _, endpoint := range metadataURLs {
		provider = oauthProvider{}
		err := g.fetchOAuthJSON(ctx, endpoint, &provider)
		if errors.Is(err, errOAuthMetadataNotFound) {
			continue
		}
		if err != nil || provider.Issuer != settings.Issuer ||
			!oauthEndpointOnIssuer(provider.AuthURL, issuer) ||
			!oauthEndpointOnIssuer(provider.TokenURL, issuer) ||
			!slices.Contains(provider.PKCEMethods, "S256") {
			return provider, nil, errOAuthProvider
		}
		method := "none"
		if settings.ClientSecret != "" {
			method = "client_secret_basic"
		}
		if len(provider.AuthMethods) > 0 && !slices.Contains(provider.AuthMethods, method) {
			return provider, nil, errOAuthProvider
		}
		scopes := settings.Scopes
		if len(scopes) == 0 {
			scopes = resource.Scopes
		}
		return provider, scopes, nil
	}
	return provider, nil, errOAuthProvider
}

func oauthEndpointOnIssuer(raw string, issuer *url.URL) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, issuer.Host) &&
		u.User == nil && u.Fragment == "" && u.Opaque == "" && u.RawQuery == "" && !u.ForceQuery
}

var errOAuthMetadataNotFound = errors.New("OAuth metadata not found")

func (g *gateway) fetchOAuthJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errOAuthProvider
	}
	req.Header.Set("Accept", "application/json")
	response, err := g.transport.RoundTrip(req)
	if err != nil {
		return errOAuthProvider
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return errOAuthMetadataNotFound
	}
	if response.StatusCode != http.StatusOK || decodeOAuthJSON(response.Body, target) != nil {
		return errOAuthProvider
	}
	return nil
}

func decodeOAuthJSON(body io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(body, 128<<10))
	if err := decoder.Decode(target); err != nil || decoder.Decode(new(any)) != io.EOF {
		return errOAuthProvider
	}
	return nil
}

func oauthAuthorizationURL(settings *config.MCPOAuth, provider oauthProvider, scopes []string, state, verifier string) string {
	client := oauth2.Config{
		ClientID: settings.ClientID, RedirectURL: settings.RedirectURL,
		Endpoint: oauth2.Endpoint{AuthURL: provider.AuthURL},
		Scopes:   scopes,
	}
	return client.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("resource", settings.Resource))
}

func (g *gateway) requestOAuthToken(ctx context.Context, settings *config.MCPOAuth, endpoint string, form url.Values) (oauthCredential, error) {
	var grant oauthCredential
	issuer, _ := url.Parse(settings.Issuer)
	if !oauthEndpointOnIssuer(endpoint, issuer) {
		return grant, errOAuthProvider
	}
	form.Set("resource", settings.Resource)
	if settings.ClientSecret == "" {
		form.Set("client_id", settings.ClientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return grant, errOAuthProvider
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if settings.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(settings.ClientID), url.QueryEscape(settings.ClientSecret))
	}
	response, err := g.transport.RoundTrip(req)
	if err != nil {
		return grant, errOAuthProvider
	}
	defer response.Body.Close()
	var result struct {
		ErrorCode    string      `json:"error"`
		AccessToken  string      `json:"access_token"`
		RefreshToken string      `json:"refresh_token"`
		TokenType    string      `json:"token_type"`
		ExpiresIn    json.Number `json:"expires_in"`
	}
	if err := decodeOAuthJSON(response.Body, &result); err != nil {
		return grant, errOAuthProvider
	}
	if response.StatusCode == http.StatusBadRequest && result.ErrorCode == "invalid_grant" {
		return grant, errOAuthConnect
	}
	if response.StatusCode != http.StatusOK || !strings.EqualFold(result.TokenType, "bearer") || !config.ValidMCPKey(result.AccessToken) {
		return grant, errOAuthProvider
	}
	grant = oauthCredential{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, TokenURL: endpoint}
	if result.ExpiresIn != "" {
		seconds, err := result.ExpiresIn.Int64()
		if err != nil || seconds <= 0 || seconds > int64((1<<63-1)/time.Second) {
			return oauthCredential{}, errOAuthProvider
		}
		grant.ExpiresAt = time.Now().Add(time.Duration(seconds) * time.Second)
	}
	return grant, nil
}
