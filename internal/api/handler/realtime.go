package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

const realtimeWriteTimeout = 15 * time.Second

// Realtime relays a model-scoped WebSocket session using the deployment key.
func (h OpenAIMedia) Realtime(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !websocket.IsWebSocketUpgrade(req) {
			model.WriteError(w, model.ErrBadRequest("WebSocket upgrade required"))
			return
		}
		if origin := req.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(parsed.Host, req.Host) {
				model.WriteError(w, model.ErrForbidden("WebSocket origin must match the proxy host"))
				return
			}
		}
		query, err := url.ParseQuery(req.URL.RawQuery)
		if err != nil || len(query["model"]) != 1 || strings.TrimSpace(query.Get("model")) == "" {
			model.WriteError(w, model.ErrBadRequest("exactly one model query parameter is required"))
			return
		}
		alias := query.Get("model")
		delete(query, "model")
		transcribe := path == "/realtime" && len(query) == 1 && len(query["intent"]) == 1 && query.Get("intent") == "transcription"
		if !transcribe && len(query) > 0 {
			model.WriteError(w, model.ErrBadRequest("unsupported Realtime query parameter"))
			return
		}

		var upstream *websocket.Conn
		h.route(w, req, alias, req.Header.Get("X-Session-ID"), true, func(ctx context.Context, p provider.MediaProvider, actual string) error {
			if !transcribe {
				query.Set("model", actual)
			}
			var err error
			upstream, err = p.ConnectRealtime(ctx, path, query, req.Header)
			return err
		}, func(ctx context.Context, actual string) (int, bool, error) {
			defer upstream.Close()
			// The upgrader writes the handshake directly to the socket.
			// Pass the proxy's accounting and routing headers explicitly.
			upgrader := websocket.Upgrader{HandshakeTimeout: realtimeWriteTimeout}
			client, err := upgrader.Upgrade(w, req, w.Header())
			if err != nil {
				return 0, true, err
			}
			defer client.Close()
			if transcribe {
				config, _ := json.Marshal(actual)
				initial := fmt.Sprintf(`{"type":"session.update","session":{"type":"transcription","audio":{"input":{"format":{"type":"audio/pcm","rate":24000},"transcription":{"model":%s},"turn_detection":null}}}}`, config)
				if err := writeRealtime(upstream, websocket.TextMessage, []byte(initial)); err != nil {
					return http.StatusSwitchingProtocols, true, err
				}
			}
			return http.StatusSwitchingProtocols, true, relayRealtime(ctx, client, upstream, alias, actual, transcribe)
		})
	}
}

type realtimeRelayResult struct {
	destination *websocket.Conn
	err         error
}

func relayRealtime(ctx context.Context, client, upstream *websocket.Conn, alias, actual string, transcribe bool) error {
	ctx, cancel := context.WithTimeout(ctx, 65*time.Minute)
	defer cancel()
	for _, conn := range []*websocket.Conn{client, upstream} {
		conn.SetReadLimit(32 << 20)
	}
	done := make(chan realtimeRelayResult, 2)
	go func() {
		err := pumpRealtime(upstream, client, func(data []byte) ([]byte, error) {
			return realtimeClientEvent(data, alias, actual, transcribe)
		})
		done <- realtimeRelayResult{destination: upstream, err: err}
	}()
	go func() {
		var usage realtimeUsage
		err := pumpRealtime(client, upstream, func(data []byte) ([]byte, error) {
			usage.observe(data)
			return data, nil
		})
		if usage.seen {
			accounting.ObserveUsage(ctx, &usage.tokens)
		}
		done <- realtimeRelayResult{destination: client, err: err}
	}()

	select {
	case result := <-done:
		code := websocket.CloseGoingAway
		var closeErr *websocket.CloseError
		if errors.As(result.err, &closeErr) && closeErr.Code != websocket.CloseAbnormalClosure && closeErr.Code != websocket.CloseNoStatusReceived {
			code = closeErr.Code
		}
		closeRealtime(result.destination, code)
		// A rejected client event should also send a policy close to the client.
		if errors.Is(result.err, errRealtimeEvent) {
			closeRealtime(client, websocket.ClosePolicyViolation)
		}
		_ = client.Close()
		_ = upstream.Close()
		<-done
		if closeErr != nil && (closeErr.Code == websocket.CloseNormalClosure || closeErr.Code == websocket.CloseGoingAway ||
			closeErr.Code == websocket.CloseNoStatusReceived) {
			return nil
		}
		return result.err
	case <-ctx.Done():
		closeRealtime(client, websocket.CloseGoingAway)
		closeRealtime(upstream, websocket.CloseGoingAway)
		_ = client.Close()
		_ = upstream.Close()
		<-done
		<-done
		return ctx.Err()
	}
}

func closeRealtime(conn *websocket.Conn, code int) {
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, ""), time.Now().Add(time.Second))
}

func pumpRealtime(dst, src *websocket.Conn, transform func([]byte) ([]byte, error)) error {
	for {
		kind, data, err := src.ReadMessage()
		if err != nil {
			return err
		}
		if kind == websocket.TextMessage {
			data, err = transform(data)
			if err != nil {
				return err
			}
		} else {
			return errRealtimeEvent
		}
		if err := writeRealtime(dst, kind, data); err != nil {
			return err
		}
	}
}

func writeRealtime(conn *websocket.Conn, kind int, data []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(realtimeWriteTimeout)); err != nil {
		return err
	}
	return conn.WriteMessage(kind, data)
}

var errRealtimeEvent = errors.New("invalid Realtime event or attempt to change the routed model")

func realtimeClientEvent(data []byte, alias, actual string, transcribe bool) ([]byte, error) {
	var event map[string]json.RawMessage
	var eventType string
	if json.Unmarshal(data, &event) != nil || json.Unmarshal(event["type"], &eventType) != nil || eventType == "" {
		return nil, errRealtimeEvent
	}
	// Reject case variants and emit one type field, so the proxy and the
	// upstream agree on the event type used to enforce model pinning.
	for field := range event {
		if field != "type" && strings.EqualFold(field, "type") {
			return nil, errRealtimeEvent
		}
	}
	if eventType != "session.update" {
		return json.Marshal(event)
	}
	session, err := mediaSession(event["session"])
	if err != nil {
		return nil, errRealtimeEvent
	}
	encodedModel, _ := json.Marshal(actual)
	if raw, exists := session["model"]; exists {
		requested, err := mediaModel(raw)
		if err != nil || (requested != alias && requested != actual) {
			return nil, errRealtimeEvent
		}
		session["model"] = encodedModel
	}
	if transcribe {
		if err := pinRealtimeTranscription(session, encodedModel); err != nil {
			return nil, err
		}
	}
	event["session"], _ = json.Marshal(session)
	return json.Marshal(event)
}

func pinRealtimeTranscription(session map[string]json.RawMessage, actual json.RawMessage) error {
	if raw, exists := session["type"]; exists && string(raw) != `"transcription"` {
		return errRealtimeEvent
	}
	session["type"] = json.RawMessage(`"transcription"`)
	// Most session updates are partial. Preserve their shape unless they update
	// transcription settings, in which case pin those settings to the route.
	parent := session
	var parents []map[string]json.RawMessage
	fields := []string{"audio", "input", "transcription"}
	for _, field := range fields {
		raw, exists := parent[field]
		if !exists {
			return nil
		}
		var child map[string]json.RawMessage
		if json.Unmarshal(raw, &child) != nil || child == nil {
			return errRealtimeEvent
		}
		parents = append(parents, parent)
		parent = child
	}
	parent["model"] = actual
	for i := len(fields) - 1; i >= 0; i-- {
		parents[i][fields[i]], _ = json.Marshal(parent)
		parent = parents[i]
	}
	return nil
}

type realtimeUsage struct {
	tokens model.Usage
	seen   bool
}

func (u *realtimeUsage) observe(data []byte) {
	var event struct {
		Type     string       `json:"type"`
		Usage    *model.Usage `json:"usage"`
		Response struct {
			Usage *model.Usage `json:"usage"`
		} `json:"response"`
	}
	if json.Unmarshal(data, &event) != nil {
		return
	}
	var turn *model.Usage
	switch event.Type {
	case "response.done":
		turn = event.Response.Usage
	case "conversation.item.input_audio_transcription.completed":
		turn = event.Usage
	}
	if turn == nil || turn.TotalTokens <= 0 || turn.TotalTokens > math.MaxInt-u.tokens.TotalTokens ||
		turn.PromptTokens < 0 || turn.PromptTokens > math.MaxInt-u.tokens.PromptTokens ||
		turn.CompletionTokens < 0 || turn.CompletionTokens > math.MaxInt-u.tokens.CompletionTokens {
		return
	}
	u.tokens.PromptTokens += turn.PromptTokens
	u.tokens.CompletionTokens += turn.CompletionTokens
	u.tokens.TotalTokens += turn.TotalTokens
	u.seen = true
}
