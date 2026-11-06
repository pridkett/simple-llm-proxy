package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

const sessionHeader = "Mcp-Session-Id"

func (g *gateway) relay(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "server")
	server, ok := g.servers[name]
	if !ok {
		fail(w, 404, "unknown_server", "MCP server is not available.")
		return
	}
	// This endpoint is for native clients and server-to-server integrations.
	// The separate connection API handles browser requests with session auth.
	if len(r.Header.Values("Origin")) > 0 {
		fail(w, 403, "invalid_origin", "Browser origins are disabled for the MCP transport.")
		return
	}
	caller, ok := g.authorize(w, r, server)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
	defer cancel()
	upstream, ok := g.upstreamRequest(w, r.WithContext(ctx), server, caller)
	if !ok {
		return
	}
	log.Ctx(ctx).Debug().Str("mcp_server", name).Str("mcp_actor", caller.actor).Msg("relaying MCP request")
	response, err := g.transport.RoundTrip(upstream)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fail(w, 504, "upstream_timeout", "The upstream MCP server timed out.")
		} else {
			fail(w, 502, "upstream_error", "Could not reach the upstream MCP server.")
		}
		return
	}
	defer response.Body.Close()
	g.relayResponse(w, response, server, caller)
}

func (g *gateway) upstreamRequest(w http.ResponseWriter, r *http.Request, server registration, caller admission) (*http.Request, bool) {
	var body io.Reader
	if r.Method == http.MethodPost {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				fail(w, 413, "request_too_large", "MCP requests must be at most 2 MiB.")
			} else {
				fail(w, 400, "invalid_request", "Could not read the MCP request.")
			}
			return nil, false
		}
		if len(data) == 0 {
			fail(w, 400, "invalid_request", "An MCP request body is required.")
			return nil, false
		}
		body = bytes.NewReader(data)
	}
	upstream, err := http.NewRequestWithContext(r.Context(), r.Method, server.URL, body)
	if err != nil {
		fail(w, 502, "upstream_error", "The upstream MCP URL is invalid.")
		return nil, false
	}
	upstream.GetBody = nil
	for header, values := range r.Header {
		switch header {
		case "Accept", "Content-Type", "Mcp-Protocol-Version", "Mcp-Method", "Mcp-Name", "Last-Event-Id":
			upstream.Header[header] = values
		default:
			if strings.HasPrefix(header, "Mcp-Param-") {
				upstream.Header[header] = values
			}
		}
	}
	if handles := r.Header.Values(sessionHeader); len(handles) != 0 {
		if len(handles) != 1 {
			fail(w, 400, "invalid_request", "Supply one MCP session identifier.")
			return nil, false
		}
		id, ok := g.openSession(handles[0], sessionBinding(server, caller))
		if !ok {
			fail(w, 404, "unknown_session", "The MCP session expired or is unavailable.")
			return nil, false
		}
		upstream.Header.Set(sessionHeader, id)
	}
	if caller.credential != "" {
		value := caller.credential
		if server.Header == "Authorization" {
			value = "Bearer " + value
		}
		upstream.Header.Set(server.Header, value)
	}
	return upstream, true
}

func (g *gateway) relayResponse(w http.ResponseWriter, response *http.Response, server registration, caller admission) {
	status := response.StatusCode
	switch {
	case status == http.StatusUnauthorized:
		if server.Scope == "user" {
			fail(w, 403, "connection_required", "The upstream rejected your key. Replace it in MCP Connections.")
		} else {
			fail(w, 502, "upstream_auth_failed", "The upstream MCP server rejected the configured credential.")
		}
		return
	case status >= 300 && status < 400:
		fail(w, 502, "upstream_redirect", "The upstream MCP server returned a redirect.")
		return
	}

	contentType := response.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType != "application/json" && mediaType != "text/event-stream" {
		if status == http.StatusAccepted || status == http.StatusNoContent {
			w.WriteHeader(status)
		} else if status >= 400 {
			copyResponseHeaders(w, response, "Retry-After", "Allow")
			fail(w, status, "upstream_error", "The upstream MCP server returned an error.")
		} else {
			fail(w, 502, "invalid_upstream_response", "Expected an MCP JSON or event stream response.")
		}
		return
	}

	if ids := response.Header.Values(sessionHeader); len(ids) > 0 {
		if len(ids) != 1 || !validUpstreamSession(ids[0]) {
			fail(w, 502, "invalid_upstream_response", "The upstream returned an invalid MCP session identifier.")
			return
		}
		w.Header().Set(sessionHeader, g.sealSession(ids[0], sessionBinding(server, caller)))
	}
	copyResponseHeaders(w, response, "Retry-After", "Allow", "Mcp-Protocol-Version")
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	if mediaType == "text/event-stream" {
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(status)
		writer := streamWriter{w}
		if writer.Flush() != nil {
			return
		}
		if _, err := io.Copy(writer, response.Body); err != nil {
			panic(http.ErrAbortHandler)
		}
		return
	}
	w.WriteHeader(status)
	if _, err := io.Copy(w, response.Body); err != nil {
		panic(http.ErrAbortHandler)
	}
}

func copyResponseHeaders(w http.ResponseWriter, response *http.Response, headers ...string) {
	for _, header := range headers {
		for _, value := range response.Header.Values(header) {
			w.Header().Add(header, value)
		}
	}
}

type streamWriter struct{ http.ResponseWriter }

func (w streamWriter) Flush() error {
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w streamWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if err == nil {
		err = w.Flush()
	}
	return n, err
}

func sessionBinding(server registration, caller admission) []byte {
	return []byte(server.binding + "\x00" + caller.actor + "\x00" + caller.generation)
}

func validUpstreamSession(id string) bool {
	if len(id) == 0 || len(id) > 512 {
		return false
	}
	for _, c := range id {
		if c < '!' || c > '~' {
			return false
		}
	}
	return true
}

// Seal the upstream ID instead of exposing it or keeping a map in memory.
func (g *gateway) sealSession(id string, binding []byte) string {
	data := binary.BigEndian.AppendUint64(nil, uint64(time.Now().Add(time.Hour).Unix()))
	data = append(data, id...)
	return base64.RawURLEncoding.EncodeToString(g.sessions.Seal(nil, nil, data, binding))
}

func (g *gateway) openSession(handle string, binding []byte) (string, bool) {
	if len(handle) > base64.RawURLEncoding.EncodedLen(8+512+g.sessions.Overhead()) {
		return "", false
	}
	encoded, err := base64.RawURLEncoding.DecodeString(handle)
	if err != nil {
		return "", false
	}
	data, err := g.sessions.Open(nil, nil, encoded, binding)
	if err != nil || len(data) <= 8 || binary.BigEndian.Uint64(data[:8]) <= uint64(time.Now().Unix()) {
		return "", false
	}
	return string(data[8:]), true
}
