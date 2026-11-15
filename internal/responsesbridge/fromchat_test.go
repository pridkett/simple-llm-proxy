package responsesbridge

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// fakeChat is a chat-only provider that records the translated request.
type fakeChat struct {
	got    *model.ChatCompletionRequest
	resp   *model.ChatCompletionResponse
	chunks []*model.StreamChunk
}

func (f *fakeChat) Name() string { return "fake" }
func (f *fakeChat) ChatCompletion(_ context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	f.got = req
	return f.resp, nil
}
func (f *fakeChat) ChatCompletionStream(_ context.Context, req *model.ChatCompletionRequest) (provider.Stream, error) {
	f.got = req
	return &sliceStream{chunks: f.chunks}, nil
}
func (f *fakeChat) Embeddings(context.Context, *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, nil
}
func (f *fakeChat) SupportsEmbeddings() bool { return false }

type sliceStream struct{ chunks []*model.StreamChunk }

func (s *sliceStream) Recv() (*model.StreamChunk, error) {
	if len(s.chunks) == 0 {
		return nil, io.EOF
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, nil
}
func (s *sliceStream) Close() error { return nil }

func decode[T any](t *testing.T, raw string) *T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	return &out
}

// asMap renders any value the way a client would see it on the wire.
func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestChatRequestTranslatesConversationAndTools(t *testing.T) {
	req := decode[model.ResponsesRequest](t, `{
		"model": "claude",
		"instructions": "Be brief.",
		"max_output_tokens": 64,
		"reasoning": {"effort": "low"},
		"text": {"format": {"type": "json_schema", "name": "answer", "strict": true, "schema": {"type": "object"}}},
		"tools": [{"type": "function", "name": "lookup", "description": "Find it", "parameters": {"type": "object"}, "strict": true}],
		"tool_choice": {"type": "function", "name": "lookup"},
		"input": [
			{"role": "developer", "content": "Use tools."},
			{"type": "message", "role": "user", "content": [
				{"type": "input_text", "text": "What is this?"},
				{"type": "input_image", "image_url": "https://example.test/a.png", "detail": "low"}
			]},
			{"type": "reasoning", "id": "rs_1", "summary": []},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Checking."}]},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": "{\"q\":1}"},
			{"type": "function_call", "call_id": "call_2", "name": "lookup", "arguments": "{\"q\":2}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "one"},
			{"type": "function_call_output", "call_id": "call_2", "output": [{"type": "input_text", "text": "two"}]}
		]
	}`)
	chat, _, err := chatRequest(req)
	if err != nil {
		t.Fatalf("chatRequest: %v", err)
	}
	got := asMap(t, chat)
	want := asMap(t, json.RawMessage(`{
		"model": "claude",
		"max_tokens": 64,
		"reasoning_effort": "low",
		"response_format": {"type": "json_schema", "json_schema": {"name": "answer", "strict": true, "schema": {"type": "object"}}},
		"tools": [{"type": "function", "function": {"name": "lookup", "description": "Find it", "parameters": {"type": "object"}, "strict": true}}],
		"tool_choice": {"type": "function", "function": {"name": "lookup"}},
		"messages": [
			{"role": "system", "content": "Be brief."},
			{"role": "system", "content": "Use tools."},
			{"role": "user", "content": [
				{"type": "text", "text": "What is this?"},
				{"type": "image_url", "image_url": {"url": "https://example.test/a.png", "detail": "low"}}
			]},
			{"role": "assistant", "content": "Checking.", "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "lookup", "arguments": "{\"q\":1}"}},
				{"id": "call_2", "type": "function", "function": {"name": "lookup", "arguments": "{\"q\":2}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": "one"},
			{"role": "tool", "tool_call_id": "call_2", "content": "two"}
		]
	}`))
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", " ")
		wantJSON, _ := json.MarshalIndent(want, "", " ")
		t.Fatalf("chat request mismatch\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func TestChatRequestRejectsWhatChatCannotExpress(t *testing.T) {
	cases := map[string]string{
		"background":           `{"model":"m","input":"hi","background":true}`,
		"previous_response_id": `{"model":"m","input":"hi","previous_response_id":"resp_1"}`,
		"conversation":         `{"model":"m","input":"hi","conversation":"conv_1"}`,
		"built-in tool":        `{"model":"m","input":"hi","tools":[{"type":"web_search"}]}`,
		"item reference":       `{"model":"m","input":[{"type":"item_reference","id":"msg_1"}]}`,
		"file input":           `{"model":"m","input":[{"role":"user","content":[{"type":"input_file","file_id":"f"}]}]}`,
		"unknown tool choice":  `{"model":"m","input":"hi","tool_choice":{"type":"function","name":"missing"}}`,
		"empty input":          `{"model":"m","input":[]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := chatRequest(decode[model.ResponsesRequest](t, body))
			if err == nil {
				t.Fatal("expected an error")
			}
			// A 400 must not count against the deployment's health.
			if !provider.IsInvalidRequest(err) {
				t.Fatalf("error is not an invalid-request error: %v", err)
			}
		})
	}
}

func TestCreateResponseTranslatesTextToolsAndUsage(t *testing.T) {
	fake := &fakeChat{resp: decode[model.ChatCompletionResponse](t, `{
		"id": "chatcmpl-1", "object": "chat.completion", "model": "claude-real",
		"choices": [{"index": 0, "finish_reason": "tool_calls", "message": {"role": "assistant", "content": "Looking.",
			"tool_calls": [{"id": "call_9", "type": "function", "function": {"name": "lookup", "arguments": "{\"q\":1}"}}]}}],
		"usage": {"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18}
	}`)}
	resp, err := FromChat(fake).CreateResponse(context.Background(), decode[model.ResponsesRequest](t, `{"model":"claude","input":"hi","metadata":{"k":"v"}}`))
	if err != nil {
		t.Fatalf("CreateResponse: %v", err)
	}
	if fake.got.Stream {
		t.Error("non-streaming call sent stream=true upstream")
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 11 || resp.Usage.CompletionTokens != 7 {
		t.Fatalf("typed usage = %+v, want 11/7 for cost accounting", resp.Usage)
	}
	got := asMap(t, resp)
	if !strings.HasPrefix(got["id"].(string), "resp_") || got["object"] != "response" || got["status"] != "completed" || got["model"] != "claude-real" {
		t.Fatalf("unexpected envelope: %v", got)
	}
	usage := got["usage"].(map[string]any)
	if usage["input_tokens"] != 11.0 || usage["output_tokens"] != 7.0 || usage["total_tokens"] != 18.0 || usage["prompt_tokens"] != nil {
		t.Fatalf("usage must use Responses names, got %v", usage)
	}
	output := got["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("output items = %d, want message + function_call", len(output))
	}
	message := output[0].(map[string]any)
	part := message["content"].([]any)[0].(map[string]any)
	if message["type"] != "message" || part["type"] != "output_text" || part["text"] != "Looking." || part["annotations"] == nil {
		t.Fatalf("unexpected message item: %v", message)
	}
	call := output[1].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_9" || call["name"] != "lookup" || call["arguments"] != `{"q":1}` {
		t.Fatalf("unexpected function_call item: %v", call)
	}
	if got["metadata"].(map[string]any)["k"] != "v" {
		t.Fatalf("metadata not echoed: %v", got["metadata"])
	}
}

func TestCreateResponseMarksTruncationIncomplete(t *testing.T) {
	fake := &fakeChat{resp: decode[model.ChatCompletionResponse](t, `{
		"choices": [{"index": 0, "finish_reason": "length", "message": {"role": "assistant", "content": "cut"}}]
	}`)}
	resp, err := FromChat(fake).CreateResponse(context.Background(), decode[model.ResponsesRequest](t, `{"model":"m","input":"hi"}`))
	if err != nil {
		t.Fatalf("CreateResponse: %v", err)
	}
	got := asMap(t, resp)
	if got["status"] != "incomplete" || got["incomplete_details"].(map[string]any)["reason"] != "max_output_tokens" {
		t.Fatalf("status = %v, details = %v", got["status"], got["incomplete_details"])
	}
}

func TestStreamEmitsResponsesEventSequence(t *testing.T) {
	fake := &fakeChat{chunks: []*model.StreamChunk{
		decode[model.StreamChunk](t, `{"model":"claude-real","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`),
		decode[model.StreamChunk](t, `{"choices":[{"index":0,"delta":{"content":"lo"}}]}`),
		decode[model.StreamChunk](t, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\""}}]}}]}`),
		decode[model.StreamChunk](t, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":1}"}}]}}]}`),
		// A call with no arguments never sends an argument delta.
		decode[model.StreamChunk](t, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"ping"}}]}}]}`),
		decode[model.StreamChunk](t, `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`),
		decode[model.StreamChunk](t, `{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`),
	}}
	stream, err := FromChat(fake).CreateResponseStream(context.Background(), decode[model.ResponsesRequest](t, `{"model":"claude","input":"hi","stream":true}`))
	if err != nil {
		t.Fatalf("CreateResponseStream: %v", err)
	}
	if !fake.got.Stream || fake.got.StreamOptions == nil || !fake.got.StreamOptions.IncludeUsage {
		t.Fatal("upstream chat stream must be asked for usage")
	}

	var types []string
	var events []map[string]any
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		wire := asMap(t, event)
		if wire["sequence_number"] != float64(len(events)) {
			t.Fatalf("event %d has sequence_number %v", len(events), wire["sequence_number"])
		}
		types = append(types, event.Type)
		events = append(events, wire)
	}
	want := []string{
		"response.created", "response.in_progress",
		"response.output_item.added", "response.content_part.added",
		"response.output_text.delta", "response.output_text.delta",
		"response.output_text.done", "response.content_part.done", "response.output_item.done",
		"response.output_item.added",
		"response.function_call_arguments.delta", "response.function_call_arguments.delta",
		"response.function_call_arguments.done", "response.output_item.done",
		"response.output_item.added", "response.function_call_arguments.done", "response.output_item.done",
		"response.completed",
	}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("event sequence\n got: %v\nwant: %v", types, want)
	}
	if created := events[0]["response"].(map[string]any); created["status"] != "in_progress" || created["output"] == nil {
		t.Fatalf("response.created must carry an in-progress response with an output array: %v", created)
	}
	if events[6]["text"] != "Hello" {
		t.Fatalf("output_text.done text = %v", events[6]["text"])
	}
	final := events[len(events)-1]["response"].(map[string]any)
	output := final["output"].([]any)
	if final["status"] != "completed" || final["model"] != "claude-real" || len(output) != 3 {
		t.Fatalf("unexpected final response: %v", final)
	}
	if first := output[1].(map[string]any); first["arguments"] != `{"q":1}` || first["call_id"] != "call_1" {
		t.Fatalf("unexpected first call: %v", first)
	}
	if second := output[2].(map[string]any); second["arguments"] != "{}" || second["name"] != "ping" {
		t.Fatalf("unexpected second call: %v", second)
	}
	if usage := final["usage"].(map[string]any); usage["input_tokens"] != 5.0 || usage["output_tokens"] != 3.0 {
		t.Fatalf("unexpected usage: %v", usage)
	}
}

func TestStreamWithoutFinishReasonFails(t *testing.T) {
	fake := &fakeChat{chunks: []*model.StreamChunk{
		decode[model.StreamChunk](t, `{"choices":[{"index":0,"delta":{"content":"partial"}}]}`),
	}}
	stream, err := FromChat(fake).CreateResponseStream(context.Background(), decode[model.ResponsesRequest](t, `{"model":"m","input":"hi"}`))
	if err != nil {
		t.Fatalf("CreateResponseStream: %v", err)
	}
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			t.Fatal("a truncated upstream stream must not look complete")
		}
		if err != nil {
			return
		}
	}
}
