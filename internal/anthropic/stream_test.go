package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

func readEvents(t *testing.T, data string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, event := range strings.Split(strings.TrimSpace(data), "\n\n") {
		lines := strings.Split(event, "\n")
		if len(lines) != 2 {
			t.Fatalf("bad SSE event: %s", event)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &payload); err != nil {
			t.Fatal(err)
		}
		if lines[0] != "event: "+payload["type"].(string) {
			t.Fatalf("event type mismatch: %s", event)
		}
		events = append(events, payload)
	}
	return events
}

func TestStreamTextAndLateUsage(t *testing.T) {
	var out bytes.Buffer
	s := NewStreamEncoder(&out, "alias")
	for _, chunk := range []*model.StreamChunk{
		{Choices: []model.Choice{{Delta: &model.Delta{Role: "assistant"}}}},
		{Choices: []model.Choice{{Delta: &model.Delta{Content: "Hi 🌎"}}}},
		{Choices: []model.Choice{{FinishReason: "length"}}},
		{Usage: &model.Usage{PromptTokens: 12, CompletionTokens: 4}},
	} {
		if err := s.WriteChunk(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(out.String(), "message_stop") || !strings.Contains(out.String(), "Hi 🌎") {
		t.Fatal("stream buffered or stopped before EOF")
	}
	if err := s.Finish(&model.Usage{PromptTokens: 12, CompletionTokens: 4, CacheReadTokens: 2}); err != nil {
		t.Fatal(err)
	}
	events := readEvents(t, out.String())
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if len(events) != len(want) {
		t.Fatalf("unexpected events: %s", out.String())
	}
	for i, event := range events {
		if event["type"] != want[i] {
			t.Fatal(i, event)
		}
	}
	delta := events[4]
	if delta["delta"].(map[string]any)["stop_reason"] != "max_tokens" || delta["usage"].(map[string]any)["input_tokens"] != float64(12) || delta["usage"].(map[string]any)["output_tokens"] != float64(4) {
		t.Fatal(delta)
	}
}

func TestStreamParallelFragmentedTools(t *testing.T) {
	var out bytes.Buffer
	s := NewStreamEncoder(&out, "alias")
	call := func(index int, id, name, args string) *model.StreamChunk {
		return &model.StreamChunk{Choices: []model.Choice{{Delta: &model.Delta{ToolCalls: []model.ToolCall{{Index: &index, ID: id, Function: model.FunctionCall{Name: name, Arguments: args}}}}}}}
	}
	for _, chunk := range []*model.StreamChunk{
		{Choices: []model.Choice{{Delta: &model.Delta{Content: "checking"}}}},
		call(4, "call-", "look", ""), call(4, "a", "up", `{"a":`),
		call(9, "call-b", "second", `{"b":`),
		call(4, "", "", `1}`), call(9, "", "", `2}`),
		{Choices: []model.Choice{{Delta: &model.Delta{Content: "done"}, FinishReason: "tool_calls"}}},
	} {
		if err := s.WriteChunk(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(out.String(), `"partial_json":"1}"`) {
		t.Fatal("arguments buffered until completion")
	}
	if err := s.Finish(nil); err != nil {
		t.Fatal(err)
	}
	starts, stops := map[int]bool{}, map[int]bool{}
	args := map[int]string{}
	for _, event := range readEvents(t, out.String()) {
		index, _ := event["index"].(float64)
		i := int(index)
		switch event["type"] {
		case "content_block_start":
			if starts[i] {
				t.Fatal("duplicate block start")
			}
			starts[i] = true
			block := event["content_block"].(map[string]any)
			if i == 1 && (block["id"] != "call-a" || block["name"] != "lookup") {
				t.Fatal(block)
			}
		case "content_block_delta":
			if !starts[i] || stops[i] {
				t.Fatal("delta outside block")
			}
			delta := event["delta"].(map[string]any)
			if delta["type"] == "input_json_delta" {
				args[i] += delta["partial_json"].(string)
			}
		case "content_block_stop":
			if !starts[i] || stops[i] {
				t.Fatal("invalid stop")
			}
			stops[i] = true
		}
	}
	if len(starts) != 4 || len(stops) != 4 || args[1] != `{"a":1}` || args[2] != `{"b":2}` {
		t.Fatalf("bad blocks: %v %v %v", starts, stops, args)
	}
}

func TestStreamFailureAndMalformedTools(t *testing.T) {
	for _, args := range []string{`{"bad":`, `[]`, `null`} {
		var out bytes.Buffer
		s := NewStreamEncoder(&out, "alias")
		if err := s.WriteChunk(&model.StreamChunk{Choices: []model.Choice{{Delta: &model.Delta{ToolCalls: []model.ToolCall{{ID: "call", Function: model.FunctionCall{Name: "tool", Arguments: args}}}}, FinishReason: "tool_calls"}}}); err != nil {
			t.Fatal(err)
		}
		err := s.Finish(nil)
		if err == nil {
			t.Fatal("malformed JSON accepted")
		}
		if err := s.Error(model.ErrProviderError("test", err), "req-id"); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "message_stop") || !strings.Contains(out.String(), "event: error") {
			t.Fatal(out.String())
		}
	}
	var out bytes.Buffer
	s := NewStreamEncoder(&out, "alias")
	if err := s.Finish(nil); err == nil {
		t.Fatal("empty upstream accepted")
	}
	if err := s.WriteChunk(nil); err == nil {
		t.Fatal("nil chunk accepted")
	}
}
