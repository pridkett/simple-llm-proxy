package content

import (
	"bytes"
	"encoding/json"

	"go.opentelemetry.io/otel/attribute"
)

// Keep fields in their client-facing format. When one field exceeds the
// budget, other fields can still be retained as valid JSON.
func (s *messageSet) decisionPayload(fields map[string]json.RawMessage, keys ...string) string {
	payload := make(map[string]any, len(keys))
	var encoded []byte
	for _, key := range keys {
		raw := fields[key]
		if len(raw) == 0 {
			continue
		}
		// Bound the amount of JSON parsed for a capture. Text can be clipped
		// by the shared sanitizer without dropping the entire field.
		if len(raw) > max(64<<10, 4*s.remaining) {
			s.truncated = true
			continue
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			continue
		}
		payload[key] = s.redact(value, true)
		candidate, err := json.Marshal(payload)
		if err != nil || len(candidate) > s.remaining+2 {
			delete(payload, key)
			s.truncated = true
			continue
		}
		encoded = candidate
	}
	return string(encoded)
}

func (c *Capture) decisionAttributes() []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if !c.dropFrame && !c.failedWrite && len(c.frame) != 0 {
		var fields map[string]json.RawMessage
		if json.Unmarshal(c.frame, &fields) == nil {
			if output := c.output.decisionPayload(fields, "answers", "refusal"); output != "" {
				attrs = append(attrs, attribute.String("langfuse.observation.output", output))
			}
		}
	}
	if c.requestJSON != "" {
		attrs = append(attrs, attribute.String("langfuse.observation.input", c.requestJSON))
	}
	if c.input.truncated {
		attrs = append(attrs, attribute.Bool("llmproxy.capture.input_truncated", true))
	}
	if c.output.truncated {
		attrs = append(attrs, attribute.Bool("llmproxy.capture.output_truncated", true))
	}
	if c.failedWrite {
		attrs = append(attrs, attribute.Bool("llmproxy.capture.output_partial", true))
	}
	return attrs
}
