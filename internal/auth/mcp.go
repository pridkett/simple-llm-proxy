package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

const (
	MCPManagementPath = "/api/v1/mcp"
	MCPMetadataPath   = "/.well-known/oauth-protected-resource" + MCPManagementPath
	MCPReadScope      = "proxy:read"
	MCPWriteScope     = "proxy:keys:write"
)

var accessTokenAlgorithms = []jose.SignatureAlgorithm{
	jose.RS256, jose.RS384, jose.RS512, jose.PS256, jose.PS384, jose.PS512,
	jose.ES256, jose.ES384, jose.ES512, jose.EdDSA,
}

type MCPOAuth struct {
	Origin   string
	issuer   string
	verifier *oidc.IDTokenVerifier
}

// NewMCPOAuth sets up access-token verification separately from browser login.
func NewMCPOAuth(ctx context.Context, provider *oidc.Provider, issuer, callback string) (*MCPOAuth, error) {
	redirect, ok := httpsEndpoint(callback)
	if !ok {
		return nil, fmt.Errorf("MCP OAuth requires an HTTPS callback URL")
	}
	var discovery struct {
		JWKS string `json:"jwks_uri"`
	}
	if err := provider.Claims(&discovery); err != nil {
		return nil, fmt.Errorf("invalid OIDC discovery metadata for MCP OAuth")
	}
	if _, ok := httpsEndpoint(discovery.JWKS); !ok {
		return nil, fmt.Errorf("MCP OAuth requires an HTTPS OIDC signing-key URL")
	}
	client := http.Client{Timeout: 5 * time.Second}
	if configured, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && configured != nil {
		client.Transport = configured.Transport
	}
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if _, ok := httpsEndpoint(r.URL.String()); !ok || len(via) >= 10 {
			return fmt.Errorf("invalid OIDC signing-key redirect")
		}
		return nil
	}
	algorithms := make([]string, 0, len(accessTokenAlgorithms))
	for _, algorithm := range accessTokenAlgorithms {
		algorithms = append(algorithms, string(algorithm))
	}
	return &MCPOAuth{
		Origin: "https://" + redirect.Host,
		issuer: issuer,
		verifier: provider.VerifierContext(oidc.ClientContext(ctx, &client), &oidc.Config{
			SkipClientIDCheck:    true, // Validate the full resource audience below.
			SupportedSigningAlgs: algorithms,
		}),
	}, nil
}

func httpsEndpoint(value string) (*url.URL, bool) {
	u, err := url.Parse(value)
	return u, err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

type MCPResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

func (a *MCPOAuth) Metadata() MCPResourceMetadata {
	return MCPResourceMetadata{
		Resource:               a.Origin + MCPManagementPath,
		AuthorizationServers:   []string{a.issuer},
		ScopesSupported:        []string{MCPReadScope, MCPWriteScope},
		BearerMethodsSupported: []string{"header"},
	}
}

// Scopes verifies an RFC 9068 access token for this MCP resource.
func (a *MCPOAuth) Scopes(ctx context.Context, raw string) ([]string, bool) {
	if len(raw) > 16<<10 {
		return nil, false
	}
	jws, err := jose.ParseSignedCompact(raw, accessTokenAlgorithms)
	if err != nil || len(jws.Signatures) != 1 {
		return nil, false
	}
	typ, _ := jws.Signatures[0].Protected.ExtraHeaders["typ"].(string)
	if !strings.EqualFold(typ, "at+jwt") && !strings.EqualFold(typ, "application/at+jwt") {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	token, err := a.verifier.Verify(ctx, raw)
	if err != nil || token.Issuer != a.issuer || token.Subject == "" || !token.Expiry.After(time.Now()) ||
		!slices.Contains(token.Audience, a.Origin+MCPManagementPath) ||
		!(len(token.Audience) == 1 || len(token.Audience) == 2 && slices.Contains(token.Audience, a.issuer)) {
		return nil, false
	}
	var claims struct {
		Scope string `json:"scope"`
	}
	if err := token.Claims(&claims); err != nil {
		return nil, false
	}
	return strings.Fields(claims.Scope), true
}
