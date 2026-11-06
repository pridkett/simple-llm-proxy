package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

const personalTokenLifetime = 30 * 24 * time.Hour

type connectionStatus struct {
	Server       string `json:"server"`
	Endpoint     string `json:"endpoint"`
	Scope        string `json:"credential_scope"`
	KeySet       bool   `json:"key_set"`
	TokenExpires int64  `json:"token_expires,omitempty"`
	OAuth        bool   `json:"oauth"`
	Connected    bool   `json:"connected"`
	CanUse       bool   `json:"can_use"`
	CanManage    bool   `json:"can_manage_oauth"`
}

func (g *gateway) list(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	names := make([]string, 0, len(g.servers))
	for name, server := range g.servers {
		if slices.Contains(server.AllowedUserIDs, user.ID) || canManageOAuth(server, user) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	statuses := make([]connectionStatus, 0, len(names))
	for _, name := range names {
		status, ok := g.status(w, r, name, g.servers[name], user)
		if !ok {
			return
		}
		statuses = append(statuses, status)
	}
	writeJSON(w, 200, statuses)
}

func (g *gateway) connection(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "server")
	server, exists := g.servers[name]
	user := middleware.UserFromContext(r.Context())
	if !exists || (!slices.Contains(server.AllowedUserIDs, user.ID) && !canManageOAuth(server, user)) {
		fail(w, 404, "unknown_server", "MCP server is not available.")
		return
	}
	if status, ok := g.status(w, r, name, server, user); ok {
		writeJSON(w, 200, status)
	}
}

func canManageOAuth(server registration, user *storage.User) bool {
	return server.OAuth != nil &&
		((server.Scope == "shared" && user.IsAdmin) ||
			(server.Scope == "user" && slices.Contains(server.AllowedUserIDs, user.ID)))
}

func (g *gateway) status(w http.ResponseWriter, r *http.Request, name string, server registration, user *storage.User) (connectionStatus, bool) {
	status := connectionStatus{
		Server: name, Endpoint: "/mcp/" + name, Scope: server.Scope,
		OAuth: server.OAuth != nil, CanManage: canManageOAuth(server, user),
		CanUse: slices.Contains(server.AllowedUserIDs, user.ID),
	}
	if status.OAuth {
		if g.oauthStore == nil {
			fail(w, 503, "storage_unavailable", "MCP connection storage is unavailable.")
			return status, false
		}
		grant, err := g.oauthStore.GetMCPOAuth(r.Context(), server.binding, oauthOwner(server, user.ID))
		if err != nil {
			fail(w, 500, "storage_error", "Could not read the MCP OAuth connection.")
			return status, false
		}
		status.Connected = grant != nil
	}
	if g.connections == nil {
		fail(w, 503, "storage_unavailable", "MCP connection storage is unavailable.")
		return status, false
	}
	connection, err := g.connections.GetMCPConnection(r.Context(), server.binding, user.ID)
	if err != nil {
		fail(w, 500, "storage_error", "Could not read the MCP connection.")
		return status, false
	}
	if connection != nil {
		status.KeySet = len(connection.Ciphertext) > 0
		status.TokenExpires = connection.TokenExpires
	}
	return status, true
}

func (g *gateway) saveKey(w http.ResponseWriter, r *http.Request) {
	_, server, userID, ok := g.userRegistration(w, r)
	if !ok {
		return
	}
	if server.Scope != "user" || server.OAuth != nil {
		fail(w, 403, "access_denied", "This server does not use personal upstream keys.")
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		fail(w, 415, "invalid_content_type", "An application/json body is required.")
		return
	}
	var input struct {
		Key string `json:"key"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || !config.ValidMCPKey(input.Key) || decoder.Decode(new(any)) != io.EOF {
		fail(w, 400, "invalid_key", "Supply a valid upstream API key.")
		return
	}
	sealed := g.credentials.Seal(nil, nil, []byte(input.Key), credentialBinding(server, userID))
	if err := g.connections.SetMCPKey(r.Context(), server.binding, userID, sealed); err != nil {
		fail(w, 500, "storage_error", "Could not save the MCP key.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *gateway) mintToken(w http.ResponseWriter, r *http.Request) {
	_, server, userID, ok := g.userRegistration(w, r)
	if !ok {
		return
	}
	var secret [32]byte
	rand.Read(secret[:])
	token := tokenPrefix + hex.EncodeToString(secret[:])
	expires := time.Now().Add(personalTokenLifetime).Unix()
	var created bool
	var err error
	if server.Scope == "user" && server.OAuth != nil {
		if g.oauthStore == nil {
			fail(w, 503, "storage_unavailable", "MCP connection storage is unavailable.")
			return
		}
		g.oauth.mu.Lock()
		created, err = g.oauthStore.SetMCPOAuthToken(r.Context(), server.binding, userID, digest(token), expires)
		g.oauth.mu.Unlock()
	} else {
		created, err = g.connections.SetMCPToken(r.Context(), server.binding, userID, digest(token), expires, server.Scope == "user")
	}
	if err != nil {
		fail(w, 500, "storage_error", "Could not issue the MCP token.")
		return
	}
	if !created {
		fail(w, 409, "connection_required", "Connect your upstream account before issuing a gateway token.")
		return
	}
	writeJSON(w, 201, map[string]any{"token": token, "expires": expires})
}

func (g *gateway) revokeToken(w http.ResponseWriter, r *http.Request) {
	_, server, userID, ok := g.userRegistration(w, r)
	if ok {
		g.finishMutation(w, g.connections.RevokeMCPToken(r.Context(), server.binding, userID))
	}
}

func (g *gateway) disconnect(w http.ResponseWriter, r *http.Request) {
	_, server, userID, ok := g.userRegistration(w, r)
	if ok {
		if server.Scope == "user" && server.OAuth != nil {
			g.disconnectOAuth(w, r)
			return
		}
		g.finishMutation(w, g.connections.DeleteMCPConnection(r.Context(), server.binding, userID))
	}
}

func (g *gateway) finishMutation(w http.ResponseWriter, err error) {
	if err != nil {
		fail(w, 500, "storage_error", "Could not update the MCP connection.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *gateway) userRegistration(w http.ResponseWriter, r *http.Request) (string, registration, string, bool) {
	name := chi.URLParam(r, "server")
	server, exists := g.servers[name]
	userID := middleware.UserFromContext(r.Context()).ID
	if !exists || !slices.Contains(server.AllowedUserIDs, userID) {
		fail(w, 404, "unknown_server", "MCP server is not available.")
		return "", registration{}, "", false
	}
	if g.connections == nil {
		fail(w, 503, "storage_unavailable", "MCP connection storage is unavailable.")
		return "", registration{}, "", false
	}
	return name, server, userID, true
}
