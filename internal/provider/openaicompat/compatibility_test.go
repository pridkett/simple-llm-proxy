package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompatibleStreamingFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `data:{"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"f","arguments":"{}"}}],"reasoning_content":"thought"},"finish_reason":null}]}`+"\n\ndata:[DONE]\n\n")
	}))
	defer server.Close()
	stream, err := newTestProvider(server.URL).ChatCompletionStream(context.Background(), simpleRequest())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	chunk, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(chunk)
	if !strings.Contains(string(data), `"index":0`) || !strings.Contains(string(data), `"reasoning_content":"thought"`) {
		t.Fatal(string(data))
	}
	if chunk.Choices[0].Delta.ToolCalls[0].Index == nil {
		t.Fatal("tool index lost")
	}
}
func TestTruncatedStreamIsAnError(t *testing.T) {
	for _, body := range []string{"", "event:error\ndata:{\"message\":\"bad\"}\n\n", "data: {\"error\":{\"message\":\"bad\"}}\n\n"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		stream, err := newTestProvider(server.URL).ChatCompletionStream(context.Background(), &model.ChatCompletionRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = stream.Recv(); err == nil || err == io.EOF {
			t.Errorf("lost error: %v", err)
		}
		stream.Close()
		server.Close()
	}
}

func TestStreamingAlwaysRequestsUsageWithoutMutatingCaller(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		options, ok := req["stream_options"].(map[string]any)
		if !ok || options["include_usage"] != true || options["vendor"] != float64(1) {
			t.Errorf("missing usage or lost options: %v", req)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	var req model.ChatCompletionRequest
	json.Unmarshal([]byte(`{"model":"test","stream_options":{"include_usage":false,"vendor":1}}`), &req)
	stream, err := newTestProvider(server.URL).ChatCompletionStream(context.Background(), &req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatal(err)
	}
	if req.StreamOptions.IncludeUsage {
		t.Fatal("caller options mutated")
	}
}
