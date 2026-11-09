// Package content captures a bounded, sanitized copy of the LLM API exchange.
package content

import (
	"bytes"
	"encoding/json"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

type Protocol string

const (
	Chat      Protocol = "chat"
	Messages  Protocol = "messages"
	Responses Protocol = "responses"
)

type Capture struct {
	protocol     Protocol
	input        messageSet
	instructions []part
	previousID   string
	output       messageSet
	responseID   string
	status       string
	frameLimit   int
	frame        []byte
	dropFrame    bool
	stream       bool
	complete     bool
	failedWrite  bool
	items        map[int]*streamItem
	streamBytes  int
}

func New(protocol Protocol, limit int) *Capture {
	return &Capture{
		protocol: protocol, frameLimit: max(64<<10, 4*limit),
		input: newMessageSet(limit), output: newMessageSet(limit),
		streamBytes: limit,
	}
}

// Input receives the original decoded fields before any protocol translation.
func (c *Capture) Input(fields map[string]json.RawMessage) {
	// Leave room for the field names in the Langfuse input envelope.
	c.input.remaining -= 64
	switch c.protocol {
	case Messages:
		c.systemInstructions(fields["system"])
	case Responses:
		c.systemInstructions(fields["instructions"])
		var id string
		if json.Unmarshal(fields["previous_response_id"], &id) == nil {
			c.previousID = c.input.text(id)
		}
	}
	if c.protocol == Responses {
		raw := fields["input"]
		if len(raw) != 0 && raw[0] == '"' {
			c.input.add("user", "", func(put func(part) bool) { c.input.textParts(raw, put) })
		} else {
			walkItems(raw, c.input.inputItem)
		}
		return
	}
	walkItems(fields["messages"], c.input.inputItem)
}

func (c *Capture) systemInstructions(raw json.RawMessage) {
	c.input.add("system", "", func(put func(part) bool) { c.input.textParts(raw, put) })
	if len(c.input.messages) > 0 {
		c.instructions = c.input.messages[0].Parts
		c.input.messages = nil
	}
}

// Write observes successfully written bytes after the real writer accepts them.
// The proxy emits one JSON value per SSE data line.
func (c *Capture) Write(data []byte, streaming, failed bool) {
	c.stream = streaming
	c.failedWrite = c.failedWrite || failed
	if !streaming {
		c.appendFrame(data)
		return
	}
	for len(data) != 0 {
		line, rest, found := bytes.Cut(data, []byte{'\n'})
		c.appendFrame(line)
		if !found {
			break
		}
		if !c.dropFrame {
			if payload, ok := bytes.CutPrefix(c.frame, []byte("data:")); ok {
				c.event(bytes.TrimSpace(payload))
			}
		}
		c.frame = c.frame[:0]
		c.dropFrame = false
		data = rest
	}
}

func (c *Capture) appendFrame(data []byte) {
	if c.dropFrame {
		return
	}
	if len(data) > c.frameLimit-len(c.frame) {
		c.output.truncated = true
		c.dropFrame = true
		c.frame = c.frame[:0]
		return
	}
	c.frame = append(c.frame, data...)
}

type choice struct {
	Index        int      `json:"index"`
	Message      wireItem `json:"message"`
	Delta        wireItem `json:"delta"`
	FinishReason string   `json:"finish_reason"`
}

type response struct {
	ID         string          `json:"id"`
	Role       string          `json:"role"`
	StopReason string          `json:"stop_reason"`
	Status     string          `json:"status"`
	Choices    []choice        `json:"choices"`
	Content    json.RawMessage `json:"content"`
	Output     json.RawMessage `json:"output"`
	OutputText string          `json:"output_text"`
}

func (c *Capture) readResponse(raw []byte) {
	var wire response
	if json.Unmarshal(raw, &wire) != nil {
		return
	}
	if wire.ID != "" {
		c.responseID = c.output.text(wire.ID)
	}
	c.status = c.output.text(wire.Status)
	switch c.protocol {
	case Chat:
		for _, choice := range wire.Choices {
			c.output.add(roleOrAssistant(choice.Message.Role), choice.FinishReason, func(put func(part) bool) {
				c.output.item(choice.Message, put)
			})
		}
	case Messages:
		c.output.add(roleOrAssistant(wire.Role), wire.StopReason, func(put func(part) bool) {
			c.output.textParts(wire.Content, put)
		})
	case Responses:
		c.output.add("assistant", "", func(put func(part) bool) {
			if len(wire.Output) == 0 && wire.OutputText != "" {
				put(part{Type: "text", Content: c.output.text(wire.OutputText)})
				return
			}
			walkItems(wire.Output, func(item wireItem) bool { return c.output.item(item, put) })
		})
	}
}

func roleOrAssistant(role string) string {
	if role == "" {
		return "assistant"
	}
	return role
}

func (c *Capture) Attributes() []attribute.KeyValue {
	if c.stream {
		if len(c.output.messages) == 0 {
			c.assemble()
		}
	} else if !c.dropFrame && !c.failedWrite && len(c.frame) != 0 {
		c.readResponse(c.frame)
	}

	var attrs []attribute.KeyValue
	if len(c.input.messages) != 0 || len(c.instructions) != 0 {
		if len(c.input.messages) != 0 {
			input, _ := json.Marshal(c.input.messages)
			attrs = append(attrs, attribute.String("gen_ai.input.messages", string(input)))
		}
		if len(c.instructions) != 0 {
			system, _ := json.Marshal(c.instructions)
			attrs = append(attrs, attribute.String("gen_ai.system_instructions", string(system)))
		}
		input, _ := json.Marshal(struct {
			Messages     []message `json:"messages,omitempty"`
			Instructions []part    `json:"system_instructions,omitempty"`
		}{c.input.messages, c.instructions})
		attrs = append(attrs, attribute.String("langfuse.observation.input", string(input)))
	}
	if len(c.output.messages) != 0 {
		output, _ := json.Marshal(c.output.messages)
		attrs = append(attrs, attribute.String("gen_ai.output.messages", string(output)),
			attribute.String("langfuse.observation.output", string(output)))
	}
	if c.responseID != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.id", c.responseID))
	}
	if c.previousID != "" {
		attrs = append(attrs, attribute.String("llmproxy.capture.previous_response_id", c.previousID))
	}
	if c.input.truncated {
		attrs = append(attrs, attribute.Bool("llmproxy.capture.input_truncated", true))
	}
	if c.output.truncated {
		attrs = append(attrs, attribute.Bool("llmproxy.capture.output_truncated", true))
	}
	if c.failedWrite || (c.stream && !c.complete) || strings.EqualFold(c.status, "incomplete") {
		attrs = append(attrs, attribute.Bool("llmproxy.capture.output_partial", true))
	}
	return attrs
}
