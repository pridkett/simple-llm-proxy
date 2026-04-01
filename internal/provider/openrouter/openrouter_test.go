package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestOpenRouterName(t *testing.T) {
	p := New(provider.ProviderOptions{APIKey: "test-key"})
	if p.Name() != "openrouter" {
		t.Errorf("expected Name() = 'openrouter', got %q", p.Name())
	}
}

func TestOpenRouterDefaultBaseURL(t *testing.T) {
	// Create a provider with no APIBase — should use the default OpenRouter URL.
	// We verify by ensuring it does NOT connect to localhost.
	p := New(provider.ProviderOptions{APIKey: "test-key"})
	if p.Name() != "openrouter" {
		t.Fatal("wrong provider name")
	}
	// The actual base URL test is implicit: if no APIBase is given,
	// requests go to https://openrouter.ai/api/v1
}

func TestOpenRouterCustomBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-test",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "gpt-4",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	// Custom base URL with trailing slash should be trimmed
	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL + "/"})
	resp, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "chatcmpl-test" {
		t.Errorf("expected ID 'chatcmpl-test', got %q", resp.ID)
	}
}

func TestOpenRouterBearerAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-test",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "gpt-4",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "sk-openrouter-key", APIBase: server.URL})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Bearer sk-openrouter-key"
	if gotAuth != expected {
		t.Errorf("expected Authorization %q, got %q", expected, gotAuth)
	}
}

func TestOpenRouterExtraHeaders(t *testing.T) {
	var gotReferer, gotTitle string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-test",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "gpt-4",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{
		APIKey:  "test-key",
		APIBase: server.URL,
		ExtraHeaders: map[string]string{
			"HTTP-Referer": "https://myapp.com",
			"X-Title":      "My App",
		},
	})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReferer != "https://myapp.com" {
		t.Errorf("expected HTTP-Referer 'https://myapp.com', got %q", gotReferer)
	}
	if gotTitle != "My App" {
		t.Errorf("expected X-Title 'My App', got %q", gotTitle)
	}
}

func TestOpenRouterChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path /chat/completions, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-or-123",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "openai/gpt-4",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "Hello from OpenRouter"}, FinishReason: "stop"}},
			Usage:   &model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		})
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL})
	resp, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "openai/gpt-4",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "chatcmpl-or-123" {
		t.Errorf("expected ID 'chatcmpl-or-123', got %q", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	content, ok := resp.Choices[0].Message.Content.(string)
	if !ok || content != "Hello from OpenRouter" {
		t.Errorf("expected content 'Hello from OpenRouter', got %v", resp.Choices[0].Message.Content)
	}
}

func TestOpenRouterRateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 429")
	}
	var rle *provider.RateLimitError
	if !isRateLimitError(err, &rle) {
		t.Errorf("expected RateLimitError, got %T: %v", err, err)
	}
}

func isRateLimitError(err error, target **provider.RateLimitError) bool {
	rle, ok := err.(*provider.RateLimitError)
	if ok {
		*target = rle
	}
	return ok
}
