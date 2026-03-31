package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestNewProvider(t *testing.T) {
	p := New(provider.ProviderOptions{
		APIKey: "test-key",
	})
	if p.Name() != "gemini" {
		t.Errorf("expected name 'gemini', got %q", p.Name())
	}
}

func TestNewProviderDefaultBaseURL(t *testing.T) {
	p := New(provider.ProviderOptions{APIKey: "k"}).(*Provider)
	if p.baseURL != "https://generativelanguage.googleapis.com/v1beta" {
		t.Errorf("expected default base URL, got %q", p.baseURL)
	}
}

func TestNewProviderCustomBaseURL(t *testing.T) {
	p := New(provider.ProviderOptions{APIKey: "k", APIBase: "https://custom.api.com/v1/"}).(*Provider)
	if p.baseURL != "https://custom.api.com/v1" {
		t.Errorf("expected trimmed custom URL, got %q", p.baseURL)
	}
}

func TestSupportsEmbeddings(t *testing.T) {
	p := New(provider.ProviderOptions{APIKey: "k"})
	if p.SupportsEmbeddings() {
		t.Error("expected SupportsEmbeddings() == false")
	}
}

func TestEmbeddingsReturnsError(t *testing.T) {
	p := New(provider.ProviderOptions{APIKey: "k"})
	_, err := p.Embeddings(context.Background(), &model.EmbeddingsRequest{})
	if err == nil {
		t.Error("expected error from Embeddings()")
	}
}

func TestBuildGeminiRequestSimpleText(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model: "gemini-pro",
		Messages: []model.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	gReq := buildGeminiRequest(req, nil)
	if len(gReq.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(gReq.Contents))
	}
	if gReq.Contents[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", gReq.Contents[0].Role)
	}
	if len(gReq.Contents[0].Parts) != 1 || gReq.Contents[0].Parts[0].Text != "Hello" {
		t.Errorf("expected text 'Hello', got %+v", gReq.Contents[0].Parts)
	}
}

func TestBuildGeminiRequestAssistantRole(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model: "gemini-pro",
		Messages: []model.Message{
			{Role: "user", Content: "Hi"},
			{Role: "assistant", Content: "Hello back"},
		},
	}
	gReq := buildGeminiRequest(req, nil)
	if len(gReq.Contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(gReq.Contents))
	}
	if gReq.Contents[1].Role != "model" {
		t.Errorf("expected role 'model' for assistant, got %q", gReq.Contents[1].Role)
	}
}

func TestBuildGeminiRequestSystemInstruction(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model: "gemini-pro",
		Messages: []model.Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hi"},
		},
	}
	gReq := buildGeminiRequest(req, nil)
	if gReq.SystemInstruction == nil {
		t.Fatal("expected systemInstruction to be set")
	}
	if len(gReq.SystemInstruction.Parts) != 1 || gReq.SystemInstruction.Parts[0].Text != "You are a helpful assistant." {
		t.Errorf("unexpected systemInstruction: %+v", gReq.SystemInstruction)
	}
	// System message should not appear in contents
	for _, c := range gReq.Contents {
		if c.Role == "system" {
			t.Error("system role should not appear in contents")
		}
	}
}

func TestBuildGeminiRequestMultipleSystemMessages(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model: "gemini-pro",
		Messages: []model.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "Hi"},
		},
	}
	gReq := buildGeminiRequest(req, nil)
	if gReq.SystemInstruction == nil {
		t.Fatal("expected systemInstruction to be set")
	}
	// Should have combined system text
	text := gReq.SystemInstruction.Parts[0].Text
	if !strings.Contains(text, "You are helpful.") || !strings.Contains(text, "Be concise.") {
		t.Errorf("expected combined system text, got %q", text)
	}
}

func TestBuildGeminiRequestTools(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model: "gemini-pro",
		Messages: []model.Message{
			{Role: "user", Content: "What is the weather?"},
		},
		Tools: []model.Tool{
			{
				Type: "function",
				Function: model.FunctionDef{
					Name:        "get_weather",
					Description: "Get weather info",
					Parameters:  map[string]any{"type": "object"},
				},
			},
		},
	}
	gReq := buildGeminiRequest(req, nil)
	if len(gReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(gReq.Tools))
	}
	if len(gReq.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("expected 1 function declaration, got %d", len(gReq.Tools[0].FunctionDeclarations))
	}
	fd := gReq.Tools[0].FunctionDeclarations[0]
	if fd.Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got %q", fd.Name)
	}
	if fd.Description != "Get weather info" {
		t.Errorf("expected description 'Get weather info', got %q", fd.Description)
	}
}

func TestBuildGeminiRequestToolResult(t *testing.T) {
	req := &model.ChatCompletionRequest{
		Model: "gemini-pro",
		Messages: []model.Message{
			{Role: "user", Content: "What is the weather?"},
			{
				Role: "assistant",
				ToolCalls: []model.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: model.FunctionCall{
							Name:      "get_weather",
							Arguments: `{"location":"NYC"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				Content:    `{"temp":72}`,
				Name:       "get_weather",
				ToolCallID: "call_1",
			},
		},
	}
	gReq := buildGeminiRequest(req, nil)
	// Should have 3 contents: user, model (with functionCall), user (with functionResponse)
	if len(gReq.Contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(gReq.Contents))
	}
	// Check assistant -> model with functionCall
	modelContent := gReq.Contents[1]
	if modelContent.Role != "model" {
		t.Errorf("expected role 'model', got %q", modelContent.Role)
	}
	if len(modelContent.Parts) < 1 || modelContent.Parts[0].FunctionCall == nil {
		t.Fatal("expected functionCall part in model content")
	}
	if modelContent.Parts[0].FunctionCall.Name != "get_weather" {
		t.Errorf("expected functionCall name 'get_weather', got %q", modelContent.Parts[0].FunctionCall.Name)
	}

	// Check tool -> user with functionResponse
	toolContent := gReq.Contents[2]
	if toolContent.Role != "user" {
		t.Errorf("expected tool content role 'user', got %q", toolContent.Role)
	}
	if len(toolContent.Parts) < 1 || toolContent.Parts[0].FunctionResponse == nil {
		t.Fatal("expected functionResponse part in tool content")
	}
	if toolContent.Parts[0].FunctionResponse.Name != "get_weather" {
		t.Errorf("expected functionResponse name 'get_weather', got %q", toolContent.Parts[0].FunctionResponse.Name)
	}
}

func TestBuildGeminiRequestGenerationConfig(t *testing.T) {
	temp := 0.7
	topP := 0.9
	maxTokens := 100
	req := &model.ChatCompletionRequest{
		Model:       "gemini-pro",
		Messages:    []model.Message{{Role: "user", Content: "Hi"}},
		Temperature: &temp,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
		Stop:        []string{"END", "STOP"},
	}
	gReq := buildGeminiRequest(req, nil)
	if gReq.GenerationConfig == nil {
		t.Fatal("expected generationConfig to be set")
	}
	gc := gReq.GenerationConfig
	if gc.Temperature == nil || *gc.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", gc.Temperature)
	}
	if gc.TopP == nil || *gc.TopP != 0.9 {
		t.Errorf("expected topP 0.9, got %v", gc.TopP)
	}
	if gc.MaxOutputTokens == nil || *gc.MaxOutputTokens != 100 {
		t.Errorf("expected maxOutputTokens 100, got %v", gc.MaxOutputTokens)
	}
	if len(gc.StopSequences) != 2 || gc.StopSequences[0] != "END" {
		t.Errorf("expected stop sequences [END, STOP], got %v", gc.StopSequences)
	}
}

func TestBuildGeminiRequestSafetySettings(t *testing.T) {
	settings := []geminiSafetySetting{
		{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "BLOCK_NONE"},
		{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "BLOCK_LOW_AND_ABOVE"},
	}
	req := &model.ChatCompletionRequest{
		Model:    "gemini-pro",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}
	gReq := buildGeminiRequest(req, settings)
	if len(gReq.SafetySettings) != 2 {
		t.Fatalf("expected 2 safety settings, got %d", len(gReq.SafetySettings))
	}
	if gReq.SafetySettings[0].Category != "HARM_CATEGORY_HARASSMENT" {
		t.Errorf("unexpected category: %q", gReq.SafetySettings[0].Category)
	}
}

func TestMapFinishReasonSTOP(t *testing.T) {
	if r := mapFinishReason("STOP"); r != "stop" {
		t.Errorf("expected 'stop', got %q", r)
	}
}

func TestMapFinishReasonMAX_TOKENS(t *testing.T) {
	if r := mapFinishReason("MAX_TOKENS"); r != "length" {
		t.Errorf("expected 'length', got %q", r)
	}
}

func TestMapFinishReasonSAFETY(t *testing.T) {
	if r := mapFinishReason("SAFETY"); r != "content_filter" {
		t.Errorf("expected 'content_filter', got %q", r)
	}
}

func TestMapFinishReasonRECITATION(t *testing.T) {
	if r := mapFinishReason("RECITATION"); r != "stop" {
		t.Errorf("expected 'stop', got %q", r)
	}
}

func TestTranslateGeminiResponseTextContent(t *testing.T) {
	resp := &geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: &geminiContent{
					Role: "model",
					Parts: []geminiPart{
						{Text: "Hello, "},
						{Text: "world!"},
					},
				},
				FinishReason: "STOP",
			},
		},
		UsageMetadata: &geminiUsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 5,
			TotalTokenCount:      15,
		},
	}
	result := translateGeminiResponse(resp, "gemini-pro")
	if result.Object != "chat.completion" {
		t.Errorf("expected object 'chat.completion', got %q", result.Object)
	}
	if result.Model != "gemini-pro" {
		t.Errorf("expected model 'gemini-pro', got %q", result.Model)
	}
	if len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(result.Choices))
	}
	choice := result.Choices[0]
	if choice.Message == nil {
		t.Fatal("expected message to be set")
	}
	content, ok := choice.Message.Content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", choice.Message.Content)
	}
	if content != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %q", content)
	}
	if choice.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", choice.FinishReason)
	}
	if result.Usage == nil {
		t.Fatal("expected usage to be set")
	}
	if result.Usage.PromptTokens != 10 {
		t.Errorf("expected prompt_tokens 10, got %d", result.Usage.PromptTokens)
	}
	if result.Usage.CompletionTokens != 5 {
		t.Errorf("expected completion_tokens 5, got %d", result.Usage.CompletionTokens)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("expected total_tokens 15, got %d", result.Usage.TotalTokens)
	}
}

func TestTranslateGeminiResponseToolCalls(t *testing.T) {
	resp := &geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: &geminiContent{
					Role: "model",
					Parts: []geminiPart{
						{
							FunctionCall: &geminiFunctionCall{
								Name: "get_weather",
								Args: json.RawMessage(`{"location":"NYC"}`),
							},
						},
					},
				},
				FinishReason: "STOP",
			},
		},
	}
	result := translateGeminiResponse(resp, "gemini-pro")
	choice := result.Choices[0]
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(choice.Message.ToolCalls))
	}
	tc := choice.Message.ToolCalls[0]
	if tc.Type != "function" {
		t.Errorf("expected type 'function', got %q", tc.Type)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got %q", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"location":"NYC"}` {
		t.Errorf("expected args, got %q", tc.Function.Arguments)
	}
	// Tool calls should set finish_reason to "tool_calls"
	if choice.FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got %q", choice.FinishReason)
	}
}

func TestTranslateGeminiResponseSAFETYBlock(t *testing.T) {
	// SAFETY block can have nil content
	resp := &geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content:      nil,
				FinishReason: "SAFETY",
			},
		},
	}
	result := translateGeminiResponse(resp, "gemini-pro")
	if len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(result.Choices))
	}
	if result.Choices[0].FinishReason != "content_filter" {
		t.Errorf("expected finish_reason 'content_filter', got %q", result.Choices[0].FinishReason)
	}
	if result.Choices[0].Message == nil {
		t.Fatal("expected message to be set even for SAFETY block")
	}
}

func TestTranslateGeminiResponseSAFETYEmptyParts(t *testing.T) {
	// SAFETY block with empty parts array
	resp := &geminiResponse{
		Candidates: []geminiCandidate{
			{
				Content: &geminiContent{
					Role:  "model",
					Parts: []geminiPart{},
				},
				FinishReason: "SAFETY",
			},
		},
	}
	result := translateGeminiResponse(resp, "gemini-pro")
	if result.Choices[0].FinishReason != "content_filter" {
		t.Errorf("expected finish_reason 'content_filter', got %q", result.Choices[0].FinishReason)
	}
}

func TestChatCompletionRequestURL(t *testing.T) {
	var receivedURL string
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.Path
		receivedAPIKey = r.Header.Get("x-goog-api-key")
		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: &geminiContent{
						Role:  "model",
						Parts: []geminiPart{{Text: "Hi"}},
					},
					FinishReason: "STOP",
				},
			},
			UsageMetadata: &geminiUsageMetadata{
				PromptTokenCount:     5,
				CandidatesTokenCount: 1,
				TotalTokenCount:      6,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{
		APIKey:  "test-api-key-123",
		APIBase: server.URL,
	})

	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "gemini-pro",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedPath := "/models/gemini-pro:generateContent"
	if receivedURL != expectedPath {
		t.Errorf("expected URL path %q, got %q", expectedPath, receivedURL)
	}
	if receivedAPIKey != "test-api-key-123" {
		t.Errorf("expected x-goog-api-key 'test-api-key-123', got %q", receivedAPIKey)
	}
}

func TestChatCompletionRateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{
		APIKey:  "k",
		APIBase: server.URL,
	})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "gemini-pro",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 429")
	}
	var rle *provider.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("expected RateLimitError, got %T: %v", err, err)
	}
	if rle.Provider != "gemini" {
		t.Errorf("expected provider 'gemini', got %q", rle.Provider)
	}
}

func TestChatCompletionNon200Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"internal error","status":"INTERNAL"}}`))
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{
		APIKey:  "k",
		APIBase: server.URL,
	})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "gemini-pro",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestNewProviderSafetySettings(t *testing.T) {
	p := New(provider.ProviderOptions{
		APIKey: "k",
		SafetySettings: []provider.SafetySetting{
			{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "BLOCK_NONE"},
		},
	}).(*Provider)
	if len(p.safetySettings) != 1 {
		t.Fatalf("expected 1 safety setting, got %d", len(p.safetySettings))
	}
	if p.safetySettings[0].Category != "HARM_CATEGORY_HARASSMENT" {
		t.Errorf("unexpected category: %q", p.safetySettings[0].Category)
	}
}

// --- Streaming Tests ---

func TestChatCompletionStreamURL(t *testing.T) {
	var receivedURL string
	var receivedAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.RequestURI()
		receivedAPIKey = r.Header.Get("x-goog-api-key")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}

		// Write SSE data
		fmt.Fprintf(w, "data: %s\n\n", `{"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"}}]}`)
		flusher.Flush()
		fmt.Fprintf(w, "data: %s\n\n", `{"candidates":[{"content":{"parts":[{"text":" world"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`)
		flusher.Flush()
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{
		APIKey:  "stream-key-456",
		APIBase: server.URL,
	})

	stream, err := p.ChatCompletionStream(context.Background(), &model.ChatCompletionRequest{
		Model:    "gemini-pro",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	expectedPath := "/models/gemini-pro:streamGenerateContent?alt=sse"
	if receivedURL != expectedPath {
		t.Errorf("expected URL %q, got %q", expectedPath, receivedURL)
	}
	if receivedAPIKey != "stream-key-456" {
		t.Errorf("expected x-goog-api-key 'stream-key-456', got %q", receivedAPIKey)
	}

	// Read first chunk
	chunk1, err := stream.Recv()
	if err != nil {
		t.Fatalf("unexpected error on first Recv: %v", err)
	}
	if len(chunk1.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chunk1.Choices))
	}
	if chunk1.Choices[0].Delta == nil {
		t.Fatal("expected delta in first chunk")
	}
	if chunk1.Choices[0].Delta.Content != "Hello" {
		t.Errorf("expected delta content 'Hello', got %q", chunk1.Choices[0].Delta.Content)
	}
	if chunk1.Object != "chat.completion.chunk" {
		t.Errorf("expected object 'chat.completion.chunk', got %q", chunk1.Object)
	}

	// Read second chunk
	chunk2, err := stream.Recv()
	if err != nil {
		t.Fatalf("unexpected error on second Recv: %v", err)
	}
	if chunk2.Choices[0].Delta.Content != " world" {
		t.Errorf("expected delta content ' world', got %q", chunk2.Choices[0].Delta.Content)
	}
	if chunk2.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", chunk2.Choices[0].FinishReason)
	}
	// Check usage in final chunk
	if chunk2.Usage == nil {
		t.Fatal("expected usage in final chunk")
	}
	if chunk2.Usage.PromptTokens != 5 {
		t.Errorf("expected prompt_tokens 5, got %d", chunk2.Usage.PromptTokens)
	}
	if chunk2.Usage.CompletionTokens != 2 {
		t.Errorf("expected completion_tokens 2, got %d", chunk2.Usage.CompletionTokens)
	}

	// Stream should end with EOF
	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestChatCompletionStreamSAFETY(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		fmt.Fprintf(w, "data: %s\n\n", `{"candidates":[{"finishReason":"SAFETY"}]}`)
		flusher.Flush()
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{
		APIKey:  "k",
		APIBase: server.URL,
	})
	stream, err := p.ChatCompletionStream(context.Background(), &model.ChatCompletionRequest{
		Model:    "gemini-pro",
		Messages: []model.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Choices[0].FinishReason != "content_filter" {
		t.Errorf("expected 'content_filter', got %q", chunk.Choices[0].FinishReason)
	}
}

func TestChatCompletionStreamRateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{
		APIKey:  "k",
		APIBase: server.URL,
	})
	_, err := p.ChatCompletionStream(context.Background(), &model.ChatCompletionRequest{
		Model:    "gemini-pro",
		Messages: []model.Message{{Role: "user", Content: "test"}},
	})
	if err == nil {
		t.Fatal("expected error for 429")
	}
	var rle *provider.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("expected RateLimitError, got %T: %v", err, err)
	}
}

func TestChatCompletionStreamUsageFromFinalChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		// Only chunk has usage metadata
		fmt.Fprintf(w, "data: %s\n\n", `{"candidates":[{"content":{"parts":[{"text":"done"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3,"totalTokenCount":13}}`)
		flusher.Flush()
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{
		APIKey:  "k",
		APIBase: server.URL,
	})
	stream, err := p.ChatCompletionStream(context.Background(), &model.ChatCompletionRequest{
		Model:    "gemini-pro",
		Messages: []model.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()

	chunk, err := stream.Recv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Usage == nil {
		t.Fatal("expected usage in chunk")
	}
	if chunk.Usage.TotalTokens != 13 {
		t.Errorf("expected total_tokens 13, got %d", chunk.Usage.TotalTokens)
	}
}
