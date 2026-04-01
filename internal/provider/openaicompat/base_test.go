package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// --- helpers ---

func validChatResponse() model.ChatCompletionResponse {
	return model.ChatCompletionResponse{
		ID:      "chatcmpl-test",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "test-model",
		Choices: []model.Choice{
			{
				Index: 0,
				Message: &model.Message{
					Role:    "assistant",
					Content: "Hello!",
				},
				FinishReason: "stop",
			},
		},
		Usage: &model.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:       15,
		},
	}
}

func validEmbeddingsResponse() model.EmbeddingsResponse {
	return model.EmbeddingsResponse{
		Object: "list",
		Data: []model.EmbeddingData{
			{
				Object:    "embedding",
				Embedding: []float64{0.1, 0.2, 0.3},
				Index:     0,
			},
		},
		Model: "text-embedding-ada-002",
		Usage: &model.Usage{
			PromptTokens: 5,
			TotalTokens:  5,
		},
	}
}

func newTestProvider(url string) *BaseProvider {
	return &BaseProvider{
		ProviderName: "test-provider",
		BaseURL:      url,
		Client:       &http.Client{},
		Auth: func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer test-key")
		},
		DoneSentinel: "[DONE]",
	}
}

func simpleRequest() *model.ChatCompletionRequest {
	return &model.ChatCompletionRequest{
		Model: "test-model",
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
	}
}

// --- Tests ---

func TestBaseProvider_Name(t *testing.T) {
	bp := &BaseProvider{ProviderName: "my-provider"}
	if bp.Name() != "my-provider" {
		t.Errorf("Name() = %q, want %q", bp.Name(), "my-provider")
	}
}

func TestBaseProvider_SupportsEmbeddings(t *testing.T) {
	bp := &BaseProvider{}
	if !bp.SupportsEmbeddings() {
		t.Error("SupportsEmbeddings() = false, want true")
	}
}

func TestBaseProvider_ChatCompletion_PostToCorrectURL(t *testing.T) {
	var gotPath string
	var gotContentType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		resp := validChatResponse()
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	_, err := bp.ChatCompletion(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("request path = %q, want %q", gotPath, "/chat/completions")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
}

func TestBaseProvider_ChatCompletion_AuthFunc(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(validChatResponse())
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	_, err := bp.ChatCompletion(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}
}

func TestBaseProvider_ChatCompletion_ExtraHeaders(t *testing.T) {
	var gotCustom string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCustom = r.Header.Get("X-Custom-Header")
		json.NewEncoder(w).Encode(validChatResponse())
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	bp.ExtraHeaders = map[string]string{"X-Custom-Header": "custom-value"}

	_, err := bp.ChatCompletion(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCustom != "custom-value" {
		t.Errorf("X-Custom-Header = %q, want %q", gotCustom, "custom-value")
	}
}

func TestBaseProvider_ChatCompletion_RateLimitError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	_, err := bp.ChatCompletion(context.Background(), simpleRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	rle, ok := err.(*provider.RateLimitError)
	if !ok {
		t.Fatalf("expected *provider.RateLimitError, got %T: %v", err, err)
	}
	if rle.Provider != "test-provider" {
		t.Errorf("Provider = %q, want %q", rle.Provider, "test-provider")
	}
	if rle.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v, want %v", rle.RetryAfter, 30*time.Second)
	}
}

func TestBaseProvider_ChatCompletion_ParseErrorCallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`custom error body`))
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	bp.ParseError = func(statusCode int, body []byte) error {
		return fmt.Errorf("custom parsed: status=%d body=%s", statusCode, string(body))
	}

	_, err := bp.ChatCompletion(context.Background(), simpleRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "custom parsed: status=400 body=custom error body") {
		t.Errorf("error = %q, want to contain custom parsed message", err.Error())
	}
}

func TestBaseProvider_ChatCompletion_DefaultErrorParsing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(model.APIError{
			Error: model.ErrorDetail{Message: "invalid model"},
		})
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	// ParseError is nil — should use default OpenAI error parsing
	_, err := bp.ChatCompletion(context.Background(), simpleRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid model") {
		t.Errorf("error = %q, want to contain 'invalid model'", err.Error())
	}
}

func TestBaseProvider_ChatCompletion_TransformResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(validChatResponse())
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	bp.TransformResponse = func(resp *model.ChatCompletionResponse) *model.ChatCompletionResponse {
		resp.Model = "transformed-" + resp.Model
		return resp
	}

	resp, err := bp.ChatCompletion(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Model != "transformed-test-model" {
		t.Errorf("Model = %q, want %q", resp.Model, "transformed-test-model")
	}
}

func TestBaseProvider_ChatCompletionStream_ParsesSSE(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`{"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`{"id":"2","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`,
		}

		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	stream, err := bp.ChatCompletionStream(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	chunk1, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error: %v", err)
	}
	if chunk1.ID != "1" {
		t.Errorf("chunk1.ID = %q, want %q", chunk1.ID, "1")
	}

	chunk2, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error: %v", err)
	}
	if chunk2.ID != "2" {
		t.Errorf("chunk2.ID = %q, want %q", chunk2.ID, "2")
	}

	// Should get EOF after [DONE]
	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestBaseProvider_ChatCompletionStream_SkipsNonDataLines(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// SSE comment and event type lines should be skipped
		fmt.Fprintf(w, ": this is a comment\n")
		fmt.Fprintf(w, "event: message\n")
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	stream, err := bp.ChatCompletionStream(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error: %v", err)
	}
	if chunk.ID != "1" {
		t.Errorf("chunk.ID = %q, want %q", chunk.ID, "1")
	}

	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestBaseProvider_ChatCompletionStream_TransformStreamChunk(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","object":"chat.completion.chunk","created":1234,"model":"original","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	bp.TransformStreamChunk = func(chunk *model.StreamChunk) *model.StreamChunk {
		chunk.Model = "transformed-" + chunk.Model
		return chunk
	}

	stream, err := bp.ChatCompletionStream(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error: %v", err)
	}
	if chunk.Model != "transformed-original" {
		t.Errorf("Model = %q, want %q", chunk.Model, "transformed-original")
	}
}

func TestBaseProvider_ChatCompletionStream_EmptyDoneSentinel_EOF(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Write one chunk, then close connection (no [DONE] sentinel)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`)
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	bp.DoneSentinel = "" // Empty means no sentinel, stream ends on EOF

	stream, err := bp.ChatCompletionStream(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error: %v", err)
	}
	if chunk.ID != "1" {
		t.Errorf("chunk.ID = %q, want %q", chunk.ID, "1")
	}

	// Stream should end with EOF when connection closes
	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestBaseProvider_ChatCompletionStream_RateLimit429(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	_, err := bp.ChatCompletionStream(context.Background(), simpleRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	rle, ok := err.(*provider.RateLimitError)
	if !ok {
		t.Fatalf("expected *provider.RateLimitError, got %T", err)
	}
	if rle.RetryAfter != 10*time.Second {
		t.Errorf("RetryAfter = %v, want %v", rle.RetryAfter, 10*time.Second)
	}
}

func TestBaseProvider_Embeddings_PostToCorrectURL(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(validEmbeddingsResponse())
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	req := &model.EmbeddingsRequest{
		Model: "text-embedding-ada-002",
		Input: "hello world",
	}
	_, err := bp.Embeddings(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/embeddings" {
		t.Errorf("request path = %q, want %q", gotPath, "/embeddings")
	}
}

func TestBaseProvider_Embeddings_AuthAndHeaders(t *testing.T) {
	var gotAuth string
	var gotCustom string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Custom")
		json.NewEncoder(w).Encode(validEmbeddingsResponse())
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	bp.ExtraHeaders = map[string]string{"X-Custom": "val"}

	req := &model.EmbeddingsRequest{
		Model: "text-embedding-ada-002",
		Input: "hello world",
	}
	_, err := bp.Embeddings(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}
	if gotCustom != "val" {
		t.Errorf("X-Custom = %q, want %q", gotCustom, "val")
	}
}

// ---------------------------------------------------------------------------
// STREAM-03 Verification: SSE streaming works across all OpenAI-compatible
// providers via BaseProvider. These tests specifically verify the requirements
// from STREAM-03 (all providers support streaming).
// ---------------------------------------------------------------------------

// TestStream_STREAM03_SSECommentLines verifies that SSE comment lines
// (prefixed with `:`) are correctly skipped by the stream parser.
// OpenRouter sends `: OPENROUTER PROCESSING` comments during inference.
func TestStream_STREAM03_SSECommentLines(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// Simulate OpenRouter-style comment lines
		fmt.Fprintf(w, ": OPENROUTER PROCESSING\n")
		fmt.Fprintf(w, ": keep-alive\n")
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`)
		fmt.Fprintf(w, ": OPENROUTER PROCESSING\n")
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"2","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	stream, err := bp.ChatCompletionStream(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	// Should receive exactly 2 data chunks, comment lines skipped
	chunk1, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() chunk1 error: %v", err)
	}
	if chunk1.Choices[0].Delta.Content != "Hello" {
		t.Errorf("chunk1 content = %q, want %q", chunk1.Choices[0].Delta.Content, "Hello")
	}

	chunk2, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() chunk2 error: %v", err)
	}
	if chunk2.Choices[0].Delta.Content != " world" {
		t.Errorf("chunk2 content = %q, want %q", chunk2.Choices[0].Delta.Content, " world")
	}

	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("expected io.EOF after [DONE], got %v", err)
	}
}

// TestStream_STREAM03_DoneSentinel verifies that the [DONE] sentinel
// correctly terminates the stream with io.EOF.
func TestStream_STREAM03_DoneSentinel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	stream, err := bp.ChatCompletionStream(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error: %v", err)
	}

	// After [DONE], stream must return io.EOF
	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("expected io.EOF after [DONE] sentinel, got %v", err)
	}
}

// TestStream_STREAM03_EmptyDoneSentinel verifies that providers using
// EOF-only stream termination (empty DoneSentinel) end cleanly.
// vLLM may close the connection without sending [DONE].
func TestStream_STREAM03_EmptyDoneSentinel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","object":"chat.completion.chunk","created":1234,"model":"test","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`)
		// Connection closes without [DONE]
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	bp.DoneSentinel = "" // EOF-only mode

	stream, err := bp.ChatCompletionStream(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error: %v", err)
	}

	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("expected io.EOF when connection closes, got %v", err)
	}
}

// TestStream_STREAM03_TransformChunk verifies that the TransformStreamChunk
// hook is applied to each chunk, enabling providers like MiniMax to inject
// XML-parsed tool calls into the stream.
func TestStream_STREAM03_TransformChunk(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","object":"chat.completion.chunk","created":1234,"model":"lowercase","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`)
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer ts.Close()

	bp := newTestProvider(ts.URL)
	bp.TransformStreamChunk = func(chunk *model.StreamChunk) *model.StreamChunk {
		chunk.Model = strings.ToUpper(chunk.Model)
		return chunk
	}

	stream, err := bp.ChatCompletionStream(context.Background(), simpleRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error: %v", err)
	}
	if chunk.Model != "LOWERCASE" {
		t.Errorf("Model = %q, want %q (uppercased by transform)", chunk.Model, "LOWERCASE")
	}
}

func TestRegistryGet_AcceptsProviderOptions(t *testing.T) {
	reg := provider.NewRegistry()

	var receivedOpts provider.ProviderOptions
	var callCount int32
	reg.Register("test", func(opts provider.ProviderOptions) provider.Provider {
		atomic.AddInt32(&callCount, 1)
		receivedOpts = opts
		return &BaseProvider{ProviderName: "test"}
	})

	opts := provider.ProviderOptions{
		APIKey:       "my-key",
		APIBase:      "https://custom.api.com",
		ExtraHeaders: map[string]string{"X-Test": "value"},
	}

	p, err := reg.Get("test", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "test" {
		t.Errorf("Name() = %q, want %q", p.Name(), "test")
	}
	if receivedOpts.APIKey != "my-key" {
		t.Errorf("APIKey = %q, want %q", receivedOpts.APIKey, "my-key")
	}
	if receivedOpts.APIBase != "https://custom.api.com" {
		t.Errorf("APIBase = %q, want %q", receivedOpts.APIBase, "https://custom.api.com")
	}
	if receivedOpts.ExtraHeaders["X-Test"] != "value" {
		t.Errorf("ExtraHeaders[X-Test] = %q, want %q", receivedOpts.ExtraHeaders["X-Test"], "value")
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("factory called %d times, want 1", callCount)
	}
}
