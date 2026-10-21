package anthropic

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

// WriteError distinguishes downstream disconnects from invalid upstream data.
type WriteError struct{ Err error }

func (e *WriteError) Error() string { return e.Err.Error() }
func (e *WriteError) Unwrap() error { return e.Err }

type streamTool struct {
	index     int
	id        string
	name      string
	arguments string
	started   bool
}

// StreamEncoder writes named Anthropic SSE events directly from normalized
// chunks. It does not buffer response text or wait for all tool arguments.
type StreamEncoder struct {
	// BeforeStop emits optional protocol extensions after validation and before message_stop.
	BeforeStop   func()
	w            io.Writer
	alias        string
	started      bool
	nextIndex    int
	textIndex    int
	tools        map[int]*streamTool
	toolOrder    []*streamTool
	finish       string
	stopSequence *string
}

func NewStreamEncoder(w io.Writer, alias string) *StreamEncoder {
	return &StreamEncoder{w: w, alias: alias, textIndex: -1, tools: map[int]*streamTool{}}
}

func (s *StreamEncoder) event(name string, fields map[string]any) error {
	fields["type"] = name
	data, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data)
	if err != nil {
		return &WriteError{Err: err}
	}
	return nil
}

func (s *StreamEncoder) start(usage *model.Usage) error {
	if s.started {
		return nil
	}
	s.started = true
	u := usageFromChat(usage)
	u.OutputTokens = 0
	return s.event("message_start", map[string]any{"message": Response{ID: messageID(), Type: "message", Role: "assistant", Model: s.alias, Content: []Content{}, Usage: u}})
}

func (s *StreamEncoder) startTool(tool *streamTool) error {
	if tool.started {
		return nil
	}
	if tool.id == "" || tool.name == "" {
		return fmt.Errorf("upstream tool call is missing id or name")
	}
	tool.index = s.nextIndex
	s.nextIndex++
	tool.started = true
	s.textIndex = -1
	return s.event("content_block_start", map[string]any{"index": tool.index, "content_block": map[string]any{"type": "tool_use", "id": tool.id, "name": tool.name, "input": map[string]any{}}})
}

func (s *StreamEncoder) WriteChunk(chunk *model.StreamChunk) error {
	if chunk == nil {
		return fmt.Errorf("upstream returned a nil stream chunk")
	}
	if err := s.start(chunk.Usage); err != nil {
		return err
	}
	for _, choice := range chunk.Choices {
		if choice.Index != 0 {
			continue
		}
		if choice.FinishReason != "" {
			s.finish = choice.FinishReason
			s.stopSequence = choice.StopSequence
		}
		if choice.Delta == nil {
			continue
		}
		if text := choice.Delta.Content; text != "" {
			if s.textIndex < 0 {
				s.textIndex = s.nextIndex
				s.nextIndex++
				if err := s.event("content_block_start", map[string]any{"index": s.textIndex, "content_block": map[string]any{"type": "text", "text": ""}}); err != nil {
					return err
				}
			}
			if err := s.event("content_block_delta", map[string]any{"index": s.textIndex, "delta": map[string]any{"type": "text_delta", "text": text}}); err != nil {
				return err
			}
		}
		for _, call := range choice.Delta.ToolCalls {
			index := 0
			if call.Index != nil {
				index = *call.Index
			}
			tool := s.tools[index]
			if tool == nil {
				tool = &streamTool{}
				s.tools[index] = tool
				s.toolOrder = append(s.toolOrder, tool)
			}
			if tool.started && (call.ID != "" || call.Function.Name != "") {
				return fmt.Errorf("upstream changed tool identity after arguments started")
			}
			tool.id += call.ID
			tool.name += call.Function.Name
			if call.Function.Arguments != "" {
				if err := s.startTool(tool); err != nil {
					return err
				}
				tool.arguments += call.Function.Arguments
				if err := s.event("content_block_delta", map[string]any{"index": tool.index, "delta": map[string]any{"type": "input_json_delta", "partial_json": call.Function.Arguments}}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *StreamEncoder) Finish(usage *model.Usage) error {
	if s.finish == "" {
		return fmt.Errorf("upstream stream ended without a finish reason")
	}
	if err := s.start(usage); err != nil {
		return err
	}
	for _, tool := range s.toolOrder {
		if _, err := toolInput(tool.arguments); err != nil {
			return err
		}
		if err := s.startTool(tool); err != nil {
			return err
		}
	}
	for index := 0; index < s.nextIndex; index++ {
		if err := s.event("content_block_stop", map[string]any{"index": index}); err != nil {
			return err
		}
	}
	if err := s.event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stopReason(s.finish), "stop_sequence": s.stopSequence}, "usage": usageFromChat(usage)}); err != nil {
		return err
	}
	if s.BeforeStop != nil {
		s.BeforeStop()
	}
	return s.event("message_stop", map[string]any{})
}

func (s *StreamEncoder) Error(err *model.ProxyError, requestID string) error {
	data, marshalErr := json.Marshal(model.ToAnthropicError(err, requestID))
	if marshalErr != nil {
		return marshalErr
	}
	_, writeErr := fmt.Fprintf(s.w, "event: error\ndata: %s\n\n", data)
	if writeErr != nil {
		return &WriteError{Err: writeErr}
	}
	return nil
}
