// Package anthropic translates the client-facing Messages protocol to and from
// the proxy's provider-independent chat types.
package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

type Request struct {
	Model         string          `json:"model"`
	MaxTokens     int             `json:"max_tokens"`
	Messages      []Message       `json:"messages"`
	System        json.RawMessage `json:"system,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Tools         []Tool          `json:"tools,omitempty"`
	ToolChoice    *ToolChoice     `json:"tool_choice,omitempty"`
	Metadata      *struct {
		UserID string `json:"user_id,omitempty"`
	} `json:"metadata,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type Block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Source    *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type,omitempty"`
		Data      string `json:"data,omitempty"`
		URL       string `json:"url,omitempty"`
	} `json:"source,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

type Tool struct {
	Type         string          `json:"type,omitempty"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	Strict       *bool           `json:"strict,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

type ToolChoice struct {
	Type                   string `json:"type"`
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse *bool  `json:"disable_parallel_tool_use,omitempty"`
}

// Reject unsupported options rather than forwarding vendor-specific fields to
// unrelated providers. Session attribution is removed before this decoder runs.
func (r *Request) UnmarshalJSON(data []byte) error {
	type plain Request
	return strictDecode(data, (*plain)(r))
}

func strictDecode(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

func blocks(raw json.RawMessage) ([]Block, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil && string(raw) != "null" {
		return []Block{{Type: "text", Text: text}}, nil
	}
	var result []Block
	if err := strictDecode(raw, &result); err != nil || len(result) == 0 {
		return nil, fmt.Errorf("content must be a string or a nonempty array of supported content blocks")
	}
	return result, nil
}

func (r *Request) ChatRequest() (*model.ChatCompletionRequest, error) {
	if strings.TrimSpace(r.Model) == "" || r.MaxTokens <= 0 || len(r.Messages) == 0 {
		return nil, fmt.Errorf("model, positive max_tokens, and nonempty messages are required")
	}
	for name, value := range map[string]*float64{"temperature": r.Temperature, "top_p": r.TopP} {
		if value != nil && (*value < 0 || *value > 1) {
			return nil, fmt.Errorf("%s must be between 0 and 1", name)
		}
	}
	for _, stop := range r.StopSequences {
		if stop == "" {
			return nil, fmt.Errorf("stop_sequences must contain nonempty strings")
		}
	}
	chat := &model.ChatCompletionRequest{Model: r.Model, MaxTokens: &r.MaxTokens, Stream: r.Stream, Temperature: r.Temperature, TopP: r.TopP, Stop: r.StopSequences}
	if r.Stream {
		chat.StreamOptions = &model.StreamOptions{IncludeUsage: true}
	}
	if r.Metadata != nil {
		chat.User = r.Metadata.UserID
	}
	if len(r.System) > 0 && string(r.System) != "null" {
		parts, err := blocks(r.System)
		if err != nil {
			return nil, fmt.Errorf("system: %w", err)
		}
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			if part.Type != "text" {
				return nil, fmt.Errorf("system supports only text blocks")
			}
			texts = append(texts, part.Text)
		}
		chat.Messages = append(chat.Messages, model.Message{Role: "system", Content: strings.Join(texts, "\n\n")})
	}
	for i, msg := range r.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			return nil, fmt.Errorf("messages[%d]: role must be user or assistant", i)
		}
		parts, err := blocks(msg.Content)
		if err != nil {
			return nil, fmt.Errorf("messages[%d]: %w", i, err)
		}
		out := model.Message{Role: msg.Role}
		var content []model.ContentPart
		var results []model.Message
		for _, part := range parts {
			switch part.Type {
			case "text":
				content = append(content, model.ContentPart{Type: "text", Text: part.Text})
			case "image":
				if msg.Role != "user" {
					return nil, fmt.Errorf("image blocks require user role")
				}
				image, err := imagePart(part)
				if err != nil {
					return nil, err
				}
				content = append(content, image)
			case "tool_use":
				var input map[string]json.RawMessage
				if msg.Role != "assistant" || part.ID == "" || part.Name == "" || json.Unmarshal(part.Input, &input) != nil || input == nil {
					return nil, fmt.Errorf("tool_use requires assistant role, id, name, and object input")
				}
				out.ToolCalls = append(out.ToolCalls, model.ToolCall{ID: part.ID, Type: "function", Function: model.FunctionCall{Name: part.Name, Arguments: string(part.Input)}})
			case "tool_result":
				if msg.Role != "user" || part.ToolUseID == "" {
					return nil, fmt.Errorf("tool_result requires user role and tool_use_id")
				}
				text := ""
				if len(part.Content) > 0 {
					result, err := blocks(part.Content)
					if err != nil {
						return nil, fmt.Errorf("tool_result: %w", err)
					}
					var texts []string
					for _, b := range result {
						if b.Type != "text" {
							return nil, fmt.Errorf("tool_result supports only text content")
						}
						texts = append(texts, b.Text)
					}
					text = strings.Join(texts, "\n")
				}
				if part.IsError {
					text = "Tool error: " + text
				}
				results = append(results, model.Message{Role: "tool", ToolCallID: part.ToolUseID, Content: text})
			default:
				return nil, fmt.Errorf("unsupported content block type %q", part.Type)
			}
		}
		chat.Messages = append(chat.Messages, results...)
		if len(content) > 0 || len(out.ToolCalls) > 0 {
			if len(content) == 1 && content[0].Type == "text" {
				out.Content = content[0].Text
			} else if len(content) > 0 {
				out.Content = content
			}
			chat.Messages = append(chat.Messages, out)
		}
	}
	names := map[string]bool{}
	for _, tool := range r.Tools {
		var schema map[string]json.RawMessage
		if (tool.Type != "" && tool.Type != "custom") || tool.Name == "" || names[tool.Name] || json.Unmarshal(tool.InputSchema, &schema) != nil || string(schema["type"]) != `"object"` {
			return nil, fmt.Errorf("tools require unique names and object input_schema; only client tools are supported")
		}
		names[tool.Name] = true
		chat.Tools = append(chat.Tools, model.Tool{Type: "function", Function: model.FunctionDef{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema, Strict: tool.Strict}})
	}
	if c := r.ToolChoice; c != nil {
		switch c.Type {
		case "auto", "none":
			chat.ToolChoice = c.Type
		case "any":
			if len(names) == 0 {
				return nil, fmt.Errorf("tool_choice any requires tools")
			}
			chat.ToolChoice = "required"
		case "tool":
			if !names[c.Name] {
				return nil, fmt.Errorf("tool_choice must name a defined tool")
			}
			chat.ToolChoice = map[string]any{"type": "function", "function": map[string]any{"name": c.Name}}
		default:
			return nil, fmt.Errorf("unsupported tool_choice type %q", c.Type)
		}
		if c.DisableParallelToolUse != nil {
			parallel := !*c.DisableParallelToolUse
			chat.ParallelToolCalls = &parallel
		}
	}
	return chat, nil
}

func imagePart(block Block) (model.ContentPart, error) {
	if block.Source == nil {
		return model.ContentPart{}, fmt.Errorf("image requires source")
	}
	s := block.Source
	var imageURL string
	switch s.Type {
	case "url":
		u, err := url.Parse(s.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return model.ContentPart{}, fmt.Errorf("image source URL must be http or https")
		}
		imageURL = s.URL
	case "base64":
		switch s.MediaType {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
		default:
			return model.ContentPart{}, fmt.Errorf("unsupported image media_type")
		}
		if data, err := base64.StdEncoding.DecodeString(s.Data); err != nil || len(data) == 0 {
			return model.ContentPart{}, fmt.Errorf("image source requires valid nonempty base64 data")
		}
		imageURL = "data:" + s.MediaType + ";base64," + s.Data
	default:
		return model.ContentPart{}, fmt.Errorf("unsupported image source type %q", s.Type)
	}
	return model.ContentPart{Type: "image_url", ImageURL: &model.ImageURL{URL: imageURL}}, nil
}
