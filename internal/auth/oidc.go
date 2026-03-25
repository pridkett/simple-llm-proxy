package auth

import (
	"context"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCProvider wraps go-oidc provider and verifier for use by handlers.
type OIDCProvider struct {
	Provider     *oidc.Provider
	Verifier     *oidc.IDTokenVerifier
	OAuth2Config *oauth2.Config
	AdminGroup   string
}

// NewOIDCProvider initializes the OIDC provider from the issuer URL.
// Returns nil (not an error) if IssuerURL is empty — OIDC is optional.
func NewOIDCProvider(ctx context.Context, issuerURL, clientID, clientSecret, redirectURL, adminGroup string) (*OIDCProvider, error) {
	if issuerURL == "" {
		return nil, nil
	}
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, err
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})
	oauth2Config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile", "groups"},
	}
	return &OIDCProvider{
		Provider:     provider,
		Verifier:     verifier,
		OAuth2Config: oauth2Config,
		AdminGroup:   adminGroup,
	}, nil
}
