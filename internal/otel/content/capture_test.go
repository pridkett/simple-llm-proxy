package content

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCaptureBudgetAndSSEFrameRecovery(t *testing.T) {
	const budget = 1024
	c := New(Chat, budget)
	raw, _ := json.Marshal([]map[string]string{
		{"role": "system", "content": strings.Repeat("📦", 3000)},
		{"role": "user", "content": "later instructions"},
	})
	c.Input(map[string]json.RawMessage{"messages": raw})
	c.Write([]byte("data: "+strings.Repeat("a", c.frameLimit+1)), true, false)
	c.Write([]byte("\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"captured later\"}}]}\n\ndata: [DONE]\n\n"), true, false)
	attrs := make(map[string]any)
	for _, attr := range c.Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsInterface()
	}
	for _, key := range []string{"gen_ai.input.messages", "gen_ai.output.messages", "langfuse.observation.input"} {
		value, _ := attrs[key].(string)
		if len(value) > budget || !json.Valid([]byte(value)) {
			t.Errorf("invalid or oversized capture %s: %d bytes", key, len(value))
		}
	}
	if attrs["llmproxy.capture.input_truncated"] != true || attrs["llmproxy.capture.output_truncated"] != true ||
		attrs["llmproxy.capture.output_partial"] != nil ||
		!strings.Contains(attrs["gen_ai.input.messages"].(string), "later instructions") ||
		!strings.Contains(attrs["gen_ai.output.messages"].(string), "captured later") {
		t.Fatalf("unexpected capture: %v", attrs)
	}
}

func TestCaptureFieldLimitScalesWithBudget(t *testing.T) {
	c := New(Chat, 1<<20)
	long := strings.Repeat("x", 20<<10)
	raw, _ := json.Marshal([]map[string]string{{"role": "user", "content": long}})
	c.Input(map[string]json.RawMessage{"messages": raw})
	c.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"`+long+`"}}]}`), false, false)
	attrs := make(map[string]any)
	for _, attr := range c.Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsInterface()
	}
	if attrs["llmproxy.capture.input_truncated"] != nil || attrs["llmproxy.capture.output_truncated"] != nil {
		t.Fatalf("20 KiB fields were truncated under a 1 MiB budget: %v", attrs["llmproxy.capture.input_truncated"])
	}
	for _, key := range []string{"gen_ai.input.messages", "gen_ai.output.messages"} {
		if value, _ := attrs[key].(string); !strings.Contains(value, long) {
			t.Errorf("%s lost content: %d bytes", key, len(value))
		}
	}
}
