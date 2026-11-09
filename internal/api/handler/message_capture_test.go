package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/otel/content"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"go.opentelemetry.io/otel/trace"
)

func captureExchange(t *testing.T, protocol content.Protocol, path, body, token string, app int64, handler func(trace.Tracer) http.Handler) (map[string]any, string) {
	t.Helper()
	recorder, provider := testTracer(t)
	tracer := provider.Tracer("capture-test")
	store := &telemetryAuthStore{captureStorage: &captureStorage{}, key: &storage.APIKey{
		ID: 1, ApplicationID: app, IsActive: true,
	}}
	capture := middleware.CaptureMessages(protocol, func() config.OTelMessageCapture {
		return config.OTelMessageCapture{ApplicationIDs: []int64{42}, MaxBytes: 1024}
	})(handler(tracer))
	h := middleware.OTel(tracer)(middleware.KeyAuth("master", store, keystore.New(0), nil, nil)(capture))
	req := makeAuthRequest(http.MethodPost, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected HTTP and model spans, got %d", len(spans))
	}
	if attrs := spanAttrs(spans[1]); attrs["gen_ai.input.messages"] != nil || attrs["gen_ai.output.messages"] != nil {
		t.Fatalf("HTTP span got message payloads: %v", attrs)
	}
	return spanAttrs(spans[0]), w.Body.String()
}

func TestChatStructuredCaptureEligibilityAndRedaction(t *testing.T) {
	resetTestMockState()
	testMockProviderState.chatResponse = &model.ChatCompletionResponse{
		ID: "chat-id", Choices: []model.Choice{{Message: &model.Message{Role: "assistant", Content: "Authorization: Bearer output-secret"}}},
	}
	rtr, err := router.New(newTestMockConfig("gpt-4"), nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := func(tracer trace.Tracer) http.Handler {
		return ChatCompletions(rtr, nil, nil, nil, nil, config.GeneralSettings{}, tracer)
	}
	body := `{"model":"gpt-4","messages":[{"role":"user","content":[{"type":"text","text":"run the tests sk-secret123"},{"type":"image_url","image_url":{"url":"data:image/png;base64,hidden"}}]}]}`
	for _, tc := range []struct {
		name, token string
		app         int64
		want        bool
	}{
		{"test application", "sk-app-credential", 42, true},
		{"ordinary application", "sk-app-credential", 43, false},
		{"master key", "master", 42, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attrs, downstream := captureExchange(t, content.Chat, "/v1/chat/completions", body, tc.token, tc.app, handler)
			input := valString(attrs["gen_ai.input.messages"])
			output := valString(attrs["gen_ai.output.messages"])
			if !strings.Contains(downstream, "output-secret") {
				t.Fatal("capture modified the live response")
			}
			if !tc.want {
				if input != "" || output != "" || attrs["langfuse.observation.input"] != nil {
					t.Fatalf("ineligible content captured: %v", attrs)
				}
				return
			}
			if !strings.Contains(input, "run the tests [REDACTED]") || !strings.Contains(output, "[REDACTED]") ||
				strings.Contains(input, "hidden") || strings.Contains(output, "output-secret") ||
				attrs["gen_ai.response.id"] != "chat-id" || attrs["langfuse.observation.output"] != output {
				t.Fatalf("unexpected capture: %v", attrs)
			}
		})
	}
}

func TestAnthropicStructuredCaptureKeepsWireOrderAndAssemblesTool(t *testing.T) {
	resetTestMockState()
	index := 0
	testMockProviderState.streamChunks = []*model.StreamChunk{
		{Choices: []model.Choice{{Delta: &model.Delta{Content: "I'll run it."}}}},
		{Choices: []model.Choice{{Delta: &model.Delta{ToolCalls: []model.ToolCall{
			{Index: &index, ID: "call-new", Function: model.FunctionCall{Name: "shell", Arguments: `{"command":"go`}},
		}}}}},
		{Choices: []model.Choice{{Delta: &model.Delta{ToolCalls: []model.ToolCall{
			{Index: &index, Function: model.FunctionCall{Arguments: ` test","api_key":"tool-secret"}`}},
		}}, FinishReason: "tool_calls"}}},
	}
	rtr, err := router.New(newTestMockConfig("gpt-4"), nil)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"gpt-4","max_tokens":200,"stream":true,"system":[{"type":"text","text":"You are an agent"}],"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-old","content":"prior result"},{"type":"text","text":"now run tests"}]}]}`
	attrs, downstream := captureExchange(t, content.Messages, "/v1/messages", body, "sk-app-test", 42, func(tracer trace.Tracer) http.Handler {
		return Messages(rtr, nil, nil, nil, nil, config.GeneralSettings{}, tracer)
	})
	input := valString(attrs["gen_ai.input.messages"])
	output := valString(attrs["gen_ai.output.messages"])
	if prior, next := strings.Index(input, "prior result"), strings.Index(input, "now run tests"); prior < 0 || next <= prior ||
		!strings.Contains(valString(attrs["gen_ai.system_instructions"]), "You are an agent") {
		t.Fatalf("bad Anthropic input: %v", attrs)
	}
	for _, want := range []string{`"type":"tool_call"`, `"id":"call-new"`, `"name":"shell"`, `"command":"go test"`, `"api_key":"[REDACTED]"`, `"finish_reason":"tool_use"`} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in %s", want, output)
		}
	}
	if strings.Contains(output, "tool-secret") || !strings.Contains(downstream, "tool-secret") ||
		attrs["llmproxy.capture.output_partial"] != nil || !strings.HasPrefix(valString(attrs["gen_ai.response.id"]), "msg_") {
		t.Fatalf("bad Anthropic output: %v", attrs)
	}
}

func TestResponsesStructuredCaptureRetainsToolMetadataInPartialStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"type":"response.created","response":{"id":"resp_new","status":"in_progress"}}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"shell","arguments":""}}

data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"cmd\":\""}

data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"go test\"}"}

`)
	}))
	defer upstream.Close()
	rtr := responsesRouterForTest(t, upstream.URL)
	body := `{"model":"test-model","stream":true,"instructions":"run tests","previous_response_id":"resp_old","input":[{"type":"function_call_output","call_id":"call_old","output":"passed"}]}`
	attrs, downstream := captureExchange(t, content.Responses, "/v1/responses", body, "sk-app-test", 42, func(tracer trace.Tracer) http.Handler {
		return Responses(rtr, nil, nil, nil, nil, config.GeneralSettings{}, tracer)
	})
	if !strings.Contains(downstream, `"call_id":"call_1"`) {
		t.Fatalf("tool call metadata missing from downstream stream: %s", downstream)
	}
	if attrs["llmproxy.capture.previous_response_id"] != "resp_old" || attrs["gen_ai.response.id"] != "resp_new" ||
		attrs["llmproxy.capture.output_partial"] != true ||
		!strings.Contains(valString(attrs["gen_ai.output.messages"]), `"arguments":{"cmd":"go test"}`) ||
		!strings.Contains(valString(attrs["gen_ai.input.messages"]), `"id":"call_old","response":"passed"`) {
		t.Fatalf("bad Responses capture: %v", attrs)
	}
}

func TestChatStructuredCaptureMarksInterruptedStream(t *testing.T) {
	resetTestMockState()
	testMockProviderState.streamChunks = []*model.StreamChunk{{Choices: []model.Choice{{Delta: &model.Delta{Content: "partial text"}}}}}
	testMockProviderState.streamErr = errors.New("upstream stream interrupted")
	rtr, err := router.New(newTestMockConfig("gpt-4"), nil)
	if err != nil {
		t.Fatal(err)
	}
	attrs, _ := captureExchange(t, content.Chat, "/v1/chat/completions", makeChatRequest("gpt-4", true), "sk-app-test", 42, func(tracer trace.Tracer) http.Handler {
		return ChatCompletions(rtr, nil, nil, nil, nil, config.GeneralSettings{}, tracer)
	})
	if attrs["llmproxy.capture.output_partial"] != true ||
		!strings.Contains(valString(attrs["gen_ai.output.messages"]), "partial text") {
		t.Fatalf("expected partial content: %v", attrs)
	}
}
