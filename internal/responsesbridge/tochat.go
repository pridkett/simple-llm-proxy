package responsesbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// responsesBacked answers chat completions through the provider's Responses
// API. Everything else, including native Responses calls, passes straight through.
type responsesBacked struct {
	provider.Provider
	provider.ResponsesProvider
}

// ToChat makes a model that is only served over the Responses API usable from
// chat and Messages clients.
func ToChat(p provider.Provider) (provider.Provider, error) {
	rp, ok := p.(provider.ResponsesProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not implement the Responses API", p.Name())
	}
	return &responsesBacked{Provider: p, ResponsesProvider: rp}, nil
}

func (p *responsesBacked) ChatCompletion(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	out, err := responsesRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.CreateResponse(ctx, out)
	if err != nil {
		return nil, err
	}
	return toChatResponse(resp)
}

func (p *responsesBacked) ChatCompletionStream(ctx context.Context, req *model.ChatCompletionRequest) (provider.Stream, error) {
	out, err := responsesRequest(req)
	if err != nil {
		return nil, err
	}
	stream, err := p.CreateResponseStream(ctx, out)
	if err != nil {
		return nil, err
	}
	return &eventStream{src: stream, tools: map[int]*chatTool{}}, nil
}

type chatPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	ImageURL *model.ImageURL `json:"image_url"`
}

// responsesRequest converts a chat completion request into a Responses request.
func responsesRequest(chat *model.ChatCompletionRequest) (*model.ResponsesRequest, error) {
	raw, err := fields(chat)
	if err != nil {
		return nil, err
	}
	switch {
	case chat.N != nil && *chat.N > 1:
		return nil, invalid("n greater than 1 is not available for this model; it is served through the Responses API")
	case len(chat.Stop) > 0:
		return nil, invalid("stop is not available for this model; it is served through the Responses API")
	case len(chat.Functions) > 0 || chat.FunctionCall != nil:
		return nil, invalid("functions and function_call are not available for this model; use tools")
	}

	// The proxy never reads a stored copy, so do not ask the upstream to keep one.
	body := map[string]any{"model": chat.Model, "store": false}
	var input []any
	for i, msg := range chat.Messages {
		items, err := inputItems(msg)
		if err != nil {
			return nil, invalid("messages[%d]: %v", i, err)
		}
		input = append(input, items...)
	}
	body["input"] = input

	if chat.MaxCompletionTokens != nil {
		body["max_output_tokens"] = *chat.MaxCompletionTokens
	} else if chat.MaxTokens != nil {
		body["max_output_tokens"] = *chat.MaxTokens
	}
	if chat.Temperature != nil {
		body["temperature"] = *chat.Temperature
	}
	if chat.TopP != nil {
		body["top_p"] = *chat.TopP
	}
	if chat.ParallelToolCalls != nil {
		body["parallel_tool_calls"] = *chat.ParallelToolCalls
	}
	if chat.User != "" {
		body["user"] = chat.User
	}
	if present(raw["reasoning_effort"]) {
		body["reasoning"] = map[string]any{"effort": raw["reasoning_effort"]}
	}

	var tools []any
	for i, tool := range chat.Tools {
		if tool.Type != "function" || tool.Function.Name == "" {
			return nil, invalid("tools[%d]: only function tools are supported", i)
		}
		flat := map[string]any{"type": "function", "name": tool.Function.Name, "parameters": tool.Function.Parameters}
		if tool.Function.Description != "" {
			flat["description"] = tool.Function.Description
		}
		if tool.Function.Strict != nil {
			flat["strict"] = *tool.Function.Strict
		}
		tools = append(tools, flat)
	}
	if tools != nil {
		body["tools"] = tools
	}
	switch choice := chat.ToolChoice.(type) {
	case nil:
	case string:
		body["tool_choice"] = choice
	case map[string]any:
		function, _ := choice["function"].(map[string]any)
		name, _ := function["name"].(string)
		if choice["type"] != "function" || name == "" {
			return nil, invalid("tool_choice must be a string or name a function")
		}
		body["tool_choice"] = map[string]any{"type": "function", "name": name}
	default:
		return nil, invalid("tool_choice must be a string or name a function")
	}
	if format := chat.ResponseFormat; format != nil && format.Type != "" && format.Type != "text" {
		flat := map[string]json.RawMessage{}
		if format.Type == "json_schema" {
			// Chat nests name/schema/strict; Responses keeps them beside type.
			if err := json.Unmarshal(format.JSONSchema, &flat); err != nil {
				return nil, invalid("response_format.json_schema must be an object")
			}
		}
		flat["type"], _ = json.Marshal(format.Type)
		body["text"] = map[string]any{"format": flat}
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var out model.ResponsesRequest
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func inputItems(msg model.Message) ([]any, error) {
	// Content may be a string, typed parts from the Messages translator, or
	// decoded JSON; normalize through JSON.
	data, err := json.Marshal(msg.Content)
	if err != nil {
		return nil, err
	}
	var text string
	var parts []chatPart
	if string(data) != "null" && json.Unmarshal(data, &text) != nil {
		if err := json.Unmarshal(data, &parts); err != nil {
			return nil, fmt.Errorf("content must be a string or an array of content parts")
		}
	}
	for _, part := range parts {
		if part.Type != "text" && (part.Type != "image_url" || msg.Role != "user" || part.ImageURL == nil) {
			return nil, fmt.Errorf("content part type %q is not supported for role %q", part.Type, msg.Role)
		}
		text += part.Text
	}

	switch msg.Role {
	case "system", "developer":
		return []any{map[string]any{"role": msg.Role, "content": text}}, nil
	case "user":
		if parts == nil {
			return []any{map[string]any{"role": "user", "content": text}}, nil
		}
		var content []any
		for _, part := range parts {
			if part.Type == "text" {
				content = append(content, map[string]any{"type": "input_text", "text": part.Text})
				continue
			}
			image := map[string]any{"type": "input_image", "image_url": part.ImageURL.URL}
			if part.ImageURL.Detail != "" {
				image["detail"] = part.ImageURL.Detail
			}
			content = append(content, image)
		}
		return []any{map[string]any{"role": "user", "content": content}}, nil
	case "assistant":
		var items []any
		if text != "" {
			items = append(items, map[string]any{"role": "assistant", "content": text})
		}
		for _, call := range msg.ToolCalls {
			if call.ID == "" || call.Function.Name == "" {
				return nil, fmt.Errorf("tool_calls require id and function name")
			}
			items = append(items, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "arguments": arguments(call.Function.Arguments)})
		}
		return items, nil
	case "tool":
		if msg.ToolCallID == "" {
			return nil, fmt.Errorf("tool messages require tool_call_id")
		}
		return []any{map[string]any{"type": "function_call_output", "call_id": msg.ToolCallID, "output": text}}, nil
	}
	return nil, fmt.Errorf("unsupported role %q", msg.Role)
}

// finishReason maps a terminal Responses status onto a chat finish reason.
func finishReason(resp *model.ResponsesResponse, toolCalls bool) (string, error) {
	switch resp.Status {
	case "completed":
		if toolCalls {
			return "tool_calls", nil
		}
		return "stop", nil
	case "incomplete":
		var details struct {
			Reason string `json:"reason"`
		}
		if raw, err := fields(resp); err == nil && present(raw["incomplete_details"]) {
			_ = json.Unmarshal(raw["incomplete_details"], &details)
		}
		if details.Reason == "content_filter" {
			return "content_filter", nil
		}
		return "length", nil
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", fmt.Errorf("upstream response %s: %s", resp.Status, resp.Error.Message)
	}
	return "", fmt.Errorf("upstream response ended with status %q", resp.Status)
}

func chatUsage(u *model.Usage) *model.Usage {
	if u == nil {
		return nil
	}
	// A fresh value drops the Responses field names so chat clients see prompt_tokens.
	total := u.TotalTokens
	if total == 0 {
		total = u.PromptTokens + u.CompletionTokens
	}
	return &model.Usage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: total}
}

// toChatResponse converts a terminal Responses object into a chat completion.
// Reasoning and built-in tool items have no chat equivalent and are dropped.
func toChatResponse(resp *model.ResponsesResponse) (*model.ChatCompletionResponse, error) {
	var text strings.Builder
	var calls []model.ToolCall
	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				text.WriteString(part.Text)
				text.WriteString(part.Refusal)
			}
		case "function_call":
			calls = append(calls, model.ToolCall{ID: item.CallID, Type: "function", Function: model.FunctionCall{Name: item.Name, Arguments: item.Arguments}})
		}
	}
	finish, err := finishReason(resp, len(calls) > 0)
	if err != nil {
		return nil, err
	}
	return &model.ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: resp.CreatedAt,
		Model:   resp.Model,
		Choices: []model.Choice{{Message: &model.Message{Role: "assistant", Content: text.String(), ToolCalls: calls}, FinishReason: finish}},
		Usage:   chatUsage(resp.Usage),
	}, nil
}

type chatTool struct {
	index  int
	deltas bool
}

// eventStream turns Responses events into chat chunks as they arrive.
type eventStream struct {
	src     provider.ResponsesStream
	queue   []*model.StreamChunk
	id      string
	model   string
	created int64
	tools   map[int]*chatTool // by output_index
	done    bool
}

func (s *eventStream) Close() error { return s.src.Close() }

func (s *eventStream) chunk(delta *model.Delta, finish string) *model.StreamChunk {
	if s.created == 0 {
		s.created = time.Now().Unix()
	}
	return &model.StreamChunk{ID: s.id, Object: "chat.completion.chunk", Created: s.created, Model: s.model,
		Choices: []model.Choice{{Delta: delta, FinishReason: finish}}}
}

func (s *eventStream) Recv() (*model.StreamChunk, error) {
	for len(s.queue) == 0 {
		if s.done {
			return nil, io.EOF
		}
		event, err := s.src.Recv()
		if err == io.EOF {
			return nil, fmt.Errorf("upstream stream ended before the response completed")
		}
		if err != nil {
			return nil, err
		}
		if err := s.write(event); err != nil {
			return nil, err
		}
	}
	chunk := s.queue[0]
	s.queue = s.queue[1:]
	return chunk, nil
}

func (s *eventStream) tool(event *model.ResponsesStreamEvent) *chatTool {
	if event.OutputIndex == nil {
		return nil
	}
	return s.tools[*event.OutputIndex]
}

func (s *eventStream) toolDelta(tool *chatTool, call model.ToolCall) {
	index := tool.index
	call.Index = &index
	s.queue = append(s.queue, s.chunk(&model.Delta{ToolCalls: []model.ToolCall{call}}, ""))
}

func (s *eventStream) write(event *model.ResponsesStreamEvent) error {
	switch event.Type {
	case "response.created":
		if event.Response != nil {
			s.id, s.model, s.created = event.Response.ID, event.Response.Model, event.Response.CreatedAt
		}
		s.queue = append(s.queue, s.chunk(&model.Delta{Role: "assistant"}, ""))
	case "response.output_text.delta", "response.refusal.delta":
		if event.Delta != "" {
			s.queue = append(s.queue, s.chunk(&model.Delta{Content: event.Delta}, ""))
		}
	case "response.output_item.added":
		if event.Item == nil || event.Item.Type != "function_call" || event.OutputIndex == nil {
			return nil
		}
		tool := &chatTool{index: len(s.tools)}
		s.tools[*event.OutputIndex] = tool
		s.toolDelta(tool, model.ToolCall{ID: event.Item.CallID, Type: "function", Function: model.FunctionCall{Name: event.Item.Name}})
	case "response.function_call_arguments.delta":
		if tool := s.tool(event); tool != nil && event.Delta != "" {
			tool.deltas = true
			s.toolDelta(tool, model.ToolCall{Function: model.FunctionCall{Arguments: event.Delta}})
		}
	case "response.function_call_arguments.done":
		// Some upstreams send the arguments whole instead of in deltas.
		if tool := s.tool(event); tool != nil && !tool.deltas {
			var args string
			if raw, err := fields(event); err == nil {
				_ = json.Unmarshal(raw["arguments"], &args)
			}
			if args != "" {
				tool.deltas = true
				s.toolDelta(tool, model.ToolCall{Function: model.FunctionCall{Arguments: args}})
			}
		}
	case "response.completed", "response.incomplete", "response.failed", "response.cancelled":
		if event.Response == nil {
			return fmt.Errorf("upstream %s event has no response", event.Type)
		}
		finish, err := finishReason(event.Response, len(s.tools) > 0)
		if err != nil {
			return err
		}
		s.done = true
		s.queue = append(s.queue, s.chunk(&model.Delta{}, finish))
		if usage := chatUsage(event.Response.Usage); usage != nil {
			final := s.chunk(nil, "")
			final.Choices = []model.Choice{}
			final.Usage = usage
			s.queue = append(s.queue, final)
		}
	case "error":
		var message string
		if raw, err := fields(event); err == nil {
			_ = json.Unmarshal(raw["message"], &message)
		}
		return fmt.Errorf("upstream stream error: %s", message)
	}
	return nil
}
