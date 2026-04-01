package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
)

// ---------------------------------------------------------------------------
// Test-scoped mock provider registered under "testmock" provider name.
// ---------------------------------------------------------------------------

// testMockProviderState is global mutable state that controls what the
// registered "testmock" provider instances return.
var testMockProviderState struct {
	chatResponse    *model.ChatCompletionResponse
	chatErr         error
	streamChunks    []*model.StreamChunk
	streamErr       error
	embResponse     *model.EmbeddingsResponse
	embErr          error
	callCount       int
	supportsEmb     bool
	providerBaseURL string // override APIBase for provider URL header test
}

func resetTestMockState() {
	testMockProviderState.chatResponse = nil
	testMockProviderState.chatErr = nil
	testMockProviderState.streamChunks = nil
	testMockProviderState.streamErr = nil
	testMockProviderState.embResponse = nil
	testMockProviderState.embErr = nil
	testMockProviderState.callCount = 0
	testMockProviderState.supportsEmb = false
	testMockProviderState.providerBaseURL = ""
}

// testMockProvider implements provider.Provider using global mutable state.
type testMockProvider struct{}

func (p *testMockProvider) Name() string { return "testmock" }

func (p *testMockProvider) ChatCompletion(_ context.Context, _ *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	testMockProviderState.callCount++
	return testMockProviderState.chatResponse, testMockProviderState.chatErr
}

func (p *testMockProvider) ChatCompletionStream(_ context.Context, _ *model.ChatCompletionRequest) (provider.Stream, error) {
	testMockProviderState.callCount++
	if testMockProviderState.chatErr != nil {
		return nil, testMockProviderState.chatErr
	}
	return &testMockStream{
		chunks: testMockProviderState.streamChunks,
		err:    testMockProviderState.streamErr,
	}, nil
}

func (p *testMockProvider) Embeddings(_ context.Context, _ *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	testMockProviderState.callCount++
	return testMockProviderState.embResponse, testMockProviderState.embErr
}

func (p *testMockProvider) SupportsEmbeddings() bool {
	return testMockProviderState.supportsEmb
}

type testMockStream struct {
	chunks []*model.StreamChunk
	pos    int
	err    error
}

func (s *testMockStream) Recv() (*model.StreamChunk, error) {
	if s.pos < len(s.chunks) {
		c := s.chunks[s.pos]
		s.pos++
		return c, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	return nil, io.EOF
}

func (s *testMockStream) Close() error { return nil }

func init() {
	provider.Register("testmock", func(_ provider.ProviderOptions) provider.Provider {
		return &testMockProvider{}
	})
}

// ---------------------------------------------------------------------------
// Test helpers: config + router creation
// ---------------------------------------------------------------------------

// newTestMockConfig creates a config with one or two testmock deployments.
func newTestMockConfig(models ...string) *config.Config {
	cfg := &config.Config{
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		GeneralSettings: config.GeneralSettings{
			MasterKey: "test-master-key",
			Port:      8080,
		},
	}
	for _, m := range models {
		cfg.ModelList = append(cfg.ModelList, config.ModelConfig{
			ModelName: m,
			LiteLLMParams: config.LiteLLMParams{
				Model:  "testmock/" + m,
				APIKey: "test-api-key",
			},
			RPM: 100,
		})
	}
	return cfg
}

// newTestMockConfigWithBase creates a config with a custom APIBase on the deployment.
func newTestMockConfigWithBase(modelName, apiBase string) *config.Config {
	return &config.Config{
		ModelList: []config.ModelConfig{
			{
				ModelName: modelName,
				LiteLLMParams: config.LiteLLMParams{
					Model:   "testmock/" + modelName,
					APIKey:  "test-api-key",
					APIBase: apiBase,
				},
				RPM: 100,
			},
		},
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		GeneralSettings: config.GeneralSettings{
			MasterKey: "test-master-key",
			Port:      8080,
		},
	}
}

// newTestMockConfigFailover creates a config with two testmock deployments
// for the same model (to test failover).
func newTestMockConfigFailover() *config.Config {
	return &config.Config{
		ModelList: []config.ModelConfig{
			{
				ModelName: "gpt-4",
				LiteLLMParams: config.LiteLLMParams{
					Model:  "testmock/gpt-4-a",
					APIKey: "key-a",
				},
				RPM: 100,
			},
			{
				ModelName: "gpt-4",
				LiteLLMParams: config.LiteLLMParams{
					Model:  "testmock/gpt-4-b",
					APIKey: "key-b",
				},
				RPM: 100,
			},
		},
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "round-robin",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		GeneralSettings: config.GeneralSettings{
			MasterKey: "test-master-key",
			Port:      8080,
		},
	}
}

// makeChatRequest creates a JSON chat completion request body.
func makeChatRequest(modelName string, stream bool) string {
	req := model.ChatCompletionRequest{
		Model: modelName,
		Messages: []model.Message{
			{Role: "user", Content: "hello"},
		},
		Stream: stream,
	}
	b, _ := json.Marshal(req)
	return string(b)
}

// makeAuthRequest creates an HTTP request with the master key auth header.
func makeAuthRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-master-key")
	return req
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestChatRoute_HeadersOnSuccess verifies that a successful single-deployment
// chat response includes X-Provider-Used, X-Providers-Tried, and no
// X-Failover-Reason header.
func TestChatRoute_HeadersOnSuccess(t *testing.T) {
	resetTestMockState()
	testMockProviderState.chatResponse = &model.ChatCompletionResponse{
		ID:      "chatcmpl-test",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{
			{Index: 0, Message: &model.Message{Role: "assistant", Content: "hello back"}, FinishReason: "stop"},
		},
		Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}

	cfg := newTestMockConfig("gpt-4")
	rtr, err := router.New(cfg, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	handler := ChatCompletions(rtr, nil, nil, nil, nil)

	req := makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", false))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// X-Provider-Used: should be "testmock/gpt-4" (provider/model format)
	providerUsed := w.Header().Get(router.HeaderProviderUsed)
	if providerUsed != "testmock/gpt-4" {
		t.Errorf("X-Provider-Used: got %q, want %q", providerUsed, "testmock/gpt-4")
	}

	// X-Providers-Tried: should have exactly 1 entry
	providersTried := w.Header().Get(router.HeaderProvidersTried)
	if providersTried != "testmock/gpt-4" {
		t.Errorf("X-Providers-Tried: got %q, want %q", providersTried, "testmock/gpt-4")
	}

	// X-Failover-Reason: should be absent (no failover occurred)
	failoverReason := w.Header().Get(router.HeaderFailoverReason)
	if failoverReason != "" {
		t.Errorf("X-Failover-Reason should be empty on success, got %q", failoverReason)
	}
}

// TestChatRoute_HeadersOnFailover verifies that when the first deployment fails
// and the second succeeds, X-Providers-Tried lists both and X-Failover-Reason
// contains "error".
func TestChatRoute_HeadersOnFailover(t *testing.T) {
	resetTestMockState()

	// First call fails, second succeeds.
	callNum := 0
	testMockProviderState.chatResponse = &model.ChatCompletionResponse{
		ID:      "chatcmpl-failover",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{
			{Index: 0, Message: &model.Message{Role: "assistant", Content: "recovered"}, FinishReason: "stop"},
		},
		Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}
	// Override ChatCompletion to fail on first call.
	origCallCount := testMockProviderState.callCount
	_ = origCallCount

	// We need a way to fail the first call and succeed on the second.
	// Since testMockProvider uses global state, we use chatErr which affects all calls.
	// Instead, create a custom approach: register a "testfailover" provider.
	provider.Register("testfailover", func(_ provider.ProviderOptions) provider.Provider {
		return &failoverMockProvider{failUntil: 1}
	})

	cfg := &config.Config{
		ModelList: []config.ModelConfig{
			{
				ModelName: "gpt-4",
				LiteLLMParams: config.LiteLLMParams{
					Model:  "testfailover/gpt-4-a",
					APIKey: "key-a",
				},
				RPM: 100,
			},
			{
				ModelName: "gpt-4",
				LiteLLMParams: config.LiteLLMParams{
					Model:  "testfailover/gpt-4-b",
					APIKey: "key-b",
				},
				RPM: 100,
			},
		},
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "round-robin",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		GeneralSettings: config.GeneralSettings{
			MasterKey: "test-master-key",
			Port:      8080,
		},
	}

	rtr, err := router.New(cfg, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	handler := ChatCompletions(rtr, nil, nil, nil, nil)

	req := makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", false))
	w := httptest.NewRecorder()

	// Reset the failover provider call count.
	failoverProviderMu.Lock()
	failoverProviderCallCount = 0
	failoverProviderMu.Unlock()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// X-Providers-Tried: should list 2 entries (comma-separated)
	providersTried := w.Header().Get(router.HeaderProvidersTried)
	parts := strings.Split(providersTried, ", ")
	if len(parts) != 2 {
		t.Errorf("X-Providers-Tried: expected 2 entries, got %d: %q", len(parts), providersTried)
	}

	// X-Failover-Reason: should contain "error"
	failoverReason := w.Header().Get(router.HeaderFailoverReason)
	if !strings.Contains(failoverReason, "error") {
		t.Errorf("X-Failover-Reason: expected to contain 'error', got %q", failoverReason)
	}

	// Verify the provider name includes testfailover
	providerUsed := w.Header().Get(router.HeaderProviderUsed)
	if !strings.HasPrefix(providerUsed, "testfailover/") {
		t.Errorf("X-Provider-Used: expected testfailover/ prefix, got %q", providerUsed)
	}

	_ = callNum
}

// TestChatRoute_StreamingHeadersBeforeChunks verifies that response headers
// (X-Provider-Used) are set before the first SSE chunk in streaming mode.
func TestChatRoute_StreamingHeadersBeforeChunks(t *testing.T) {
	resetTestMockState()
	testMockProviderState.streamChunks = []*model.StreamChunk{
		{
			ID:      "cmp-1",
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   "gpt-4",
			Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: "hello"}}},
		},
		{
			ID:      "cmp-2",
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   "gpt-4",
			Choices: []model.Choice{{Index: 0, Delta: &model.Delta{Content: " world"}, FinishReason: "stop"}},
		},
	}

	cfg := newTestMockConfig("gpt-4")
	rtr, err := router.New(cfg, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	handler := ChatCompletions(rtr, nil, nil, nil, nil)

	req := makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", true))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// X-Provider-Used must be present in headers (set before first SSE chunk).
	providerUsed := w.Header().Get(router.HeaderProviderUsed)
	if providerUsed != "testmock/gpt-4" {
		t.Errorf("X-Provider-Used: got %q, want %q", providerUsed, "testmock/gpt-4")
	}

	// Verify SSE content is present.
	body := w.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Errorf("expected SSE data chunks in body, got: %q", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("expected [DONE] marker in body, got: %q", body)
	}

	// Content-Type must be text/event-stream.
	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want %q", ct, "text/event-stream")
	}
}

// TestChatRoute_ProviderURLHeader verifies that X-Provider-URL-Used reflects
// a custom APIBase when set on the deployment.
func TestChatRoute_ProviderURLHeader(t *testing.T) {
	resetTestMockState()
	testMockProviderState.chatResponse = &model.ChatCompletionResponse{
		ID:      "chatcmpl-url",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{
			{Index: 0, Message: &model.Message{Role: "assistant", Content: "custom url"}, FinishReason: "stop"},
		},
		Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}

	customBase := "https://custom.api.com/v1"
	cfg := newTestMockConfigWithBase("gpt-4", customBase)
	rtr, err := router.New(cfg, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}

	handler := ChatCompletions(rtr, nil, nil, nil, nil)

	req := makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", false))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// X-Provider-URL-Used should match the custom base URL.
	urlUsed := w.Header().Get(router.HeaderProviderURLUsed)
	if urlUsed != customBase {
		t.Errorf("X-Provider-URL-Used: got %q, want %q", urlUsed, customBase)
	}
}

// ---------------------------------------------------------------------------
// Failover mock provider: fails the first N calls, then succeeds.
// ---------------------------------------------------------------------------

var (
	failoverProviderCallCount int
	failoverProviderMu        = &syncMu{}
)

type syncMu struct {
	locked bool
}

func (m *syncMu) Lock()   { m.locked = true }
func (m *syncMu) Unlock() { m.locked = false }

type failoverMockProvider struct {
	failUntil int
}

func (p *failoverMockProvider) Name() string { return "testfailover" }

func (p *failoverMockProvider) ChatCompletion(_ context.Context, _ *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	failoverProviderMu.Lock()
	n := failoverProviderCallCount
	failoverProviderCallCount++
	failoverProviderMu.Unlock()

	if n < p.failUntil {
		return nil, fmt.Errorf("simulated provider error (call %d)", n)
	}
	return &model.ChatCompletionResponse{
		ID:      "chatcmpl-recovered",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []model.Choice{
			{Index: 0, Message: &model.Message{Role: "assistant", Content: "recovered"}, FinishReason: "stop"},
		},
		Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}, nil
}

func (p *failoverMockProvider) ChatCompletionStream(_ context.Context, _ *model.ChatCompletionRequest) (provider.Stream, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *failoverMockProvider) Embeddings(_ context.Context, _ *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *failoverMockProvider) SupportsEmbeddings() bool { return false }
