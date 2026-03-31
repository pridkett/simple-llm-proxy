package vllm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestVLLMName(t *testing.T) {
	p := New(provider.ProviderOptions{APIBase: "http://localhost:8000/v1"})
	if p.Name() != "vllm" {
		t.Errorf("expected Name() = 'vllm', got %q", p.Name())
	}
}

func TestVLLMBearerAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-vllm",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "meta-llama/Llama-3-8b",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "vllm-secret", APIBase: server.URL})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "meta-llama/Llama-3-8b",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Bearer vllm-secret"
	if gotAuth != expected {
		t.Errorf("expected Authorization %q, got %q", expected, gotAuth)
	}
}

func TestVLLMNoAuthWhenKeyEmpty(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-vllm",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "meta-llama/Llama-3-8b",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	// vLLM with no API key should not send auth header
	p := New(provider.ProviderOptions{APIKey: "", APIBase: server.URL})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "meta-llama/Llama-3-8b",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header when APIKey is empty, got %q", gotAuth)
	}
}

func TestVLLMRequiresAPIBase(t *testing.T) {
	// vLLM has no default base URL — empty APIBase means empty string which would fail
	p := New(provider.ProviderOptions{APIKey: "test", APIBase: ""})
	if p.Name() != "vllm" {
		t.Fatal("wrong provider name")
	}
	// With empty base URL, a request should fail (can't connect to empty URL)
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "test",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("expected error when APIBase is empty")
	}
}

func TestVLLMChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path /chat/completions, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-vllm-789",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "meta-llama/Llama-3-70b",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "Hello from vLLM"}, FinishReason: "stop"}},
			Usage:   &model.Usage{PromptTokens: 12, CompletionTokens: 6, TotalTokens: 18},
		})
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL})
	resp, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "meta-llama/Llama-3-70b",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "chatcmpl-vllm-789" {
		t.Errorf("expected ID 'chatcmpl-vllm-789', got %q", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	content, ok := resp.Choices[0].Message.Content.(string)
	if !ok || content != "Hello from vLLM" {
		t.Errorf("expected content 'Hello from vLLM', got %v", resp.Choices[0].Message.Content)
	}
}

func TestVLLMRateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "test",
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
