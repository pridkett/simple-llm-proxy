package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestOllamaName(t *testing.T) {
	p := New(provider.ProviderOptions{})
	if p.Name() != "ollama" {
		t.Errorf("expected Name() = 'ollama', got %q", p.Name())
	}
}

func TestOllamaDefaultBaseURL(t *testing.T) {
	// Verify that a provider created with no APIBase doesn't fail to construct.
	// The actual URL is http://localhost:11434/v1 (not tested via HTTP here).
	p := New(provider.ProviderOptions{})
	if p.Name() != "ollama" {
		t.Fatal("wrong provider name")
	}
}

func TestOllamaNoAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-ollama",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "llama3",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	// Empty API key: no Authorization header should be sent
	p := New(provider.ProviderOptions{APIKey: "", APIBase: server.URL})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "llama3",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header when APIKey is empty, got %q", gotAuth)
	}
}

func TestOllamaWithAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-ollama",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "llama3",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	// Non-empty API key: should set Bearer auth
	p := New(provider.ProviderOptions{APIKey: "ollama-secret", APIBase: server.URL})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "llama3",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Bearer ollama-secret"
	if gotAuth != expected {
		t.Errorf("expected Authorization %q, got %q", expected, gotAuth)
	}
}

func TestOllamaChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path /chat/completions, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-ollama-456",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "llama3:8b",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "Hello from Ollama"}, FinishReason: "stop"}},
			Usage:   &model.Usage{PromptTokens: 8, CompletionTokens: 4, TotalTokens: 12},
		})
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIBase: server.URL})
	resp, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "llama3:8b",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "chatcmpl-ollama-456" {
		t.Errorf("expected ID 'chatcmpl-ollama-456', got %q", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	content, ok := resp.Choices[0].Message.Content.(string)
	if !ok || content != "Hello from Ollama" {
		t.Errorf("expected content 'Hello from Ollama', got %v", resp.Choices[0].Message.Content)
	}
}

func TestOllamaRateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIBase: server.URL})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "llama3",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 429")
	}
	_, ok := err.(*provider.RateLimitError)
	if !ok {
		t.Errorf("expected RateLimitError, got %T: %v", err, err)
	}
}
