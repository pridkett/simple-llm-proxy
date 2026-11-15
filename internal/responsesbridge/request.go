// Package responsesbridge translates between the OpenAI Responses API and the
// proxy's chat-completion model, so either dialect can reach either kind of
// upstream (ADR 020).
package responsesbridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// invalid reports a request the bridge cannot express. Status 400 keeps the
// router from cooling down a healthy deployment over bad input.
func invalid(format string, args ...any) error {
	body, _ := json.Marshal(map[string]any{"error": map[string]any{
		"message": fmt.Sprintf(format, args...),
		"type":    "invalid_request_error",
	}})
	return provider.NewHTTPError("proxy", http.StatusBadRequest, http.Header{}, body)
}

// fields exposes every JSON field of a model value, including the unknown ones
// the model types keep private.
func fields(v any) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	return out, json.Unmarshal(data, &out)
}

func present(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

type inputItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Output    json.RawMessage `json:"output"`
}

type inputPart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Refusal  string `json:"refusal"`
	ImageURL string `json:"image_url"`
	Detail   string `json:"detail"`
}

type flatTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      *bool           `json:"strict"`
}

// chatRequest converts a Responses request into a chat completion request.
// The second result holds the request fields a Responses object echoes back.
func chatRequest(req *model.ResponsesRequest) (*model.ChatCompletionRequest, map[string]any, error) {
	raw, err := fields(req)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case req.Background:
		return nil, nil, invalid("background responses are not available for this model; it is served through chat completions")
	case req.PreviousResponseID != nil && *req.PreviousResponseID != "":
		return nil, nil, invalid("previous_response_id is not available for this model; send the full conversation in input")
	case present(raw["conversation"]):
		return nil, nil, invalid("conversation is not available for this model; send the full conversation in input")
	}

	chat := &model.ChatCompletionRequest{
		Model:             req.Model,
		Temperature:       req.Temperature,
		TopP:              req.TopP,
		MaxTokens:         req.MaxOutputTokens,
		ParallelToolCalls: req.ParallelToolCalls,
		User:              req.User,
	}
	if req.Instructions != "" {
		chat.Messages = append(chat.Messages, model.Message{Role: "system", Content: req.Instructions})
	}
	messages, err := inputMessages(raw["input"])
	if err != nil {
		return nil, nil, err
	}
	chat.Messages = append(chat.Messages, messages...)

	names := map[string]bool{}
	for i, tool := range req.Tools {
		data, err := json.Marshal(tool)
		if err != nil {
			return nil, nil, err
		}
		var flat flatTool
		if err := json.Unmarshal(data, &flat); err != nil || flat.Type != "function" || flat.Name == "" {
			return nil, nil, invalid("tools[%d]: only function tools are available for this model", i)
		}
		names[flat.Name] = true
		def := model.FunctionDef{Name: flat.Name, Description: flat.Description, Strict: flat.Strict}
		if present(flat.Parameters) {
			def.Parameters = flat.Parameters
		}
		chat.Tools = append(chat.Tools, model.Tool{Type: "function", Function: def})
	}
	if chat.ToolChoice, err = chatToolChoice(req.ToolChoice, names); err != nil {
		return nil, nil, err
	}
	if chat.ResponseFormat, err = chatResponseFormat(raw["text"]); err != nil {
		return nil, nil, err
	}

	var reasoning struct {
		Effort string `json:"effort"`
	}
	if present(raw["reasoning"]) {
		_ = json.Unmarshal(raw["reasoning"], &reasoning)
	}
	if reasoning.Effort != "" {
		// OpenAI-compatible upstreams read reasoning_effort; native adapters ignore it.
		if chat, err = withField(chat, "reasoning_effort", reasoning.Effort); err != nil {
			return nil, nil, err
		}
	}

	echo := map[string]any{
		"error":               nil,
		"incomplete_details":  nil,
		"instructions":        nil,
		"metadata":            map[string]string{},
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"tools":               []any{},
	}
	if req.Instructions != "" {
		echo["instructions"] = req.Instructions
	}
	if req.Metadata != nil {
		echo["metadata"] = req.Metadata
	}
	if req.ParallelToolCalls != nil {
		echo["parallel_tool_calls"] = *req.ParallelToolCalls
	}
	for _, key := range []string{"tool_choice", "tools", "temperature", "top_p", "max_output_tokens", "text", "reasoning"} {
		if present(raw[key]) {
			echo[key] = raw[key]
		}
	}
	return chat, echo, nil
}

// withField adds a field the chat request type does not declare.
func withField(chat *model.ChatCompletionRequest, key string, value any) (*model.ChatCompletionRequest, error) {
	raw, err := fields(chat)
	if err != nil {
		return nil, err
	}
	if raw[key], err = json.Marshal(value); err != nil {
		return nil, err
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var out model.ChatCompletionRequest
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	out.AnthropicOptions = chat.AnthropicOptions
	return &out, nil
}

func inputMessages(raw json.RawMessage) ([]model.Message, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []model.Message{{Role: "user", Content: text}}, nil
	}
	var items []inputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, invalid("input must be a string or an array of input items")
	}
	var out []model.Message
	for i, item := range items {
		switch item.Type {
		case "", "message":
			msg, err := inputMessage(item)
			if err != nil {
				return nil, invalid("input[%d]: %v", i, err)
			}
			out = append(out, msg)
		case "function_call":
			if item.CallID == "" || item.Name == "" {
				return nil, invalid("input[%d]: function_call requires call_id and name", i)
			}
			call := model.ToolCall{ID: item.CallID, Type: "function", Function: model.FunctionCall{Name: item.Name, Arguments: item.Arguments}}
			// An assistant turn is a message item plus adjacent call items; chat
			// wants the text and every parallel call on one message.
			if n := len(out); n > 0 && out[n-1].Role == "assistant" {
				out[n-1].ToolCalls = append(out[n-1].ToolCalls, call)
			} else {
				out = append(out, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}})
			}
		case "function_call_output":
			if item.CallID == "" {
				return nil, invalid("input[%d]: function_call_output requires call_id", i)
			}
			text, err := outputText(item.Output)
			if err != nil {
				return nil, invalid("input[%d]: %v", i, err)
			}
			out = append(out, model.Message{Role: "tool", ToolCallID: item.CallID, Content: text})
		case "reasoning":
			// Opaque to other providers and not replayable.
		default:
			return nil, invalid("input[%d]: item type %q is not available for this model", i, item.Type)
		}
	}
	if len(out) == 0 {
		return nil, invalid("input must contain at least one message")
	}
	return out, nil
}

func inputMessage(item inputItem) (model.Message, error) {
	role := item.Role
	switch role {
	case "developer":
		role = "system"
	case "user", "assistant", "system":
	default:
		return model.Message{}, fmt.Errorf("unsupported role %q", item.Role)
	}
	var text string
	if json.Unmarshal(item.Content, &text) == nil {
		return model.Message{Role: role, Content: text}, nil
	}
	var parts []inputPart
	if err := json.Unmarshal(item.Content, &parts); err != nil {
		return model.Message{}, fmt.Errorf("content must be a string or an array of content parts")
	}
	var content []model.ContentPart
	textOnly := true
	for _, part := range parts {
		switch part.Type {
		case "input_text", "output_text", "text":
			content = append(content, model.ContentPart{Type: "text", Text: part.Text})
		case "refusal":
			content = append(content, model.ContentPart{Type: "text", Text: part.Refusal})
		case "input_image":
			if part.ImageURL == "" {
				return model.Message{}, fmt.Errorf("input_image requires image_url; file ids are not available for this model")
			}
			textOnly = false
			content = append(content, model.ContentPart{Type: "image_url", ImageURL: &model.ImageURL{URL: part.ImageURL, Detail: part.Detail}})
		default:
			return model.Message{}, fmt.Errorf("content part type %q is not available for this model", part.Type)
		}
	}
	if textOnly {
		// A plain string is the most widely accepted form, and required for system and assistant turns.
		texts := make([]string, len(content))
		for i, part := range content {
			texts[i] = part.Text
		}
		return model.Message{Role: role, Content: strings.Join(texts, "")}, nil
	}
	return model.Message{Role: role, Content: content}, nil
}

func outputText(raw json.RawMessage) (string, error) {
	var text string
	if !present(raw) || json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var parts []inputPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", fmt.Errorf("function_call_output output must be a string or an array of text parts")
	}
	var texts []string
	for _, part := range parts {
		if part.Type != "input_text" && part.Type != "output_text" && part.Type != "text" {
			return "", fmt.Errorf("function_call_output supports only text output")
		}
		texts = append(texts, part.Text)
	}
	return strings.Join(texts, "\n"), nil
}

func chatToolChoice(choice any, names map[string]bool) (any, error) {
	switch c := choice.(type) {
	case nil:
		return nil, nil
	case string:
		return c, nil
	case map[string]any:
		name, _ := c["name"].(string)
		if c["type"] == "function" && names[name] {
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}, nil
		}
	}
	return nil, invalid("tool_choice must be a string or name a defined function tool")
}

func chatResponseFormat(text json.RawMessage) (*model.ResponseFormat, error) {
	if !present(text) {
		return nil, nil
	}
	var config struct {
		Format map[string]json.RawMessage `json:"format"`
	}
	if err := json.Unmarshal(text, &config); err != nil {
		return nil, invalid("text must be an object")
	}
	var kind string
	_ = json.Unmarshal(config.Format["type"], &kind)
	switch kind {
	case "", "text":
		return nil, nil
	case "json_object":
		return &model.ResponseFormat{Type: kind}, nil
	case "json_schema":
		// Responses keeps name/schema/strict beside type; chat nests them.
		delete(config.Format, "type")
		schema, err := json.Marshal(config.Format)
		if err != nil {
			return nil, err
		}
		return &model.ResponseFormat{Type: kind, JSONSchema: schema}, nil
	}
	return nil, invalid("text.format type %q is not supported", kind)
}
