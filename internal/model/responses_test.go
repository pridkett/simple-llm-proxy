package model

import (
	"encoding/json"
	"testing"
)

func TestResponsesRequest_MarshalRoundTrip(t *testing.T) {
	max := 100
	req := ResponsesRequest{
		Model:      "gpt-5",
		Input:      "hello",
		Background: true,
		Stream:     false,
		MaxOutputTokens: &max,
		Metadata:   map[string]string{"trace": "abc"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ResponsesRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Model != req.Model || got.Input != req.Input || got.Background != req.Background {
		t.Errorf("round trip mismatch: got %+v", got)
	}
	if got.MaxOutputTokens == nil || *got.MaxOutputTokens != max {
		t.Errorf("MaxOutputTokens round trip failed: %+v", got.MaxOutputTokens)
	}
}

func TestResponsesResponse_IsTerminal(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"queued", false},
		{"in_progress", false},
		{"completed", true},
		{"failed", true},
		{"cancelled", true},
		{"incomplete", true},
	}
	for _, c := range cases {
		r := &ResponsesResponse{Status: c.status}
		if got := r.IsTerminal(); got != c.want {
			t.Errorf("IsTerminal(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestResponsesResponse_UnmarshalOutputItems(t *testing.T) {
	raw := `{
		"id": "resp_1",
		"object": "response",
		"status": "completed",
		"output": [
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "hi"}]},
			{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "{}"}
		],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`

	var resp ResponsesResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 output items, got %d", len(resp.Output))
	}
	if resp.Output[0].Content[0].Text != "hi" {
		t.Errorf("unexpected message content: %+v", resp.Output[0])
	}
	if resp.Output[1].Name != "get_weather" || resp.Output[1].CallID != "call_1" {
		t.Errorf("unexpected function_call item: %+v", resp.Output[1])
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 15 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}
}

func TestResponsesStreamEvent_MarshalRoundTrip(t *testing.T) {
	idx := 0
	event := ResponsesStreamEvent{
		Type:        "response.output_text.delta",
		Delta:       "hi",
		ItemID:      "item_1",
		OutputIndex: &idx,
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ResponsesStreamEvent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != event.Type || got.Delta != event.Delta || got.ItemID != event.ItemID {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if got.OutputIndex == nil || *got.OutputIndex != idx {
		t.Errorf("OutputIndex round trip failed: %+v", got.OutputIndex)
	}
}
