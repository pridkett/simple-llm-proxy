package responsesbridge

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// fakeResponses is a provider whose chat endpoint must never be used.
type fakeResponses struct {
	fakeChat
	got    *model.ResponsesRequest
	resp   *model.ResponsesResponse
	events []*model.ResponsesStreamEvent
}

func (f *fakeResponses) ChatCompletion(context.Context, *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	panic("chat endpoint called for a Responses-only model")
}
func (f *fakeResponses) CreateResponse(_ context.Context, req *model.ResponsesRequest) (*model.ResponsesResponse, error) {
	f.got = req
	return f.resp, nil
}
func (f *fakeResponses) CreateResponseStream(_ context.Context, req *model.ResponsesRequest) (provider.ResponsesStream, error) {
	f.got = req
	return &eventSlice{events: f.events}, nil
}
func (f *fakeResponses) GetResponse(context.Context, string) (*model.ResponsesResponse, error) {
	return nil, nil
}
func (f *fakeResponses) CancelResponse(context.Context, string) (*model.ResponsesResponse, error) {
	return nil, nil
}

type eventSlice struct{ events []*model.ResponsesStreamEvent }

func (s *eventSlice) Recv() (*model.ResponsesStreamEvent, error) {
	if len(s.events) == 0 {
		return nil, io.EOF
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event, nil
}
func (s *eventSlice) Close() error { return nil }

func toChat(t *testing.T, f *fakeResponses) provider.Provider {
	t.Helper()
	p, err := ToChat(f)
	if err != nil {
		t.Fatalf("ToChat: %v", err)
	}
	return p
}

func TestToChatRequiresResponsesProvider(t *testing.T) {
	if _, err := ToChat(&fakeChat{}); err == nil {
		t.Fatal("a chat-only provider cannot back a responses/ deployment")
	}
}

func TestToChatKeepsNativeResponsesPassthrough(t *testing.T) {
	if _, ok := toChat(t, &fakeResponses{}).(provider.ResponsesProvider); !ok {
		t.Fatal("wrapped provider must still serve /v1/responses natively")
	}
}

func TestChatCompletionIsSentThroughResponses(t *testing.T) {
	fake := &fakeResponses{resp: decode[model.ResponsesResponse](t, `{
		"id": "resp_1", "object": "response", "created_at": 42, "model": "gpt-5-pro", "status": "completed",
		"output": [
			{"type": "reasoning", "id": "rs_1", "summary": []},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Sure.", "annotations": []}]},
			{"type": "function_call", "id": "fc_1", "call_id": "call_7", "name": "lookup", "arguments": "{\"q\":1}"}
		],
		"usage": {"input_tokens": 20, "output_tokens": 9, "total_tokens": 29}
	}`)}
	chat := decode[model.ChatCompletionRequest](t, `{
		"model": "gpt-5-pro",
		"max_completion_tokens": 100,
		"reasoning_effort": "high",
		"response_format": {"type": "json_schema", "json_schema": {"name": "answer", "schema": {"type": "object"}}},
		"tools": [{"type": "function", "function": {"name": "lookup", "description": "Find it", "parameters": {"type": "object"}}}],
		"tool_choice": {"type": "function", "function": {"name": "lookup"}},
		"messages": [
			{"role": "system", "content": "Be brief."},
			{"role": "user", "content": [{"type": "text", "text": "Look"}, {"type": "image_url", "image_url": {"url": "data:image/png;base64,AAAA"}}]},
			{"role": "assistant", "content": "Checking.", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "lookup", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "found"}
		]
	}`)
	resp, err := toChat(t, fake).ChatCompletion(context.Background(), chat)
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	gotReq := asMap(t, fake.got)
	wantReq := asMap(t, json.RawMessage(`{
		"model": "gpt-5-pro",
		"store": false,
		"max_output_tokens": 100,
		"reasoning": {"effort": "high"},
		"text": {"format": {"type": "json_schema", "name": "answer", "schema": {"type": "object"}}},
		"tools": [{"type": "function", "name": "lookup", "description": "Find it", "parameters": {"type": "object"}}],
		"tool_choice": {"type": "function", "name": "lookup"},
		"input": [
			{"role": "system", "content": "Be brief."},
			{"role": "user", "content": [{"type": "input_text", "text": "Look"}, {"type": "input_image", "image_url": "data:image/png;base64,AAAA"}]},
			{"role": "assistant", "content": "Checking."},
			{"type": "function_call", "call_id": "call_1", "name": "lookup", "arguments": "{}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "found"}
		]
	}`))
	if !reflect.DeepEqual(gotReq, wantReq) {
		gotJSON, _ := json.MarshalIndent(gotReq, "", " ")
		wantJSON, _ := json.MarshalIndent(wantReq, "", " ")
		t.Fatalf("upstream request mismatch\n got: %s\nwant: %s", gotJSON, wantJSON)
	}

	got := asMap(t, resp)
	want := asMap(t, json.RawMessage(`{
		"id": "resp_1", "object": "chat.completion", "created": 42, "model": "gpt-5-pro",
		"choices": [{"index": 0, "finish_reason": "tool_calls", "message": {"role": "assistant", "content": "Sure.",
			"tool_calls": [{"id": "call_7", "type": "function", "function": {"name": "lookup", "arguments": "{\"q\":1}"}}]}}],
		"usage": {"prompt_tokens": 20, "completion_tokens": 9, "total_tokens": 29}
	}`))
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", " ")
		t.Fatalf("chat response mismatch\n got: %s", gotJSON)
	}
}

func TestChatCompletionFinishReasons(t *testing.T) {
	cases := map[string]struct{ body, finish string }{
		"truncated": {`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}`, "length"},
		"filtered":  {`{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[]}`, "content_filter"},
		"plain":     {`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`, "stop"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeResponses{resp: decode[model.ResponsesResponse](t, tc.body)}
			resp, err := toChat(t, fake).ChatCompletion(context.Background(), decode[model.ChatCompletionRequest](t, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
			if err != nil {
				t.Fatalf("ChatCompletion: %v", err)
			}
			if got := resp.Choices[0].FinishReason; got != tc.finish {
				t.Fatalf("finish_reason = %q, want %q", got, tc.finish)
			}
		})
	}
}

func TestChatCompletionSurfacesFailedResponse(t *testing.T) {
	fake := &fakeResponses{resp: decode[model.ResponsesResponse](t, `{"status":"failed","error":{"message":"boom","type":"server_error"}}`)}
	_, err := toChat(t, fake).ChatCompletion(context.Background(), decode[model.ChatCompletionRequest](t, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	if err == nil || provider.IsInvalidRequest(err) {
		t.Fatalf("a failed upstream response must be a provider error, got %v", err)
	}
}

func TestResponsesRequestRejectsWhatResponsesCannotExpress(t *testing.T) {
	cases := map[string]string{
		"n":         `{"model":"m","n":2,"messages":[{"role":"user","content":"hi"}]}`,
		"stop":      `{"model":"m","stop":["END"],"messages":[{"role":"user","content":"hi"}]}`,
		"functions": `{"model":"m","functions":[{"name":"f"}],"messages":[{"role":"user","content":"hi"}]}`,
		"bad role":  `{"model":"m","messages":[{"role":"function","content":"hi"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := responsesRequest(decode[model.ChatCompletionRequest](t, body))
			if !provider.IsInvalidRequest(err) {
				t.Fatalf("want an invalid-request error, got %v", err)
			}
		})
	}
}

func TestChatStreamFromResponsesEvents(t *testing.T) {
	fake := &fakeResponses{events: []*model.ResponsesStreamEvent{
		decode[model.ResponsesStreamEvent](t, `{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","model":"gpt-5-pro","created_at":42,"status":"in_progress","output":[]}}`),
		decode[model.ResponsesStreamEvent](t, `{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1"}}`),
		decode[model.ResponsesStreamEvent](t, `{"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"Hi"}`),
		decode[model.ResponsesStreamEvent](t, `{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_7","name":"lookup","arguments":""}}`),
		decode[model.ResponsesStreamEvent](t, `{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"q\":"}`),
		decode[model.ResponsesStreamEvent](t, `{"type":"response.function_call_arguments.delta","output_index":2,"delta":"1}"}`),
		decode[model.ResponsesStreamEvent](t, `{"type":"response.function_call_arguments.done","output_index":2,"arguments":"{\"q\":1}"}`),
		decode[model.ResponsesStreamEvent](t, `{"type":"response.output_item.added","output_index":3,"item":{"type":"function_call","id":"fc_2","call_id":"call_8","name":"ping","arguments":""}}`),
		// No deltas for this call: the arguments arrive whole.
		decode[model.ResponsesStreamEvent](t, `{"type":"response.function_call_arguments.done","output_index":3,"arguments":"{}"}`),
		decode[model.ResponsesStreamEvent](t, `{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":20,"output_tokens":9,"total_tokens":29}}}`),
	}}
	stream, err := toChat(t, fake).ChatCompletionStream(context.Background(), decode[model.ChatCompletionRequest](t, `{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("ChatCompletionStream: %v", err)
	}
	var got []any
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if chunk.ID != "resp_1" || chunk.Model != "gpt-5-pro" || chunk.Object != "chat.completion.chunk" {
			t.Fatalf("unexpected chunk envelope: %+v", chunk)
		}
		wire := asMap(t, chunk)
		if usage, ok := wire["usage"]; ok {
			got = append(got, usage)
			continue
		}
		choice := wire["choices"].([]any)[0].(map[string]any)
		delete(choice, "index")
		got = append(got, choice)
	}
	var want []any
	if err := json.Unmarshal([]byte(`[
		{"delta": {"role": "assistant"}},
		{"delta": {"content": "Hi"}},
		{"delta": {"tool_calls": [{"index": 0, "id": "call_7", "type": "function", "function": {"name": "lookup"}}]}},
		{"delta": {"tool_calls": [{"index": 0, "function": {"arguments": "{\"q\":"}}]}},
		{"delta": {"tool_calls": [{"index": 0, "function": {"arguments": "1}"}}]}},
		{"delta": {"tool_calls": [{"index": 1, "id": "call_8", "type": "function", "function": {"name": "ping"}}]}},
		{"delta": {"tool_calls": [{"index": 1, "function": {"arguments": "{}"}}]}},
		{"delta": {}, "finish_reason": "tool_calls"},
		{"prompt_tokens": 20, "completion_tokens": 9, "total_tokens": 29}
	]`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", " ")
		t.Fatalf("chunk sequence mismatch\n got: %s", gotJSON)
	}
}

func TestChatStreamFailsWhenUpstreamStopsEarly(t *testing.T) {
	cases := map[string][]*model.ResponsesStreamEvent{
		"truncated": {decode[model.ResponsesStreamEvent](t, `{"type":"response.output_text.delta","delta":"partial"}`)},
		"failed":    {decode[model.ResponsesStreamEvent](t, `{"type":"response.failed","response":{"status":"failed","error":{"message":"boom","type":"server_error"}}}`)},
		"error":     {decode[model.ResponsesStreamEvent](t, `{"type":"error","message":"boom"}`)},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			stream, err := toChat(t, &fakeResponses{events: events}).ChatCompletionStream(context.Background(), decode[model.ChatCompletionRequest](t, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
			if err != nil {
				t.Fatalf("ChatCompletionStream: %v", err)
			}
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					t.Fatal("stream must not end cleanly")
				}
				if err != nil {
					return
				}
			}
		})
	}
}
