package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestJSONCompatibilityRoundTrips(t *testing.T) {
	cases := []struct {
		name, input string
		value       any
	}{
		{"chat", `{"model":"alias","messages":[{"role":"assistant","content":null,"refusal":"no","tool_calls":[{"id":"call","type":"function","function":{"name":"f","arguments":"{}","vendor":true}}]}],"max_completion_tokens":7,"reasoning_effort":"low","parallel_tool_calls":false,"tools":[{"type":"function","function":{"name":"f","parameters":{},"strict":true}}],"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{},"strict":true}},"stream_options":{"include_usage":true,"vendor":2},"stop":"END","vendor":{"nested":9007199254740993}}`, &ChatCompletionRequest{}},
		{"response", `{"id":"c","object":"chat.completion","created":1,"model":"m","service_tier":"default","choices":[{"index":0,"message":{"role":"assistant","content":"hi","annotations":[1]},"finish_reason":"stop","logprobs":{"content":[]}}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13,"prompt_tokens_details":{"cached_tokens":2}}}`, &ChatCompletionResponse{}},
		{"stream", `{"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}],"reasoning_content":"thinking"},"finish_reason":null}],"vendor":false}`, &StreamChunk{}},
		{"embedding_request", `{"model":"m","input":"hello","dimensions":2,"encoding_format":"base64","vendor":true}`, &EmbeddingsRequest{}},
		{"embedding_response", `{"object":"list","model":"m","data":[{"object":"embedding","index":0,"embedding":"AAAAAA==","vendor":true}],"usage":{"prompt_tokens":1,"total_tokens":1},"vendor":true}`, &EmbeddingsResponse{}},
		{"native_usage", `{"input_tokens":10,"output_tokens":3,"total_tokens":13,"input_tokens_details":{"cached_tokens":2}}`, &Usage{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.input), tc.value); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			var want, got map[string]json.RawMessage
			json.Unmarshal([]byte(tc.input), &want)
			json.Unmarshal(encoded, &got)
			assertFieldsPreserved(t, want, got)
		})
	}
}

func assertFieldsPreserved(t *testing.T, want, got map[string]json.RawMessage) {
	t.Helper()
	for k, v := range want {
		var a, b any
		if err := json.Unmarshal(v, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(got[k], &b); err != nil {
			t.Fatalf("missing field %s: %s", k, got[k])
		}
		// Compare recursively so optional fields added by typed serialization do not
		// hide a lost unknown field inside nested tool and message objects.
		assertJSONSubset(t, a, b, k)
	}
}
func assertJSONSubset(t *testing.T, want, got any, path string) {
	t.Helper()
	switch a := want.(type) {
	case map[string]any:
		b, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("%s: got %v", path, got)
		}
		for k, v := range a {
			assertJSONSubset(t, v, b[k], path+"."+k)
		}
	case []any:
		b, ok := got.([]any)
		if !ok || len(a) != len(b) {
			t.Fatalf("%s: array changed", path)
		}
		for i, v := range a {
			assertJSONSubset(t, v, b[i], path)
		}
	default:
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("%s: want %v got %v", path, want, got)
		}
	}
}

func TestTypedEditsOverrideOriginalJSON(t *testing.T) {
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(`{"model":"alias","messages":[],"stop":"END","stream":false}`), &req); err != nil {
		t.Fatal(err)
	}
	req.Model = "actual"
	req.Stream = true
	req.Stop = []string{"NEW"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.Unmarshal(data, &got)
	if got["model"] != "actual" || got["stream"] != true || got["stop"] != "NEW" {
		t.Fatalf("edits lost: %s", data)
	}
	var usage Usage
	if err := json.Unmarshal([]byte(`{"input_tokens":10,"output_tokens":3,"total_tokens":13}`), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 3 {
		t.Fatal("native usage not normalized")
	}
}

func TestInvalidJSONRejected(t *testing.T) {
	for _, v := range []any{&ChatCompletionRequest{}, &Message{}, &ToolCall{}, &FunctionDef{}, &FunctionCall{}, &ResponseFormat{}, &EmbeddingsRequest{}, &ChatCompletionResponse{}, &Choice{}, &Delta{}, &Usage{}, &EmbeddingsResponse{}, &EmbeddingData{}, &StreamChunk{}, &StreamOptions{}} {
		if err := json.Unmarshal([]byte(`[]`), v); err == nil {
			t.Fatalf("%T accepted array", v)
		}
	}
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(`{"stop":123}`), &req); err == nil {
		t.Fatal("invalid stop accepted")
	}
	var usage Usage
	for _, data := range []string{`{"input_tokens":"bad"}`, `{"output_tokens":"bad"}`} {
		if err := json.Unmarshal([]byte(data), &usage); err == nil {
			t.Fatal("invalid tokens accepted")
		}
	}
}
