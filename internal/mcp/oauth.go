package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/auth"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"golang.org/x/oauth2"
)

var errOAuthConnect = errors.New("MCP OAuth connection required")

type oauthState struct {
	mu      sync.Mutex
	pending map[string]pendingOAuth
	refresh [64]sync.Mutex
}

type pendingOAuth struct {
	stateHash string
	userID    string
	session   string
	verifier  string
	expires   time.Time
	provider  oauthProvider
	claimed   bool
}

func (g *gateway) registerOAuth(r chi.Router, sm *scs.SessionManager) {
	r.Use(sm.LoadAndSave)
	r.Use(middleware.RequireSession(g.keys, sm))
	r.Use(http.NewCrossOriginProtection().Handler)
	r.Post("/{server}", g.startOAuth(sm))
	r.Delete("/{server}", g.disconnectOAuth)
	r.Get("/callback/{server}", g.completeOAuth(sm))
}

func (g *gateway) oauthRegistration(w http.ResponseWriter, r *http.Request) (registration, string, bool) {
	server, exists := g.servers[chi.URLParam(r, "server")]
	user := middleware.UserFromContext(r.Context())
	if exists && server.OAuth != nil &&
		canManageOAuth(server, user) {
		if g.oauthStore != nil {
			return server, oauthOwner(server, user.ID), true
		}
		fail(w, 503, "storage_unavailable", "MCP connection storage is unavailable.")
		return registration{}, "", false
	}
	fail(w, 404, "unknown_server", "MCP OAuth connection is not available.")
	return registration{}, "", false
}

func oauthOwner(server registration, userID string) string {
	if server.Scope == "shared" {
		return ""
	}
	return userID
}

func oauthBinding(server registration, owner string) string {
	return server.binding + "\x00" + owner
}

func (g *gateway) startOAuth(sm *scs.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		server, owner, ok := g.oauthRegistration(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		provider, scopes, err := g.discoverOAuth(ctx, server.OAuth)
		if err != nil {
			fail(w, 502, "oauth_discovery_failed", "Could not verify the upstream OAuth provider.")
			return
		}
		state, err := auth.RandString(32)
		if err != nil {
			fail(w, 500, "oauth_setup_failed", "Could not start OAuth.")
			return
		}
		pending := pendingOAuth{
			stateHash: digest(state),
			userID:    middleware.UserFromContext(ctx).ID,
			session:   digest(sm.Token(ctx)),
			verifier:  oauth2.GenerateVerifier(),
			expires:   time.Now().Add(10 * time.Minute),
			provider:  provider,
		}
		binding := oauthBinding(server, owner)
		g.oauth.mu.Lock()
		for key, entry := range g.oauth.pending {
			if time.Now().After(entry.expires) {
				delete(g.oauth.pending, key)
			}
		}
		if _, exists := g.oauth.pending[binding]; !exists && len(g.oauth.pending) >= 1024 {
			g.oauth.mu.Unlock()
			fail(w, 503, "oauth_busy", "Too many pending connections. Try again shortly.")
			return
		}
		g.oauth.pending[binding] = pending
		g.oauth.mu.Unlock()
		writeJSON(w, 200, map[string]string{
			"authorization_url": oauthAuthorizationURL(server.OAuth, provider, scopes, state, pending.verifier),
		})
	}
}

func (g *gateway) completeOAuth(sm *scs.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		server, owner, ok := g.oauthRegistration(w, r)
		if !ok {
			return
		}
		binding := oauthBinding(server, owner)
		query := r.URL.Query()
		stateHash := digest(query.Get("state"))
		g.oauth.mu.Lock()
		pending, exists := g.oauth.pending[binding]
		if !exists || pending.claimed || pending.stateHash != stateHash ||
			pending.userID != middleware.UserFromContext(r.Context()).ID ||
			pending.session != digest(sm.Token(r.Context())) || time.Now().After(pending.expires) {
			g.oauth.mu.Unlock()
			fail(w, 400, "invalid_oauth_state", "This OAuth attempt has expired or belongs to another session.")
			return
		}
		pending.claimed = true
		g.oauth.pending[binding] = pending
		g.oauth.mu.Unlock()

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		outcome := "failed"
		issuer := query.Get("iss")
		issuerOK := issuer == server.OAuth.Issuer || (!query.Has("iss") && !pending.provider.IssuerInResponse)
		if code := query.Get("code"); code != "" && issuerOK && !query.Has("error") &&
			len(query["state"]) == 1 && len(query["code"]) == 1 && len(query["iss"]) <= 1 {
			grant, err := g.requestOAuthToken(ctx, server.OAuth, pending.provider.TokenURL, url.Values{
				"grant_type": {"authorization_code"}, "code": {code},
				"redirect_uri": {server.OAuth.RedirectURL}, "code_verifier": {pending.verifier},
			})
			if err == nil {
				g.oauth.mu.Lock()
				// A disconnect or a newer attempt can supersede this exchange.
				current, active := g.oauth.pending[binding]
				if active && current.stateHash == stateHash && time.Now().Before(current.expires) {
					credential := storage.MCPOAuthCredential{Ciphertext: g.sealOAuth(grant, binding), Generation: stateHash}
					if g.oauthStore.PutMCPOAuth(ctx, server.binding, owner, credential) == nil {
						outcome = "connected"
					}
				}
				g.oauth.mu.Unlock()
			}
		}
		g.oauth.mu.Lock()
		if current := g.oauth.pending[binding]; current.stateHash == stateHash {
			delete(g.oauth.pending, binding)
		}
		g.oauth.mu.Unlock()
		http.Redirect(w, r, "/#/mcp?oauth="+outcome, http.StatusSeeOther)
	}
}

func (g *gateway) disconnectOAuth(w http.ResponseWriter, r *http.Request) {
	server, owner, ok := g.oauthRegistration(w, r)
	if !ok {
		return
	}
	g.oauth.mu.Lock()
	defer g.oauth.mu.Unlock()
	delete(g.oauth.pending, oauthBinding(server, owner))
	g.finishMutation(w, g.oauthStore.DeleteMCPOAuth(r.Context(), server.binding, owner))
}

func (g *gateway) sealOAuth(grant oauthCredential, binding string) []byte {
	data, _ := json.Marshal(grant) // fixed JSON types and validated expiry
	return g.credentials.Seal(nil, nil, data, []byte(binding))
}

func (g *gateway) oauthAccessToken(ctx context.Context, server registration, owner string) (string, string, error) {
	binding := oauthBinding(server, owner)
	// Re-read the grant under a bounded set of locks to serialize refresh-token
	// rotation. A compare-and-swap also protects concurrent reconnect/disconnect.
	hash := sha256.Sum256([]byte(binding))
	lock := &g.oauth.refresh[int(hash[0])%len(g.oauth.refresh)]
	lock.Lock()
	defer lock.Unlock()
	record, err := g.oauthStore.GetMCPOAuth(ctx, server.binding, owner)
	if err != nil {
		return "", "", err
	}
	if record == nil {
		return "", "", errOAuthConnect
	}
	var grant oauthCredential
	plain, err := g.credentials.Open(nil, nil, record.Ciphertext, []byte(binding))
	if err != nil || json.Unmarshal(plain, &grant) != nil {
		return "", "", errOAuthConnect
	}
	if grant.ExpiresAt.IsZero() || grant.ExpiresAt.After(time.Now().Add(10*time.Second)) {
		return grant.AccessToken, record.Generation, nil
	}
	if grant.RefreshToken == "" {
		return "", "", errOAuthConnect
	}
	refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	updated, err := g.requestOAuthToken(refreshCtx, server.OAuth, grant.TokenURL, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {grant.RefreshToken},
	})
	if err != nil {
		return "", "", err
	}
	if updated.RefreshToken == "" {
		updated.RefreshToken = grant.RefreshToken
	}
	saved, err := g.oauthStore.RefreshMCPOAuth(ctx, server.binding, owner, record.Ciphertext, g.sealOAuth(updated, binding))
	if err != nil {
		return "", "", err
	}
	if !saved {
		return "", "", errOAuthConnect
	}
	return updated.AccessToken, record.Generation, nil
}
