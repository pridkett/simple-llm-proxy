package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

type Content struct {
	Type  string          `json:"type"`
	Text  *string         `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

type Response struct {
	ProxyAccounting any       `json:"proxy_accounting,omitempty"`
	ID              string    `json:"id"`
	Type            string    `json:"type"`
	Role            string    `json:"role"`
	Model           string    `json:"model"`
	Content         []Content `json:"content"`
	StopReason      *string   `json:"stop_reason"`
	StopSequence    *string   `json:"stop_sequence"`
	Usage           Usage     `json:"usage"`
}

func messageID() string { return "msg_" + uuid.NewString() }

func usageFromChat(u *model.Usage) Usage {
	if u == nil {
		return Usage{}
	}
	return Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens, CacheReadInputTokens: u.CacheReadTokens, CacheCreationInputTokens: u.CacheWriteTokens}
}

func stopReason(reason string) string {
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "stop_sequence", "pause_turn", "refusal":
		return reason
	case "content_filter":
		return "refusal"
	default:
		return "end_turn"
	}
}

func toolInput(arguments string) (json.RawMessage, error) {
	if arguments == "" {
		arguments = "{}"
	}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(arguments), &object) != nil || object == nil {
		return nil, fmt.Errorf("upstream returned invalid tool input: expected a JSON object")
	}
	return json.RawMessage(arguments), nil
}

func FromChat(chat *model.ChatCompletionResponse, alias string) (*Response, error) {
	if chat == nil || len(chat.Choices) == 0 || chat.Choices[0].Message == nil {
		return nil, fmt.Errorf("upstream returned no assistant message")
	}
	choice := chat.Choices[0]
	response := &Response{ID: messageID(), Type: "message", Role: "assistant", Model: alias, Content: []Content{}, Usage: usageFromChat(chat.Usage)}
	text := func(s string) { response.Content = append(response.Content, Content{Type: "text", Text: &s}) }
	switch content := choice.Message.Content.(type) {
	case string:
		if content != "" {
			text(content)
		}
	case nil:
	default:
		// Normalize typed and decoded content arrays without flattening images or
		// provider-specific output into invented text.
		data, err := json.Marshal(content)
		if err != nil {
			return nil, err
		}
		var parts []model.ContentPart
		if json.Unmarshal(data, &parts) != nil {
			return nil, fmt.Errorf("unsupported upstream message content")
		}
		for _, part := range parts {
			if part.Type != "text" {
				return nil, fmt.Errorf("unsupported upstream content type %q", part.Type)
			}
			text(part.Text)
		}
	}
	for _, call := range choice.Message.ToolCalls {
		if call.ID == "" || call.Function.Name == "" {
			return nil, fmt.Errorf("upstream tool call is missing id or name")
		}
		input, err := toolInput(call.Function.Arguments)
		if err != nil {
			return nil, err
		}
		response.Content = append(response.Content, Content{Type: "tool_use", ID: call.ID, Name: call.Function.Name, Input: input})
	}
	reason := stopReason(choice.FinishReason)
	response.StopReason = &reason
	if reason == "stop_sequence" {
		response.StopSequence = choice.StopSequence
	}
	return response, nil
}
