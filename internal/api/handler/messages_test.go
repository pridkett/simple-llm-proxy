package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
)

type disconnectedMessagesWriter struct{ header http.Header }

func (w *disconnectedMessagesWriter) Header() http.Header     { return w.header }
func (*disconnectedMessagesWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (*disconnectedMessagesWriter) WriteHeader(int)           {}
func (*disconnectedMessagesWriter) Flush()                    {}

func TestMessagesStreamingErrorsAndProviderHealth(t *testing.T) {
	text := &model.StreamChunk{Choices: []model.Choice{{Delta: &model.Delta{Content: "partial"}}}}
	finished := &model.StreamChunk{Choices: []model.Choice{{FinishReason: "stop"}}}
	badTool := &model.StreamChunk{Choices: []model.Choice{{Delta: &model.Delta{ToolCalls: []model.ToolCall{{ID: "tool-a", Function: model.FunctionCall{Name: "lookup", Arguments: `{"incomplete":`}}}}, FinishReason: "tool_calls"}}}
	for _, test := range []struct {
		name                            string
		chunks                          []*model.StreamChunk
		err                             error
		disconnect, healthy, errorEvent bool
	}{
		{"success", []*model.StreamChunk{text, finished}, nil, false, true, false},
		{"upstream failure", []*model.StreamChunk{text}, errors.New("broken upstream"), false, false, true},
		{"failure after finish", []*model.StreamChunk{text, finished}, io.ErrUnexpectedEOF, false, false, true},
		{"no finish reason", []*model.StreamChunk{text}, nil, false, false, true},
		{"invalid tool JSON", []*model.StreamChunk{badTool}, nil, false, false, true},
		{"cancelled", []*model.StreamChunk{text}, context.Canceled, false, true, false},
		{"client write failure", []*model.StreamChunk{text}, nil, true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetTestMockState()
			testMockProviderState.streamChunks = test.chunks
			testMockProviderState.streamErr = test.err
			cfg := newTestMockConfig("alias")
			cfg.RouterSettings.AllowedFails = 1
			r, err := router.New(cfg, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			handler := Messages(r, nil, nil, nil, nil, cfg.GeneralSettings)
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"alias","max_tokens":20,"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
			rr := httptest.NewRecorder()
			if test.disconnect {
				handler.ServeHTTP(&disconnectedMessagesWriter{header: make(http.Header)}, req)
			} else {
				handler.ServeHTTP(rr, req)
			}
			if strings.Contains(rr.Body.String(), "event: error") != test.errorEvent {
				t.Fatal(rr.Body.String())
			}
			if test.errorEvent && strings.Contains(rr.Body.String(), "event: message_stop") {
				t.Fatal("failure marked as complete")
			}
			if test.name == "success" && !strings.Contains(rr.Body.String(), "event: message_stop") {
				t.Fatal("successful stream not completed")
			}
			status := r.GetStatus()[0]
			if (status.HealthyDeployments == 1) != test.healthy {
				t.Fatalf("provider health: %+v", status)
			}
		})
	}
}

func TestMessagesToolResponseAndMalformedUpstream(t *testing.T) {
	for _, arguments := range []string{`{"city":"NYC"}`, `not JSON`} {
		resetTestMockState()
		testMockProviderState.chatResponse = &model.ChatCompletionResponse{Choices: []model.Choice{{Message: &model.Message{ToolCalls: []model.ToolCall{{ID: "call-a", Function: model.FunctionCall{Name: "lookup", Arguments: arguments}}}}, FinishReason: "tool_calls"}}}
		cfg := newTestMockConfig("alias")
		r, err := router.New(cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		rr := httptest.NewRecorder()
		Messages(r, nil, nil, nil, nil, cfg.GeneralSettings).ServeHTTP(rr, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"alias","max_tokens":20,"messages":[{"role":"user","content":"hi"}]}`)))
		r.Close()
		if arguments == `not JSON` {
			if rr.Code != 502 || !strings.Contains(rr.Body.String(), `"type":"error"`) {
				t.Fatal(rr.Code, rr.Body.String())
			}
		} else if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"type":"tool_use"`) || !strings.Contains(rr.Body.String(), `"input":{"city":"NYC"}`) {
			t.Fatal(rr.Code, rr.Body.String())
		}
	}
}
