package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamParallelToolArguments(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"a","name":"first","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"x\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"1}"}}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"b","name":"second","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		json.NewDecoder(r.Body).Decode(&request)
		if request["max_tokens"] != float64(7) {
			t.Errorf("max_completion_tokens not translated: %v", request)
		}
		for _, event := range events {
			fmt.Fprintf(w, "data:%s\n\n", event)
		}
	}))
	defer server.Close()
	p := New(provider.ProviderOptions{APIBase: server.URL, APIKey: "dummy"})
	max := 7
	stream, err := p.ChatCompletionStream(context.Background(), &model.ChatCompletionRequest{Model: "test", MaxCompletionTokens: &max})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	arguments := map[int]string{}
	names := map[int]string{}
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, choice := range chunk.Choices {
			if choice.Delta == nil {
				continue
			}
			for _, call := range choice.Delta.ToolCalls {
				if call.Index == nil {
					t.Fatal("missing index")
				}
				i := *call.Index
				arguments[i] += call.Function.Arguments
				if call.Function.Name != "" {
					names[i] = call.Function.Name
				}
			}
		}
	}
	if arguments[0] != `{"x":1}` || arguments[1] != "{}" || names[0] != "first" || names[1] != "second" {
		t.Fatalf("lost tool calls: %v %v", arguments, names)
	}
}
func TestStreamErrorsAreNotSuccess(t *testing.T) {
	for _, body := range []string{"", "data: broken\n\n", `data: {"type":"error","error":{"message":"overloaded"}}` + "\n\n"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		p := New(provider.ProviderOptions{APIBase: server.URL})
		stream, err := p.ChatCompletionStream(context.Background(), &model.ChatCompletionRequest{Model: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = stream.Recv(); err == nil || err == io.EOF {
			t.Errorf("stream error lost: %v", err)
		}
		stream.Close()
		server.Close()
	}
}

func TestStructuredOutputAndToolControls(t *testing.T) {
	var req model.ChatCompletionRequest
	data := `{"model":"test","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"},"strict":true}}],"tool_choice":{"type":"function","function":{"name":"lookup"}},"parallel_tool_calls":false,"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object","properties":{}}}}}`
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatal(err)
	}
	p := &Provider{}
	got, err := p.translateRequest(&req)
	if err != nil {
		t.Fatal(err)
	}
	if got.ToolChoice["type"] != "tool" || got.ToolChoice["name"] != "lookup" || got.ToolChoice["disable_parallel_tool_use"] != true {
		t.Fatal(got.ToolChoice)
	}
	if got.Tools[0].Strict == nil || !*got.Tools[0].Strict || got.OutputConfig["format"] == nil {
		t.Fatal("structured settings lost")
	}
	for _, choice := range []string{"auto", "none", "required"} {
		req.ToolChoice = choice
		if _, err := p.translateRequest(&req); err != nil {
			t.Fatal(err)
		}
	}
	for _, choice := range []any{"invalid", map[string]any{"type": "function"}} {
		req.ToolChoice = choice
		if _, err := p.translateRequest(&req); err == nil {
			t.Fatal("invalid tool choice accepted")
		}
	}
	req.ToolChoice = "auto"
	req.ResponseFormat = &model.ResponseFormat{Type: "json_object"}
	if _, err := p.translateRequest(&req); err == nil {
		t.Fatal("unsupported format silently dropped")
	}
}
