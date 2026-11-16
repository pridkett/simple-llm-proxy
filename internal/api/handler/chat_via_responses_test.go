package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
)

// responsesOnlyUpstream stands in for a model OpenAI serves only over the
// Responses API: any other path fails the test.
func responsesOnlyUpstream(t *testing.T, seen *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("upstream path = %s, want /responses", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, seen); err != nil {
			t.Errorf("upstream body: %v", err)
		}
		if (*seen)["stream"] != true {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"resp_1","object":"response","created_at":42,"model":"gpt-5-pro","status":"completed",
				"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"pong","annotations":[]}]}],
				"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"type":"response.created","response":{"id":"resp_1","model":"gpt-5-pro","created_at":42,"status":"in_progress","output":[]}}`,
			`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"po"}`,
			`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"ng"}`,
			`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func responsesOnlyRouter(t *testing.T, apiBase string) *router.Router {
	t.Helper()
	cfg := &config.Config{
		ModelList: []config.ModelConfig{{
			ModelName:     "pro",
			LiteLLMParams: config.LiteLLMParams{Model: "responsestest/responses/gpt-5-pro", APIKey: "k", APIBase: apiBase},
		}},
		RouterSettings: config.RouterSettings{RoutingStrategy: "simple-shuffle", CooldownTime: 30 * time.Second, AllowedFails: 3},
	}
	r, err := router.New(cfg, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return r
}

// ADR 020: an OpenAI chat client reaches a Responses-only model.
func TestChatCompletions_ResponsesOnlyModel(t *testing.T) {
	var seen map[string]any
	r := responsesOnlyRouter(t, responsesOnlyUpstream(t, &seen).URL)

	w := httptest.NewRecorder()
	ChatCompletions(r, nil, nil, nil, nil, config.GeneralSettings{})(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewBufferString(`{"model":"pro","max_tokens":16,"messages":[{"role":"user","content":"ping"}]}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if seen["model"] != "gpt-5-pro" || seen["store"] != false || seen["max_output_tokens"] != 16.0 || seen["messages"] != nil {
		t.Fatalf("upstream did not receive a Responses request: %v", seen)
	}
	var got struct {
		Object  string `json:"object"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage map[string]int `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Object != "chat.completion" || len(got.Choices) != 1 || got.Choices[0].Message.Content != "pong" || got.Choices[0].FinishReason != "stop" {
		t.Fatalf("unexpected chat response: %s", w.Body.String())
	}
	if got.Usage["prompt_tokens"] != 3 || got.Usage["completion_tokens"] != 1 {
		t.Fatalf("usage must use chat names: %s", w.Body.String())
	}
}

// ADR 020: an Anthropic Messages client reaches the same model, streaming.
func TestMessages_ResponsesOnlyModelStreams(t *testing.T) {
	var seen map[string]any
	r := responsesOnlyRouter(t, responsesOnlyUpstream(t, &seen).URL)

	w := httptest.NewRecorder()
	Messages(r, nil, nil, nil, nil, config.GeneralSettings{})(w, httptest.NewRequest(http.MethodPost, "/v1/messages",
		bytes.NewBufferString(`{"model":"pro","max_tokens":16,"stream":true,"system":"be kind","messages":[{"role":"user","content":"ping"}]}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	input, _ := json.Marshal(seen["input"])
	if string(input) != `[{"content":"be kind","role":"system"},{"content":"ping","role":"user"}]` {
		t.Fatalf("upstream input = %s", input)
	}
	body := w.Body.String()
	for _, want := range []string{
		"event: message_start\n",
		`{"text":"po","type":"text_delta"}`,
		`{"text":"ng","type":"text_delta"}`,
		`"stop_reason":"end_turn"`,
		`"input_tokens":3`,
		"event: message_stop\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Messages stream is missing %q\n%s", want, body)
		}
	}
}

// The alias still serves /v1/responses natively, without translation.
func TestResponses_ResponsesOnlyModelPassesThrough(t *testing.T) {
	var seen map[string]any
	r := responsesOnlyRouter(t, responsesOnlyUpstream(t, &seen).URL)

	w := httptest.NewRecorder()
	Responses(r, nil, nil, nil, nil, config.GeneralSettings{})(w, httptest.NewRequest(http.MethodPost, "/v1/responses",
		bytes.NewBufferString(`{"model":"pro","input":"ping","previous_response_id":"resp_0"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if seen["previous_response_id"] != "resp_0" || seen["input"] != "ping" {
		t.Fatalf("native request was altered: %v", seen)
	}
}

func TestResponsesPrefixNeedsAResponsesProvider(t *testing.T) {
	cfg := &config.Config{
		ModelList:      []config.ModelConfig{{ModelName: "bad", LiteLLMParams: config.LiteLLMParams{Model: "chatonlytest/responses/some-model"}}},
		RouterSettings: config.RouterSettings{RoutingStrategy: "simple-shuffle"},
	}
	if _, err := router.New(cfg, nil); err == nil || !strings.Contains(err.Error(), "does not implement the Responses API") {
		t.Fatalf("router.New error = %v, want a Responses API config error", err)
	}
}
