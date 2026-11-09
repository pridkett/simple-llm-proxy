package content

import (
	"encoding/json"
	"slices"
	"strings"
)

type streamItem struct {
	role   string
	finish string
	parts  map[int]*streamPart
}

type streamPart struct {
	kind      string
	text      strings.Builder
	id        strings.Builder
	name      strings.Builder
	arguments strings.Builder
	argsLost  bool
}

func (c *Capture) streamPart(index, block int, kind string) *streamPart {
	item := c.items[index]
	if item == nil {
		if c.streamBytes < 64 {
			c.output.truncated = true
			return nil
		}
		if c.items == nil {
			c.items = make(map[int]*streamItem)
		}
		item = &streamItem{parts: make(map[int]*streamPart)}
		c.items[index] = item
		c.streamBytes -= 64
	}
	p := item.parts[block]
	if p == nil {
		if c.streamBytes < 96 {
			c.output.truncated = true
			return nil
		}
		p = &streamPart{kind: kind}
		item.parts[block] = p
		c.streamBytes -= 96
	}
	return p
}

func (c *Capture) appendDelta(dst *strings.Builder, delta string) bool {
	room := min(c.streamBytes, c.output.fieldLimit+8192-dst.Len())
	ok := len(delta) <= room
	if !ok {
		c.output.truncated = true
		delta = clip(delta, room)
	}
	c.streamBytes -= len(delta)
	dst.WriteString(delta)
	return ok
}

type event struct {
	Type         string          `json:"type"`
	ID           string          `json:"id"`
	Index        int             `json:"index"`
	OutputIndex  int             `json:"output_index"`
	ContentIndex int             `json:"content_index"`
	Choices      []choice        `json:"choices"`
	Message      *response       `json:"message"`
	Block        wireItem        `json:"content_block"`
	Item         wireItem        `json:"item"`
	Response     json.RawMessage `json:"response"`
	Delta        json.RawMessage `json:"delta"`
}

func (c *Capture) event(payload []byte) {
	if c.protocol == Chat && string(payload) == "[DONE]" {
		c.complete = true
		return
	}
	var e event
	if json.Unmarshal(payload, &e) != nil {
		return
	}
	switch c.protocol {
	case Chat:
		c.chatEvent(e)
	case Messages:
		c.messagesEvent(e)
	case Responses:
		c.responsesEvent(e)
	}
}

func (c *Capture) chatEvent(e event) {
	if c.responseID == "" && e.ID != "" {
		c.responseID = c.output.text(e.ID)
	}
	for _, choice := range e.Choices {
		var text string
		if json.Unmarshal(choice.Delta.Content, &text) == nil && text != "" {
			if p := c.streamPart(choice.Index, 0, "text"); p != nil {
				c.appendDelta(&p.text, text)
			}
		}
		walkItems(choice.Delta.ToolCalls, func(call wireItem) bool {
			return c.chatToolDelta(choice.Index, call.Index+1, call.ID, call.Function)
		})
		if choice.Delta.LegacyCall != nil {
			c.chatToolDelta(choice.Index, 1, "", choice.Delta.LegacyCall)
		}
		if item := c.items[choice.Index]; item != nil {
			if choice.Delta.Role != "" {
				item.role = c.output.text(choice.Delta.Role)
			}
			if choice.FinishReason != "" {
				item.finish = c.output.text(choice.FinishReason)
			}
		}
	}
}

func (c *Capture) chatToolDelta(choice, index int, id string, function *wireItem) bool {
	if function == nil {
		return true
	}
	p := c.streamPart(choice, index, "tool_call")
	if p == nil {
		return false
	}
	c.appendDelta(&p.id, id)
	c.appendDelta(&p.name, function.Name)
	var args string
	if json.Unmarshal(function.Arguments, &args) == nil && !c.appendDelta(&p.arguments, args) {
		p.argsLost = true
	}
	return true
}

func (c *Capture) messagesEvent(e event) {
	switch e.Type {
	case "message_start":
		if e.Message != nil {
			c.responseID = c.output.text(e.Message.ID)
		}
	case "content_block_start":
		switch e.Block.Type {
		case "text":
			if p := c.streamPart(0, e.Index, "text"); p != nil {
				c.appendDelta(&p.text, e.Block.Text)
			}
		case "tool_use":
			if p := c.streamPart(0, e.Index, "tool_call"); p != nil {
				c.appendDelta(&p.id, e.Block.ID)
				c.appendDelta(&p.name, e.Block.Name)
				if string(e.Block.Input) != "{}" {
					p.argsLost = !c.appendDelta(&p.arguments, string(e.Block.Input))
				}
			}
		}
	case "content_block_delta":
		var delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		}
		if json.Unmarshal(e.Delta, &delta) != nil {
			return
		}
		switch delta.Type {
		case "text_delta":
			if p := c.streamPart(0, e.Index, "text"); p != nil {
				c.appendDelta(&p.text, delta.Text)
			}
		case "input_json_delta":
			if p := c.streamPart(0, e.Index, "tool_call"); p != nil && !c.appendDelta(&p.arguments, delta.PartialJSON) {
				p.argsLost = true
			}
		}
	case "message_delta":
		var delta response
		if item := c.items[0]; item != nil && json.Unmarshal(e.Delta, &delta) == nil {
			item.finish = c.output.text(delta.StopReason)
		}
	case "message_stop":
		c.complete = true
	}
}

func (c *Capture) responsesEvent(e event) {
	switch e.Type {
	case "response.created", "response.in_progress":
		var envelope response
		if json.Unmarshal(e.Response, &envelope) == nil {
			c.responseID = c.output.text(envelope.ID)
		}
	case "response.completed", "response.incomplete", "response.failed", "response.cancelled":
		c.complete = e.Type == "response.completed"
		c.readResponse(e.Response)
	case "response.output_item.added", "response.output_item.done":
		kind := e.Item.Type
		if kind != "function_call" && kind != "custom_tool_call" {
			return
		}
		if p := c.streamPart(e.OutputIndex, -1, kind); p != nil {
			if p.id.Len() == 0 {
				c.appendDelta(&p.id, e.Item.CallID)
			}
			if p.name.Len() == 0 {
				c.appendDelta(&p.name, e.Item.Name)
			}
			if e.Type == "response.output_item.done" && p.arguments.Len() == 0 {
				var args string
				raw := e.Item.Arguments
				if kind == "custom_tool_call" {
					raw = e.Item.Input
				}
				if json.Unmarshal(raw, &args) == nil {
					p.argsLost = !c.appendDelta(&p.arguments, args)
				}
			}
		}
	case "response.output_text.delta", "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		var delta string
		if json.Unmarshal(e.Delta, &delta) != nil {
			return
		}
		switch e.Type {
		case "response.output_text.delta":
			if p := c.streamPart(e.OutputIndex, e.ContentIndex, "text"); p != nil {
				c.appendDelta(&p.text, delta)
			}
		default:
			kind := "function_call"
			if e.Type == "response.custom_tool_call_input.delta" {
				kind = "custom_tool_call"
			}
			if p := c.streamPart(e.OutputIndex, -1, kind); p != nil && !c.appendDelta(&p.arguments, delta) {
				p.argsLost = true
			}
		}
	}
}

func (c *Capture) assemble() {
	if c.protocol == Responses {
		c.output.add("assistant", "", func(put func(part) bool) {
			for _, index := range sortedIndexes(c.items) {
				if !c.parts(c.items[index], put) {
					break
				}
			}
		})
		return
	}
	for _, index := range sortedIndexes(c.items) {
		item := c.items[index]
		c.output.add(roleOrAssistant(item.role), item.finish, func(put func(part) bool) { c.parts(item, put) })
	}
}

func (c *Capture) parts(item *streamItem, put func(part) bool) bool {
	for _, block := range sortedIndexes(item.parts) {
		p := item.parts[block]
		if p.kind == "text" {
			if !put(part{Type: "text", Content: c.output.text(p.text.String())}) {
				return false
			}
			continue
		}
		var args json.RawMessage
		if !p.argsLost {
			args = json.RawMessage(p.arguments.String())
			if p.kind == "custom_tool_call" {
				args, _ = json.Marshal(p.arguments.String())
			}
		}
		if !put(c.output.call(p.id.String(), p.name.String(), args, p.kind == "custom_tool_call")) {
			return false
		}
	}
	return true
}

func sortedIndexes[T any](items map[int]T) []int {
	keys := make([]int, 0, len(items))
	for index := range items {
		keys = append(keys, index)
	}
	slices.Sort(keys)
	return keys
}
