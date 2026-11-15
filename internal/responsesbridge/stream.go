package responsesbridge

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

type streamTool struct {
	itemID    string
	callID    string
	name      string
	arguments strings.Builder
	output    int // output_index, valid once started
	started   bool
}

// chatStream turns chat chunks into Responses events as they arrive. Text and
// tool arguments are forwarded incrementally; nothing waits for the full reply.
type chatStream struct {
	src     provider.Stream
	env     *envelope
	queue   []*model.ResponsesStreamEvent
	seq     int
	started bool
	done    bool

	output []map[string]any // items by output_index; finalized as they close
	textID string           // open message item, or ""
	textAt int
	text   strings.Builder
	tools  map[int]*streamTool
	order  []*streamTool
	finish string
	usage  *model.Usage
}

func newChatStream(src provider.Stream, env *envelope) *chatStream {
	return &chatStream{src: src, env: env, tools: map[int]*streamTool{}}
}

func (s *chatStream) Close() error { return s.src.Close() }

func (s *chatStream) Recv() (*model.ResponsesStreamEvent, error) {
	for len(s.queue) == 0 {
		if s.done {
			return nil, io.EOF
		}
		var err error
		if !s.started {
			// Announce the response before the first upstream byte.
			s.started = true
			err = s.lifecycle("response.created", "response.in_progress")
		} else {
			var chunk *model.StreamChunk
			chunk, err = s.src.Recv()
			if err == io.EOF {
				s.done = true
				err = s.end()
			} else if err == nil {
				err = s.write(chunk)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	event := s.queue[0]
	s.queue = s.queue[1:]
	return event, nil
}

func (s *chatStream) emit(name string, body map[string]any) error {
	body["type"] = name
	body["sequence_number"] = s.seq
	s.seq++
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var event model.ResponsesStreamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return err
	}
	s.queue = append(s.queue, &event)
	return nil
}

func (s *chatStream) lifecycle(names ...string) error {
	for _, name := range names {
		response, err := s.env.response("", []map[string]any{}, nil)
		if err != nil {
			return err
		}
		if err := s.emit(name, map[string]any{"response": response}); err != nil {
			return err
		}
	}
	return nil
}

func (s *chatStream) write(chunk *model.StreamChunk) error {
	if chunk == nil {
		return fmt.Errorf("upstream returned a nil stream chunk")
	}
	if chunk.Usage != nil {
		s.usage = chunk.Usage
	}
	if chunk.Model != "" {
		s.env.model = chunk.Model
	}
	for _, choice := range chunk.Choices {
		if choice.Index != 0 {
			continue
		}
		if choice.FinishReason != "" {
			s.finish = choice.FinishReason
		}
		if choice.Delta == nil {
			continue
		}
		if text := choice.Delta.Content; text != "" {
			if err := s.writeText(text); err != nil {
				return err
			}
		}
		for _, call := range choice.Delta.ToolCalls {
			if err := s.writeTool(call); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *chatStream) writeText(text string) error {
	if s.textID == "" {
		s.textID = newID("msg")
		s.textAt = len(s.output)
		s.text.Reset()
		item := messageItem(s.textID, "", "in_progress")
		item["content"] = []any{}
		s.output = append(s.output, item)
		if err := s.emit("response.output_item.added", map[string]any{"output_index": s.textAt, "item": item}); err != nil {
			return err
		}
		if err := s.emit("response.content_part.added", map[string]any{"item_id": s.textID, "output_index": s.textAt, "content_index": 0, "part": textPart("")}); err != nil {
			return err
		}
	}
	s.text.WriteString(text)
	return s.emit("response.output_text.delta", map[string]any{"item_id": s.textID, "output_index": s.textAt, "content_index": 0, "delta": text})
}

func (s *chatStream) closeText() error {
	if s.textID == "" {
		return nil
	}
	id, at, text := s.textID, s.textAt, s.text.String()
	s.textID = ""
	if err := s.emit("response.output_text.done", map[string]any{"item_id": id, "output_index": at, "content_index": 0, "text": text}); err != nil {
		return err
	}
	if err := s.emit("response.content_part.done", map[string]any{"item_id": id, "output_index": at, "content_index": 0, "part": textPart(text)}); err != nil {
		return err
	}
	s.output[at] = messageItem(id, text, "completed")
	return s.emit("response.output_item.done", map[string]any{"output_index": at, "item": s.output[at]})
}

func (s *chatStream) writeTool(call model.ToolCall) error {
	index := 0
	if call.Index != nil {
		index = *call.Index
	}
	tool := s.tools[index]
	if tool == nil {
		tool = &streamTool{itemID: newID("fc")}
		s.tools[index] = tool
		s.order = append(s.order, tool)
	}
	if tool.started && (call.ID != "" || call.Function.Name != "") {
		return fmt.Errorf("upstream changed tool identity after arguments started")
	}
	tool.callID += call.ID
	tool.name += call.Function.Name
	if call.Function.Arguments == "" {
		return nil
	}
	if err := s.startTool(tool); err != nil {
		return err
	}
	tool.arguments.WriteString(call.Function.Arguments)
	return s.emit("response.function_call_arguments.delta", map[string]any{"item_id": tool.itemID, "output_index": tool.output, "delta": call.Function.Arguments})
}

func (s *chatStream) startTool(tool *streamTool) error {
	if tool.started {
		return nil
	}
	if tool.callID == "" || tool.name == "" {
		return fmt.Errorf("upstream tool call is missing id or name")
	}
	// A message item ends where the first tool call begins.
	if err := s.closeText(); err != nil {
		return err
	}
	tool.started = true
	tool.output = len(s.output)
	item := functionItem(tool.itemID, tool.callID, tool.name, "", "in_progress")
	s.output = append(s.output, item)
	return s.emit("response.output_item.added", map[string]any{"output_index": tool.output, "item": item})
}

func (s *chatStream) end() error {
	if s.finish == "" {
		return fmt.Errorf("upstream stream ended without a finish reason")
	}
	if err := s.closeText(); err != nil {
		return err
	}
	for _, tool := range s.order {
		// Calls with no arguments never produced a delta, so they open here.
		if err := s.startTool(tool); err != nil {
			return err
		}
		args := arguments(tool.arguments.String())
		if err := s.emit("response.function_call_arguments.done", map[string]any{"item_id": tool.itemID, "output_index": tool.output, "arguments": args}); err != nil {
			return err
		}
		s.output[tool.output] = functionItem(tool.itemID, tool.callID, tool.name, args, "completed")
		if err := s.emit("response.output_item.done", map[string]any{"output_index": tool.output, "item": s.output[tool.output]}); err != nil {
			return err
		}
	}
	response, err := s.env.response(s.finish, s.output, s.usage)
	if err != nil {
		return err
	}
	name := "response.completed"
	if response.Status == "incomplete" {
		name = "response.incomplete"
	}
	return s.emit(name, map[string]any{"response": response})
}
