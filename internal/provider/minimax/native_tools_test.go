package minimax

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// The hosted Coding Plan API returns native tool_calls and reasoning fields.
// Exercise the full assistant/tool-result round trip rather than the legacy XML
// parser, including indexed argument fragments used by streamed tool calls.
func TestNativeToolRoundTripPreservesReasoning(t *testing.T) {
	for _, tc := range []struct {
		name      string
		streaming bool
		done      bool
	}{{"sync", false, false}, {"stream-sentinel", true, true}, {"stream-finish-EOF", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			const arguments = `{"numbers":[2,3,5],"enabled":true}`
			const reasoning = "Use the calculator."
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("wrong endpoint or authentication")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests++
				if body["reasoning_split"] != true {
					t.Error("reasoning_split request extension was lost")
				}
				if requests == 2 {
					messages := body["messages"].([]any)
					assistant := messages[1].(map[string]any)
					if assistant["reasoning_content"] != reasoning {
						t.Errorf("assistant reasoning lost: %v", assistant)
					}
					call := assistant["tool_calls"].([]any)[0].(map[string]any)
					if call["id"] != "call_native" || call["function"].(map[string]any)["arguments"] != arguments {
						t.Errorf("native arguments or ID changed: %v", call)
					}
					result := messages[2].(map[string]any)
					if result["role"] != "tool" || result["tool_call_id"] != "call_native" || result["content"] != "10" {
						t.Errorf("tool result association lost: %v", result)
					}
					io.WriteString(w, `{"model":"MiniMax-M2.7","choices":[{"index":0,"message":{"role":"assistant","content":"TOTAL=10"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}}`)
					return
				}
				if !tc.streaming {
					io.WriteString(w, `{"model":"MiniMax-M2.7","choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":"Use the calculator.","reasoning_details":[{"type":"reasoning.text","text":"Use the calculator.","index":0}],"tool_calls":[{"id":"call_native","type":"function","function":{"name":"sum_numbers","arguments":"{\"numbers\":[2,3,5],\"enabled\":true}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30,"completion_tokens_details":{"reasoning_tokens":12}}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, event := range []string{
					`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Use the "}}]}`,
					`{"choices":[{"index":0,"delta":{"reasoning_content":"calculator."}}]}`,
					`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_native","type":"function","function":{"name":"sum_numbers","arguments":"{\"numbers\":[2,"}}]}}]}`,
					`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"3,5],\"enabled\":true}"}}]},"finish_reason":"tool_calls"}]}`,
					`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30,"completion_tokens_details":{"reasoning_tokens":12}}}`,
				} {
					fmt.Fprintf(w, "data: %s\n\n", event)
				}
				if tc.done {
					io.WriteString(w, "data: [DONE]\n\n")
				}
			}))
			defer server.Close()
			p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL})
			var req model.ChatCompletionRequest
			if err := json.Unmarshal([]byte(`{"model":"MiniMax-M2.7","reasoning_split":true,"messages":[{"role":"user","content":"Sum 2, 3, and 5."}]}`), &req); err != nil {
				t.Fatal(err)
			}
			var assistant model.Message
			if !tc.streaming {
				resp, err := p.ChatCompletion(context.Background(), &req)
				if err != nil {
					t.Fatal(err)
				}
				assistant = *resp.Choices[0].Message
				encoded, _ := json.Marshal(resp)
				if !strings.Contains(string(encoded), `"reasoning_tokens":12`) || !strings.Contains(string(encoded), `"reasoning_details"`) {
					t.Fatalf("reasoning or usage extension lost: %s", encoded)
				}
			} else {
				req.Stream = true
				stream, err := p.ChatCompletionStream(context.Background(), &req)
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				var reasoningParts, argumentParts strings.Builder
				var call model.ToolCall
				usageSeen, finished := false, false
				for {
					chunk, err := stream.Recv()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if chunk.Usage != nil {
						encoded, _ := json.Marshal(chunk.Usage)
						usageSeen = strings.Contains(string(encoded), `"reasoning_tokens":12`)
					}
					for _, choice := range chunk.Choices {
						finished = finished || choice.FinishReason == "tool_calls"
						encoded, _ := json.Marshal(choice.Delta)
						var delta map[string]any
						json.Unmarshal(encoded, &delta)
						if text, ok := delta["reasoning_content"].(string); ok {
							reasoningParts.WriteString(text)
						}
						for _, fragment := range choice.Delta.ToolCalls {
							if fragment.Index == nil || *fragment.Index != 0 {
								t.Fatal("streamed tool index lost")
							}
							if fragment.ID != "" {
								call = fragment
							}
							argumentParts.WriteString(fragment.Function.Arguments)
						}
					}
				}
				if !usageSeen || !finished {
					t.Fatal("missing streamed usage or tool finish reason")
				}
				call.Index = nil
				call.Function.Arguments = argumentParts.String()
				encoded, _ := json.Marshal(map[string]any{"role": "assistant", "content": nil, "reasoning_content": reasoningParts.String(), "tool_calls": []model.ToolCall{call}})
				json.Unmarshal(encoded, &assistant)
			}
			req.Stream = false
			req.Messages = append(req.Messages, assistant, model.Message{Role: "tool", Content: "10", ToolCallID: "call_native"})
			resp, err := p.ChatCompletion(context.Background(), &req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Choices[0].Message.Content != "TOTAL=10" || requests != 2 {
				t.Fatal("native tool conversation did not complete")
			}
		})
	}
}

func TestMiniMaxStreamRejectsEOFWithoutFinish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Partial reply\"}}]}\n\n")
	}))
	defer server.Close()
	p := New(provider.ProviderOptions{APIKey: "test-key", APIBase: server.URL})
	stream, err := p.ChatCompletionStream(context.Background(), &model.ChatCompletionRequest{
		Model: "MiniMax-M2.7", Messages: []model.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != io.ErrUnexpectedEOF {
		t.Fatalf("incomplete MiniMax stream: got %v, want unexpected EOF", err)
	}
}
