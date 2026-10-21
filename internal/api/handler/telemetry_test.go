package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	proxyotel "github.com/pwagstro/simple_llm_proxy/internal/otel"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type telemetryAuthStore struct {
	*captureStorage
	key *storage.APIKey
}

func (s *telemetryAuthStore) GetAPIKeyByHash(context.Context, string) (*storage.APIKey, error) {
	return s.key, nil
}

func testTracer(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { provider.Shutdown(context.Background()) })
	return recorder, provider
}

func spanAttrs(span sdktrace.ReadOnlySpan) map[string]any {
	attrs := make(map[string]any)
	for _, a := range span.Attributes() {
		attrs[string(a.Key)] = a.Value.AsInterface()
	}
	return attrs
}

func TestChatTelemetryIdentityCostAndEndpoint(t *testing.T) {
	resetTestMockState()
	testMockProviderState.chatResponse = &model.ChatCompletionResponse{Usage: &model.Usage{PromptTokens: 5, CompletionTokens: 3}}
	cfg := newTestMockConfigWithBase("gpt-4", "https://provider.example.com/v1")
	rtr, err := router.New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	cm := costmap.New()
	cm.SetCustomSpec("gpt-4", costmap.ModelSpec{InputCostPerToken: 0.01, OutputCostPerToken: 0.02})
	recorder, provider := testTracer(t)
	store := &telemetryAuthStore{captureStorage: &captureStorage{}, key: &storage.APIKey{
		ID: 1, Name: "named-key", KeyHash: "secret-hash", AppName: "my-app", TeamName: "my-team", IsActive: true,
	}}
	h := middleware.KeyAuth("test-master-key", store, keystore.New(0), nil, nil)(ChatCompletions(rtr, nil, nil, cm, nil, config.GeneralSettings{}, provider.Tracer("test")))
	req := makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", false))
	req.Header.Set("Authorization", "Bearer sk-app-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans", len(spans))
	}
	a := spanAttrs(spans[0])
	for key, expected := range map[string]any{
		proxyotel.GenAIOperationName: "chat", proxyotel.GenAIRequestModel: "gpt-4",
		proxyotel.GenAIProviderName: "testmock", proxyotel.GenAIInputTokens: int64(5),
		proxyotel.GenAIOutputTokens: int64(3), proxyotel.CostTotalUSD: 0.11,
		proxyotel.Team: "my-team", proxyotel.Application: "my-app", proxyotel.APIKeyName: "named-key",
		proxyotel.ServerAddress: "provider.example.com", proxyotel.ServerPort: int64(443),
	} {
		if a[key] != expected {
			t.Errorf("%s = %v, want %v", key, a[key], expected)
		}
	}
	for key, val := range a {
		if strings.Contains(key, "secret") || strings.Contains(valString(val), "secret-hash") || strings.Contains(valString(val), "sk-app-secret") {
			t.Fatalf("secret leaked into span: %s=%v", key, val)
		}
	}
}

func valString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func TestMasterKeySpanOmitsKeyIdentity(t *testing.T) {
	resetTestMockState()
	testMockProviderState.chatResponse = &model.ChatCompletionResponse{Usage: &model.Usage{PromptTokens: 1}}
	rtr, _ := router.New(newTestMockConfig("gpt-4"), nil)
	recorder, provider := testTracer(t)
	w := httptest.NewRecorder()
	ChatCompletions(rtr, nil, nil, nil, nil, config.GeneralSettings{}, provider.Tracer("test")).ServeHTTP(w,
		makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", false)))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d", w.Code)
	}
	a := spanAttrs(recorder.Ended()[0])
	for _, key := range []string{proxyotel.Team, proxyotel.Application, proxyotel.APIKeyName} {
		if _, found := a[key]; found {
			t.Errorf("unexpected %s", key)
		}
	}
}

func TestStreamingFailureEndsSpanWithError(t *testing.T) {
	resetTestMockState()
	testMockProviderState.streamErr = errors.New("upstream broke")
	rtr, _ := router.New(newTestMockConfig("gpt-4"), nil)
	recorder, provider := testTracer(t)
	w := httptest.NewRecorder()
	ChatCompletions(rtr, nil, nil, nil, nil, config.GeneralSettings{}, provider.Tracer("test")).ServeHTTP(w,
		makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", true)))
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans", len(spans))
	}
	a := spanAttrs(spans[0])
	if a[proxyotel.ErrorType] == nil || spans[0].Status().Code != codes.Error {
		t.Fatalf("stream failure not recorded: attrs=%v status=%v", a, spans[0].Status())
	}
}

type noFlushWriter struct{ header http.Header }

func (w *noFlushWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (*noFlushWriter) Write(b []byte) (int, error) { return len(b), nil }
func (*noFlushWriter) WriteHeader(int)             {}

func TestStreamingMissingFlusherEndsSpanWithError(t *testing.T) {
	resetTestMockState()
	rtr, _ := router.New(newTestMockConfig("gpt-4"), nil)
	recorder, provider := testTracer(t)
	ChatCompletions(rtr, nil, nil, nil, nil, config.GeneralSettings{}, provider.Tracer("test")).ServeHTTP(&noFlushWriter{},
		makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", true)))
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Status().Code != codes.Error {
		t.Fatalf("missing flusher span was not closed with error: %+v", spans)
	}
}

func TestStreamingCancellationEndsSpanWithError(t *testing.T) {
	resetTestMockState()
	testMockProviderState.streamErr = context.Canceled
	rtr, _ := router.New(newTestMockConfig("gpt-4"), nil)
	recorder, provider := testTracer(t)
	ChatCompletions(rtr, nil, nil, nil, nil, config.GeneralSettings{}, provider.Tracer("test")).ServeHTTP(httptest.NewRecorder(),
		makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", true)))
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Status().Code != codes.Error {
		t.Fatalf("cancellation span was not closed with error: %+v", spans)
	}
}

func TestEmbeddingsTelemetry(t *testing.T) {
	resetTestMockState()
	testMockProviderState.supportsEmb = true
	testMockProviderState.embResponse = &model.EmbeddingsResponse{Usage: &model.Usage{PromptTokens: 7}}
	rtr, _ := router.New(newTestMockConfig("embedding-model"), nil)
	recorder, provider := testTracer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(`{"model":"embedding-model","input":"hello"}`))
	w := httptest.NewRecorder()
	Embeddings(rtr, nil, nil, nil, nil, provider.Tracer("test")).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	a := spanAttrs(recorder.Ended()[0])
	if a[proxyotel.GenAIOperationName] != "embeddings" || a[proxyotel.GenAIInputTokens] != int64(7) {
		t.Fatalf("embedding attributes: %v", a)
	}
}

func TestFailedAttemptsAreEventsOnOneLogicalSpan(t *testing.T) {
	resetTestMockState()
	testMockProviderState.chatErr = errors.New("upstream failed")
	rtr, _ := router.New(newTestMockConfig("gpt-4"), nil)
	recorder, provider := testTracer(t)
	w := httptest.NewRecorder()
	ChatCompletions(rtr, nil, nil, nil, nil, config.GeneralSettings{}, provider.Tracer("test")).ServeHTTP(w,
		makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", false)))
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected one logical span, got %d", len(spans))
	}
	if len(spans[0].Events()) == 0 {
		t.Fatal("failed attempt event missing")
	}
	if spanAttrs(spans[0])[proxyotel.ErrorType] == nil {
		t.Fatal("terminal error type missing")
	}
}

func TestFailoverSuccessKeepsOneSpanAndAttemptEvent(t *testing.T) {
	provider.Register("testfailover", func(provider.ProviderOptions) provider.Provider { return &failoverMockProvider{failUntil: 1} })
	cfg := newTestMockConfig()
	cfg.RouterSettings.RoutingStrategy = "round-robin"
	for _, modelName := range []string{"gpt-4-a", "gpt-4-b"} {
		cfg.ModelList = append(cfg.ModelList, config.ModelConfig{ModelName: "gpt-4", LiteLLMParams: config.LiteLLMParams{
			Model: "testfailover/" + modelName, APIKey: "test-key",
		}})
	}
	rtr, err := router.New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	failoverProviderCallCount = 0
	recorder, tp := testTracer(t)
	w := httptest.NewRecorder()
	ChatCompletions(rtr, nil, nil, nil, nil, config.GeneralSettings{}, tp.Tracer("test")).ServeHTTP(w,
		makeAuthRequest(http.MethodPost, "/v1/chat/completions", makeChatRequest("gpt-4", false)))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans=%d", len(spans))
	}
	if len(spans[0].Events()) != 1 {
		t.Fatalf("attempt events=%d", len(spans[0].Events()))
	}
	if spanAttrs(spans[0])[proxyotel.GenAIProviderName] != "testfailover" {
		t.Fatal("winning provider missing")
	}
}

func TestResponsesTelemetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_otel", "status": "completed",
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 3},
		})
	}))
	defer server.Close()
	rtr := responsesRouterForTest(t, server.URL)
	recorder, provider := testTracer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test-model","input":"hi"}`))
	w := httptest.NewRecorder()
	Responses(rtr, nil, nil, nil, nil, config.GeneralSettings{}, provider.Tracer("test")).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	a := spanAttrs(recorder.Ended()[0])
	if a[proxyotel.GenAIOperationName] != "generate_content" || a[proxyotel.GenAIInputTokens] != int64(2) || a[proxyotel.GenAIOutputTokens] != int64(3) {
		t.Fatalf("Responses span attributes: %v", a)
	}
}
