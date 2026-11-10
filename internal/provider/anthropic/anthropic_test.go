package anthropic

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestAnthropicExtraHeaders(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream bool
	}{
		{name: "non-streaming"},
		{name: "streaming", stream: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for name, want := range map[string]string{
					"User-Agent":        "example-client/1.0",
					"x-api-key":         "test-key",
					"anthropic-version": anthropicVersion,
					"Content-Type":      "application/json",
				} {
					if got := r.Header.Get(name); got != want {
						t.Errorf("%s = %q, want %q", name, got, want)
					}
				}
				if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
					return
				}
				io.WriteString(w, `{}`)
			}))
			defer server.Close()

			p := New(provider.ProviderOptions{
				APIKey:  "test-key",
				APIBase: server.URL,
				ExtraHeaders: map[string]string{
					"user-agent":        "example-client/1.0",
					"X-Api-Key":         "override-key",
					"Anthropic-Version": "override-version",
					"content-type":      "text/plain",
				},
			})
			req := &model.ChatCompletionRequest{
				Model:    "test",
				Messages: []model.Message{{Role: "user", Content: "Hello"}},
			}
			if !tc.stream {
				if _, err := p.ChatCompletion(t.Context(), req); err != nil {
					t.Fatal(err)
				}
				return
			}
			stream, err := p.ChatCompletionStream(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if _, err := stream.Recv(); err != io.EOF {
				t.Errorf("expected io.EOF, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Phase 13 Plan 03 — Anthropic cache token translation tests (GREEN)
//
// These tests verify that Anthropic's cache token fields are correctly
// translated from anthropicUsage into model.Usage for both streaming and
// non-streaming responses. The t.Skip stubs from Plan 01 have been removed.
// ---------------------------------------------------------------------------

// TestAnthropicCacheTokensNonStreaming (INSTR-04): verifies that Anthropic
// cache token counts are translated from anthropicUsage into model.Usage for
// non-streaming responses.
func TestAnthropicCacheTokensNonStreaming(t *testing.T) {
	p := &Provider{apiKey: "test", baseURL: "http://localhost", client: &http.Client{}}

	resp := &anthropicResponse{
		ID:   "msg-test-001",
		Type: "message",
		Role: "assistant",
		Content: []anthropicContent{
			{Type: "text", Text: "Hello from Anthropic"},
		},
		Model:      "claude-3-5-sonnet-20241022",
		StopReason: "end_turn",
		Usage: anthropicUsage{
			InputTokens:              10,
			OutputTokens:             5,
			CacheReadInputTokens:     50,
			CacheCreationInputTokens: 25,
		},
	}

	result := p.translateResponse(resp, "claude-3-5-sonnet-20241022")
	if result == nil {
		t.Fatal("translateResponse returned nil")
	}
	if result.Usage == nil {
		t.Fatal("result.Usage is nil")
	}

	// INSTR-04: cache tokens must flow through the translation layer.
	if result.Usage.CacheReadTokens != 50 {
		t.Errorf("CacheReadTokens: got %d, want 50", result.Usage.CacheReadTokens)
	}
	if result.Usage.CacheWriteTokens != 25 {
		t.Errorf("CacheWriteTokens: got %d, want 25", result.Usage.CacheWriteTokens)
	}
	// Standard token fields must still be correct.
	if result.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens: got %d, want 10", result.Usage.PromptTokens)
	}
	if result.Usage.CompletionTokens != 5 {
		t.Errorf("CompletionTokens: got %d, want 5", result.Usage.CompletionTokens)
	}
}

// TestAnthropicCacheTokensStreaming (INSTR-04): verifies that Anthropic cache
// token counts are translated from streaming events into model.Usage for the
// final StreamChunk.
//
// Anthropic streams cache tokens in the message_start event's Message.Usage.
// translateStreamEvent captures them and includes them in the StreamChunk.Usage
// on the message_delta event.
func TestAnthropicCacheTokensStreaming(t *testing.T) {
	p := &Provider{apiKey: "test", baseURL: "http://localhost", client: &http.Client{}}
	responseID := "msg-stream-001"
	requestModel := "claude-3-5-sonnet-20241022"

	// message_start event carries input + cache tokens in Message.Usage.
	msgStartEvent := &anthropicStreamEvent{
		Type: "message_start",
		Message: &anthropicResponse{
			ID:   responseID,
			Type: "message",
			Role: "assistant",
			Usage: anthropicUsage{
				InputTokens:              10,
				OutputTokens:             0,
				CacheReadInputTokens:     50,
				CacheCreationInputTokens: 25,
			},
		},
	}

	// message_delta event carries output tokens.
	msgDeltaEvent := &anthropicStreamEvent{
		Type:  "message_delta",
		Delta: &anthropicDelta{StopReason: "end_turn"},
		Usage: &anthropicUsage{OutputTokens: 15},
	}

	var inputTokens int
	var cacheReadTokens, cacheWriteTokens int

	// Process message_start — sets inputTokens and cache tokens.
	startChunk := p.translateStreamEvent(msgStartEvent, responseID, requestModel, &inputTokens, &cacheReadTokens, &cacheWriteTokens)
	if startChunk == nil {
		t.Fatal("message_start translateStreamEvent returned nil")
	}

	// Process message_delta — produces final chunk with Usage.
	finalChunk := p.translateStreamEvent(msgDeltaEvent, responseID, requestModel, &inputTokens, &cacheReadTokens, &cacheWriteTokens)
	if finalChunk == nil {
		t.Fatal("message_delta translateStreamEvent returned nil")
	}
	if finalChunk.Usage == nil {
		t.Fatal("finalChunk.Usage is nil")
	}

	// INSTR-04: cache tokens from message_start must appear in the final chunk's Usage.
	if finalChunk.Usage.CacheReadTokens != 50 {
		t.Errorf("CacheReadTokens: got %d, want 50", finalChunk.Usage.CacheReadTokens)
	}
	if finalChunk.Usage.CacheWriteTokens != 25 {
		t.Errorf("CacheWriteTokens: got %d, want 25", finalChunk.Usage.CacheWriteTokens)
	}
	// Standard token counts must still be correct.
	if finalChunk.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens: got %d, want 10", finalChunk.Usage.PromptTokens)
	}
	if finalChunk.Usage.CompletionTokens != 15 {
		t.Errorf("CompletionTokens: got %d, want 15", finalChunk.Usage.CompletionTokens)
	}
}
