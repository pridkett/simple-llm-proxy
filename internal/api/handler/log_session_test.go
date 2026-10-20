package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
)

func TestDecodeSessionRequest(t *testing.T) {
	for _, tc := range []struct {
		name, header, body, want string
		invalid                  bool
	}{
		{name: "header wins", header: "header", body: `{"session_id":"body","metadata":{"session_id":"meta"}}`, want: "header"},
		{name: "canonical wins", body: `{"session_id":"canonical","litellm_session_id":"legacy"}`, want: "canonical"},
		{name: "LiteLLM top level", body: `{"litellm_session_id":"play-1"}`, want: "play-1"},
		{name: "LiteLLM metadata", body: `{"metadata":{"litellm_session_id":"play-1"}}`, want: "play-1"},
		{name: "metadata canonical wins", body: `{"metadata":{"session_id":"canonical","litellm_session_id":"legacy"}}`, want: "canonical"},
		{name: "trim and skip blanks", header: " ", body: `{"session_id":" ","metadata":{"session_id":" play-1 "}}`, want: "play-1"},
		{name: "ungrouped", body: `{}`},
		{name: "null", body: `{"session_id":null}`},
		{name: "wrong type", body: `{"session_id":3}`, invalid: true},
		{name: "bad metadata value", body: `{"metadata":{"litellm_session_id":{}}}`, invalid: true},
		{name: "too long", header: strings.Repeat("x", 257), body: `{}`, invalid: true},
		{name: "control at edge", body: `{"session_id":"a\n"}`, invalid: true},
		{name: "control character", body: `{"session_id":"a\nb"}`, invalid: true},
		{name: "not an object", body: `[]`, invalid: true},
		{name: "null body", body: `null`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body))
			req.Header.Set("X-Session-ID", tc.header)
			var dst model.ChatCompletionRequest
			got, err := decodeSessionRequest(req, &dst)
			if (err != nil) != tc.invalid {
				t.Fatalf("error=%v, invalid=%v", err, tc.invalid)
			}
			if got != tc.want {
				t.Errorf("session=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestSessionFieldsConsumedWithoutLosingProviderFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"session_id":"play-1","litellm_session_id":"legacy","metadata":{"session_id":"meta","litellm_session_id":"legacy-meta","label":"keep"},"reasoning_effort":"high"}`))
	var dst model.ChatCompletionRequest
	if _, err := decodeSessionRequest(req, &dst); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(dst)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["session_id"]; ok {
		t.Fatal("session_id leaked upstream")
	}
	if _, ok := got["litellm_session_id"]; ok {
		t.Fatal("litellm_session_id leaked upstream")
	}
	meta := got["metadata"].(map[string]any)
	if len(meta) != 1 || meta["label"] != "keep" || got["reasoning_effort"] != "high" {
		t.Fatalf("provider fields changed: %s", data)
	}
}

func TestChatSessionLoggedWithoutBodyCapture(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "stream"}[stream], func(t *testing.T) {
			resetTestMockState()
			testMockProviderState.chatResponse = &model.ChatCompletionResponse{Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 3}}
			testMockProviderState.streamChunks = []*model.StreamChunk{{Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 3}}}
			rtr, err := router.New(newTestMockConfigWithBase("gpt-4", ""), nil)
			if err != nil {
				t.Fatal(err)
			}
			store := &captureStorage{}
			body := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"metadata":{"litellm_session_id":"play-1"},"stream":` + map[bool]string{false: "false", true: "true"}[stream] + `}`
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			w := httptest.NewRecorder()
			ChatCompletions(rtr, store, nil, nil, nil, config.GeneralSettings{})(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			entry := store.waitForLogs(t, 1)[0]
			if entry.SessionID != "play-1" || entry.ReqBodySnippet != nil || entry.IsStreaming != stream {
				t.Fatalf("incorrect attribution: %+v", entry)
			}
		})
	}
}

func TestInvalidSessionReturnsBadRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"session_id":123}`))
	w := httptest.NewRecorder()
	ChatCompletions(nil, nil, nil, nil, nil, config.GeneralSettings{})(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
}

func TestEmbeddingsSessionAttribution(t *testing.T) {
	resetTestMockState()
	testMockProviderState.supportsEmb = true
	testMockProviderState.embResponse = &model.EmbeddingsResponse{Usage: &model.Usage{PromptTokens: 5}}
	rtr, err := router.New(newTestMockConfigWithBase("gpt-4", ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &captureStorage{}
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"gpt-4","input":"hi"}`))
	req.Header.Set("X-Session-ID", "play-embedding")
	w := httptest.NewRecorder()
	Embeddings(rtr, store, nil, nil, nil)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	if got := store.waitForLogs(t, 1)[0].SessionID; got != "play-embedding" {
		t.Fatalf("session=%q", got)
	}
}

func TestResponsesSessionAttribution(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "stream"}[stream], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if _, ok := body["session_id"]; ok {
					t.Error("session_id leaked upstream")
				}
				if body["metadata"].(map[string]any)["label"] != "keep" {
					t.Error("lost provider metadata")
				}
				response := `{"id":"resp_session","status":"completed","usage":{"prompt_tokens":2,"completion_tokens":3}}`
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":%s}\n\n", response)
				} else {
					fmt.Fprint(w, response)
				}
			}))
			defer upstream.Close()
			rtr := responsesRouterForTest(t, upstream.URL)
			store := &captureStorage{}
			body := `{"model":"test-model","input":"hi","session_id":"play-response","metadata":{"label":"keep"},"stream":` + map[bool]string{false: "false", true: "true"}[stream] + `}`
			w := httptest.NewRecorder()
			Responses(rtr, store, nil, nil, nil, config.GeneralSettings{})(w, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body)))
			if w.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			if got := store.waitForLogs(t, 1)[0].SessionID; got != "play-response" {
				t.Fatalf("session=%q", got)
			}
		})
	}
}

func TestSessionLogAdminAPI(t *testing.T) {
	store, err := sqlite.New(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	sessionID := "story:/?a=1&b=%"
	for i, id := range []string{sessionID, sessionID, "unrelated"} {
		if err := store.LogRequest(ctx, &storage.RequestLog{RequestID: fmt.Sprintf("req-%d", i), SessionID: id, Model: "m", Provider: "p", Endpoint: "/v1/chat/completions", RequestTime: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	AdminLogs(store)(w, httptest.NewRequest(http.MethodGet, "/admin/logs?session_id="+url.QueryEscape(sessionID), nil))
	var result adminLogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || result.Total != 2 || len(result.Logs) != 2 || result.Logs[0].SessionID != sessionID {
		t.Fatalf("incorrect session list: %s", w.Body.String())
	}
}
