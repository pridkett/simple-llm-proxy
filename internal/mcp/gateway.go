// Package mcp exposes registered MCP servers through an authenticated HTTP relay.
package mcp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

const tokenPrefix = "sk-mcp-"

type registration struct {
	config.MCPServer
	binding string
}

type gateway struct {
	servers     map[string]registration
	keys        storage.Storage
	connections storage.MCPStore
	limiter     *keystore.RateLimiter
	credentials cipher.AEAD
	sessions    cipher.AEAD
	transport   http.RoundTripper
}

func Register(mux chi.Router, cfg *config.Config, store storage.Storage, sm *scs.SessionManager, limiter *keystore.RateLimiter) {
	// Expose an empty list in the dashboard when the registry is disabled.
	if store == nil {
		return
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.MaxResponseHeaderBytes = 64 << 10
	g := newGateway(cfg, store, limiter, transport)
	g.register(mux, sm)
}

func (g *gateway) register(mux chi.Router, sm *scs.SessionManager) {
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		mux.MethodFunc(method, "/mcp/{server}", g.relay)
	}
	mux.Route("/api/mcp/connections", func(r chi.Router) {
		r.Use(sm.LoadAndSave)
		r.Use(middleware.RequireSession(g.keys, sm))
		r.Use(http.NewCrossOriginProtection().Handler)
		r.Get("/", g.list)
		r.Get("/{server}", g.connection)
		r.Put("/{server}", g.saveKey)
		r.Delete("/{server}", g.disconnect)
		r.Post("/{server}/token", g.mintToken)
		r.Delete("/{server}/token", g.revokeToken)
	})
}

func newGateway(cfg *config.Config, store storage.Storage, limiter *keystore.RateLimiter, transport http.RoundTripper) *gateway {
	connections, _ := store.(storage.MCPStore)
	var sessionKey [32]byte
	rand.Read(sessionKey[:])
	g := &gateway{
		servers: make(map[string]registration, len(cfg.MCP.Servers)),
		keys:    store, connections: connections, limiter: limiter,
		credentials: newAEAD(cfg.MCP.CredentialKey), sessions: newAEAD(sessionKey),
		transport: transport,
	}
	for name, server := range cfg.MCP.Servers {
		// Reusing an alias for another resource, header, or identity namespace
		// must not reuse the previous registration's personal credentials.
		binding := digest(strings.Join([]string{name, server.URL, server.Scope, server.Header, cfg.OIDCSettings.IssuerURL}, "\x00"))
		g.servers[name] = registration{server, binding}
	}
	return g
}

func newAEAD(key [32]byte) cipher.AEAD {
	block, _ := aes.NewCipher(key[:]) // AES-256 uses a fixed 32-byte key.
	aead, _ := cipher.NewGCMWithRandomNonce(block)
	return aead
}

func digest(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

type admission struct {
	actor      string
	credential string
	generation string
}

func (g *gateway) authorize(w http.ResponseWriter, r *http.Request, server registration) (admission, bool) {
	var caller admission
	token, ok := bearer(r)
	if !ok {
		unauthorized(w)
		return caller, false
	}
	if strings.HasPrefix(token, tokenPrefix) && g.connections != nil {
		hash := digest(token)
		connection, err := g.connections.GetMCPConnectionByToken(r.Context(), server.binding, hash, time.Now().Unix())
		if err != nil {
			fail(w, 500, "storage_error", "Could not check MCP access.")
			return caller, false
		}
		if connection == nil {
			unauthorized(w)
			return caller, false
		}
		if !slices.Contains(server.AllowedUserIDs, connection.UserID) {
			fail(w, 403, "access_denied", "This user is not granted access to the server.")
			return caller, false
		}
		caller.actor = "user:" + connection.UserID
		caller.generation = hash
		if server.Scope == "user" {
			if len(connection.Ciphertext) == 0 {
				fail(w, 403, "connection_required", "Connect your upstream account in MCP Connections.")
				return caller, false
			}
			key, err := g.credentials.Open(nil, nil, connection.Ciphertext, credentialBinding(server, connection.UserID))
			if err != nil {
				fail(w, 403, "connection_required", "Replace your upstream key in MCP Connections.")
				return caller, false
			}
			caller.credential = string(key)
			caller.generation = digest(hash + string(connection.Ciphertext))
		}
	} else {
		if !strings.HasPrefix(token, "sk-app-") {
			unauthorized(w)
			return caller, false
		}
		key, err := g.keys.GetAPIKeyByHash(r.Context(), digest(token))
		if err != nil {
			fail(w, 500, "storage_error", "Could not check MCP access.")
			return caller, false
		}
		if key == nil || !key.IsActive {
			unauthorized(w)
			return caller, false
		}
		if server.Scope == "user" || !slices.Contains(server.AllowedAppKeyIDs, key.ID) {
			fail(w, 403, "access_denied", "This application key is not granted access to the server.")
			return caller, false
		}
		if (key.MaxRPM != nil && !g.limiter.CheckAndIncrementRPM(key.ID, *key.MaxRPM)) ||
			(key.MaxRPD != nil && !g.limiter.CheckAndIncrementRPD(key.ID, *key.MaxRPD)) {
			fail(w, 429, "rate_limit_exceeded", "Application key request limit exceeded.")
			return caller, false
		}
		caller.actor = "key:" + strconv.FormatInt(key.ID, 10)
		caller.generation = digest(token)
	}
	if server.Scope == "shared" {
		caller.credential = server.SharedKey
		caller.generation = digest(caller.generation + server.SharedKey)
	}
	return caller, true
}

func credentialBinding(server registration, userID string) []byte {
	return []byte(server.binding + "\x00" + userID)
}

func bearer(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	var encoded string
	var bytes int
	switch {
	case strings.HasPrefix(token, "sk-app-"):
		encoded, bytes = strings.TrimPrefix(token, "sk-app-"), 24
	case strings.HasPrefix(token, tokenPrefix):
		encoded, bytes = strings.TrimPrefix(token, tokenPrefix), 32
	default:
		return "", false
	}
	if len(encoded) != bytes*2 {
		return "", false
	}
	_, err := hex.DecodeString(encoded)
	return token, err == nil
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
	fail(w, 401, "invalid_gateway_token", "A valid MCP gateway or application bearer token is required.")
}

func fail(w http.ResponseWriter, status int, code, message string) {
	body := map[string]string{"code": code, "message": message}
	if code == "connection_required" {
		body["setup_url"] = "/#/mcp"
	}
	writeJSON(w, status, map[string]any{"error": body})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
