package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
)

func TestDiscoverModels(t *testing.T) {
	models := modelListResponse{
		Data: []modelEntry{
			{ID: "google/gemini-2.5-pro"},
			{ID: "anthropic/claude-sonnet-4"},
			{ID: "openai/gpt-4o"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("expected path /models, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("expected Authorization 'Bearer test-key', got %q", auth)
		}
		json.NewEncoder(w).Encode(models)
	}))
	defer server.Close()

	template := config.ModelConfig{
		LiteLLMParams: config.LiteLLMParams{
			APIKey: "test-key",
		},
		RPM: 100,
		TPM: 50000,
	}

	result, err := DiscoverModels(context.Background(), "test-key", server.URL, template)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 models, got %d", len(result))
	}

	// Check first model
	if result[0].ModelName != "google/gemini-2.5-pro" {
		t.Errorf("expected model_name 'google/gemini-2.5-pro', got %q", result[0].ModelName)
	}
	if result[0].LiteLLMParams.Model != "openrouter/google/gemini-2.5-pro" {
		t.Errorf("expected litellm model 'openrouter/google/gemini-2.5-pro', got %q", result[0].LiteLLMParams.Model)
	}
	if result[0].LiteLLMParams.APIKey != "test-key" {
		t.Errorf("expected api_key 'test-key', got %q", result[0].LiteLLMParams.APIKey)
	}
	if result[0].RPM != 100 {
		t.Errorf("expected RPM 100, got %d", result[0].RPM)
	}
	if result[0].TPM != 50000 {
		t.Errorf("expected TPM 50000, got %d", result[0].TPM)
	}

	// Check second model
	if result[1].ModelName != "anthropic/claude-sonnet-4" {
		t.Errorf("expected model_name 'anthropic/claude-sonnet-4', got %q", result[1].ModelName)
	}
	if result[1].LiteLLMParams.Model != "openrouter/anthropic/claude-sonnet-4" {
		t.Errorf("expected litellm model 'openrouter/anthropic/claude-sonnet-4', got %q", result[1].LiteLLMParams.Model)
	}

	// Check third model
	if result[2].ModelName != "openai/gpt-4o" {
		t.Errorf("expected model_name 'openai/gpt-4o', got %q", result[2].ModelName)
	}
}

func TestDiscoverModelsInheritsExtraHeaders(t *testing.T) {
	models := modelListResponse{
		Data: []modelEntry{
			{ID: "google/gemini-2.5-pro"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(models)
	}))
	defer server.Close()

	template := config.ModelConfig{
		LiteLLMParams: config.LiteLLMParams{
			APIKey: "test-key",
			ExtraHeaders: map[string]string{
				"HTTP-Referer": "https://myapp.com",
				"X-Title":      "My App",
			},
		},
		RPM: 50,
	}

	result, err := DiscoverModels(context.Background(), "test-key", server.URL, template)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result))
	}

	eh := result[0].LiteLLMParams.ExtraHeaders
	if eh == nil {
		t.Fatal("expected ExtraHeaders to be non-nil")
	}
	if eh["HTTP-Referer"] != "https://myapp.com" {
		t.Errorf("expected HTTP-Referer 'https://myapp.com', got %q", eh["HTTP-Referer"])
	}
	if eh["X-Title"] != "My App" {
		t.Errorf("expected X-Title 'My App', got %q", eh["X-Title"])
	}
}

func TestDiscoverModelsInheritsAPIBase(t *testing.T) {
	models := modelListResponse{
		Data: []modelEntry{
			{ID: "test/model"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(models)
	}))
	defer server.Close()

	template := config.ModelConfig{
		LiteLLMParams: config.LiteLLMParams{
			APIKey:  "test-key",
			APIBase: server.URL,
		},
	}

	result, err := DiscoverModels(context.Background(), "test-key", server.URL, template)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result))
	}

	if result[0].LiteLLMParams.APIBase != server.URL {
		t.Errorf("expected APIBase %q, got %q", server.URL, result[0].LiteLLMParams.APIBase)
	}
}

func TestDiscoverModelsSkipsEmptyIDs(t *testing.T) {
	models := modelListResponse{
		Data: []modelEntry{
			{ID: "google/gemini-2.5-pro"},
			{ID: ""},
			{ID: "openai/gpt-4o"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(models)
	}))
	defer server.Close()

	template := config.ModelConfig{
		LiteLLMParams: config.LiteLLMParams{
			APIKey: "test-key",
		},
	}

	result, err := DiscoverModels(context.Background(), "test-key", server.URL, template)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 models (empty ID skipped), got %d", len(result))
	}
}

func TestDiscoverModelsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	template := config.ModelConfig{
		LiteLLMParams: config.LiteLLMParams{
			APIKey: "test-key",
		},
	}

	_, err := DiscoverModels(context.Background(), "test-key", server.URL, template)
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestDiscoverModelsInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	template := config.ModelConfig{
		LiteLLMParams: config.LiteLLMParams{
			APIKey: "test-key",
		},
	}

	_, err := DiscoverModels(context.Background(), "test-key", server.URL, template)
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestDiscoverModelsEmptyList(t *testing.T) {
	models := modelListResponse{
		Data: []modelEntry{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(models)
	}))
	defer server.Close()

	template := config.ModelConfig{
		LiteLLMParams: config.LiteLLMParams{
			APIKey: "test-key",
		},
	}

	result, err := DiscoverModels(context.Background(), "test-key", server.URL, template)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 models, got %d", len(result))
	}
}

func TestDiscoverModelsDefaultBaseURL(t *testing.T) {
	// When apiBase is empty, the default base URL should be used.
	// We can't easily test the actual URL without a mock, but we can
	// verify that the function uses the custom apiBase when provided.
	models := modelListResponse{
		Data: []modelEntry{
			{ID: "test/model"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(models)
	}))
	defer server.Close()

	template := config.ModelConfig{
		LiteLLMParams: config.LiteLLMParams{
			APIKey: "test-key",
		},
	}

	// With custom apiBase, should use the server URL
	result, err := DiscoverModels(context.Background(), "test-key", server.URL, template)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result))
	}
}

func TestIsWildcard(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{"wildcard", "openrouter/*", true},
		{"specific model", "openrouter/google/gemini-2.5-pro", false},
		{"openai model", "openai/gpt-4", false},
		{"empty", "", false},
		{"just openrouter", "openrouter/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := config.ModelConfig{
				LiteLLMParams: config.LiteLLMParams{
					Model: tt.model,
				},
			}
			if got := IsWildcard(mc); got != tt.expected {
				t.Errorf("IsWildcard(%q) = %v, want %v", tt.model, got, tt.expected)
			}
		})
	}
}
