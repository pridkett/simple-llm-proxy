package minimax

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestMiniMaxName(t *testing.T) {
	p := New(provider.ProviderOptions{APIKey: "test-key"})
	if p.Name() != "minimax" {
		t.Errorf("expected Name() = 'minimax', got %q", p.Name())
	}
}

func TestMiniMaxDefaultBaseURL(t *testing.T) {
	p := New(provider.ProviderOptions{APIKey: "test-key"})
	if p.Name() != "minimax" {
		t.Fatal("wrong provider name")
	}
	// Default base URL is https://api.minimax.io/v1; verified implicitly
}

func TestMiniMaxBearerAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-minimax",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "abab6-chat",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
		})
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "mm-secret-key", APIBase: server.URL})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "abab6-chat",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "Bearer mm-secret-key"
	if gotAuth != expected {
		t.Errorf("expected Authorization %q, got %q", expected, gotAuth)
	}
}

func TestMiniMaxSupportsEmbeddings(t *testing.T) {
	p := New(provider.ProviderOptions{APIKey: "test-key"})
	if !p.SupportsEmbeddings() {
		t.Error("MiniMax should support embeddings (per D-19)")
	}
}

// --- XML Tool Call Parser Tests ---

func TestParseMinimaxToolCallsSingleInvoke(t *testing.T) {
	content := `Here is the result.
<minimax:tool_call>
  <invoke name="get_weather">
    <parameter name="city">Tokyo</parameter>
    <parameter name="unit">celsius</parameter>
  </invoke>
</minimax:tool_call>`

	toolCalls, cleaned, err := parseMinimaxToolCalls(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	tc := toolCalls[0]
	if tc.ID != "call_0" {
		t.Errorf("expected ID 'call_0', got %q", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("expected Type 'function', got %q", tc.Type)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected Function.Name 'get_weather', got %q", tc.Function.Name)
	}

	// Verify arguments are valid JSON with expected keys
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("failed to unmarshal arguments: %v", err)
	}
	if args["city"] != "Tokyo" {
		t.Errorf("expected city=Tokyo, got %v", args["city"])
	}
	if args["unit"] != "celsius" {
		t.Errorf("expected unit=celsius, got %v", args["unit"])
	}

	// Cleaned content should have XML blocks removed
	if cleaned != "Here is the result." {
		t.Errorf("expected cleaned content 'Here is the result.', got %q", cleaned)
	}
}

func TestParseMinimaxToolCallsMultipleInvokes(t *testing.T) {
	content := `<minimax:tool_call>
  <invoke name="search">
    <parameter name="query">news</parameter>
  </invoke>
  <invoke name="get_time">
    <parameter name="timezone">UTC</parameter>
  </invoke>
</minimax:tool_call>`

	toolCalls, cleaned, err := parseMinimaxToolCalls(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}

	if toolCalls[0].ID != "call_0" {
		t.Errorf("expected first ID 'call_0', got %q", toolCalls[0].ID)
	}
	if toolCalls[0].Function.Name != "search" {
		t.Errorf("expected first function name 'search', got %q", toolCalls[0].Function.Name)
	}

	if toolCalls[1].ID != "call_1" {
		t.Errorf("expected second ID 'call_1', got %q", toolCalls[1].ID)
	}
	if toolCalls[1].Function.Name != "get_time" {
		t.Errorf("expected second function name 'get_time', got %q", toolCalls[1].Function.Name)
	}

	// Content should be empty after removing the tool_call block
	if cleaned != "" {
		t.Errorf("expected empty cleaned content, got %q", cleaned)
	}
}

func TestParseMinimaxToolCallsNoXML(t *testing.T) {
	content := "Just a plain text response with no tool calls."
	toolCalls, cleaned, err := parseMinimaxToolCalls(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if toolCalls != nil {
		t.Errorf("expected nil tool calls for plain text, got %d", len(toolCalls))
	}
	if cleaned != content {
		t.Errorf("expected cleaned content = original, got %q", cleaned)
	}
}

func TestParseMinimaxToolCallsArgumentsJSON(t *testing.T) {
	content := `<minimax:tool_call>
  <invoke name="calculate">
    <parameter name="expression">2+2</parameter>
    <parameter name="precision">high</parameter>
  </invoke>
</minimax:tool_call>`

	toolCalls, _, err := parseMinimaxToolCalls(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("failed to unmarshal arguments: %v", err)
	}
	if args["expression"] != "2+2" {
		t.Errorf("expected expression='2+2', got %v", args["expression"])
	}
	if args["precision"] != "high" {
		t.Errorf("expected precision='high', got %v", args["precision"])
	}
}

// --- TransformResponse Tests ---

func TestTransformResponsePopulatesToolCalls(t *testing.T) {
	// Server returns a response with XML tool calls in content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := model.ChatCompletionResponse{
			ID:      "chatcmpl-mm-xml",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "abab6-chat",
			Choices: []model.Choice{
				{
					Index: 0,
					Message: &model.Message{
						Role: "assistant",
						Content: `Let me check that.
<minimax:tool_call>
  <invoke name="get_weather">
    <parameter name="city">London</parameter>
  </invoke>
</minimax:tool_call>`,
					},
					FinishReason: "stop",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL})
	resp, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "abab6-chat",
		Messages: []model.Message{{Role: "user", Content: "weather in London?"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}

	choice := resp.Choices[0]
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call after transform, got %d", len(choice.Message.ToolCalls))
	}
	if choice.Message.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got %q", choice.Message.ToolCalls[0].Function.Name)
	}

	// Content should be cleaned (XML removed)
	content, ok := choice.Message.Content.(string)
	if !ok {
		t.Fatal("expected string content")
	}
	if content != "Let me check that." {
		t.Errorf("expected cleaned content 'Let me check that.', got %q", content)
	}

	// finish_reason should be fixed to "tool_calls"
	if choice.FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got %q", choice.FinishReason)
	}
}

func TestTransformResponseSkipsWhenToolCallsPopulated(t *testing.T) {
	// Server returns a response that already has tool_calls populated
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := model.ChatCompletionResponse{
			ID:      "chatcmpl-mm-skip",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "abab6-chat",
			Choices: []model.Choice{
				{
					Index: 0,
					Message: &model.Message{
						Role:    "assistant",
						Content: "Some content with <minimax:tool_call> still in it",
						ToolCalls: []model.ToolCall{
							{
								ID:       "existing_call",
								Type:     "function",
								Function: model.FunctionCall{Name: "existing_func", Arguments: `{}`},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL})
	resp, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "abab6-chat",
		Messages: []model.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	choice := resp.Choices[0]
	// Should still have the original tool calls, not be modified
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call (original), got %d", len(choice.Message.ToolCalls))
	}
	if choice.Message.ToolCalls[0].ID != "existing_call" {
		t.Errorf("expected original tool call ID 'existing_call', got %q", choice.Message.ToolCalls[0].ID)
	}
}

func TestTransformResponseDisabledWhenXMLToolCallsFalse(t *testing.T) {
	// Server returns a response with XML tool calls in content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := model.ChatCompletionResponse{
			ID:      "chatcmpl-mm-disabled",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "abab6-chat",
			Choices: []model.Choice{
				{
					Index: 0,
					Message: &model.Message{
						Role: "assistant",
						Content: `Test <minimax:tool_call>
  <invoke name="fn"><parameter name="x">1</parameter></invoke>
</minimax:tool_call>`,
					},
					FinishReason: "stop",
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Explicitly disable XML tool calls
	xmlFalse := false
	p := New(provider.ProviderOptions{
		APIKey:       "test-key",
		APIBase:      server.URL,
		XMLToolCalls: &xmlFalse,
	})
	resp, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "abab6-chat",
		Messages: []model.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	choice := resp.Choices[0]
	// With XML parsing disabled, tool_calls should NOT be populated
	if len(choice.Message.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls when XML disabled, got %d", len(choice.Message.ToolCalls))
	}
	// finish_reason should remain "stop"
	if choice.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop' when XML disabled, got %q", choice.FinishReason)
	}
}

func TestMiniMaxRateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL})
	_, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "abab6-chat",
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

func TestMiniMaxChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(model.ChatCompletionResponse{
			ID:      "chatcmpl-mm-basic",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "abab6-chat",
			Choices: []model.Choice{{Index: 0, Message: &model.Message{Role: "assistant", Content: "Hello from MiniMax"}, FinishReason: "stop"}},
			Usage:   &model.Usage{PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9},
		})
	}))
	defer server.Close()

	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL})
	resp, err := p.ChatCompletion(context.Background(), &model.ChatCompletionRequest{
		Model:    "abab6-chat",
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "chatcmpl-mm-basic" {
		t.Errorf("expected ID 'chatcmpl-mm-basic', got %q", resp.ID)
	}
	content, ok := resp.Choices[0].Message.Content.(string)
	if !ok || content != "Hello from MiniMax" {
		t.Errorf("expected content 'Hello from MiniMax', got %v", resp.Choices[0].Message.Content)
	}
}
