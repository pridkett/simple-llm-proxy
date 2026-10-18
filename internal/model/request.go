package model

import "encoding/json"

// ChatCompletionRequest represents an OpenAI-compatible chat completion request.
type ChatCompletionRequest struct {
	rawFields           map[string]json.RawMessage
	stopString          bool
	Model               string          `json:"model"`
	Messages            []Message       `json:"messages"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	N                   *int            `json:"n,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	Stop                []string        `json:"stop,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	StreamOptions       *StreamOptions  `json:"stream_options,omitempty"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
	PresencePenalty     *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64        `json:"frequency_penalty,omitempty"`
	User                string          `json:"user,omitempty"`
	Functions           []FunctionDef   `json:"functions,omitempty"`
	FunctionCall        any             `json:"function_call,omitempty"`
	Tools               []Tool          `json:"tools,omitempty"`
	ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"`
	ToolChoice          any             `json:"tool_choice,omitempty"`
	ResponseFormat      *ResponseFormat `json:"response_format,omitempty"`
}

// Message represents a chat message.
type Message struct {
	rawFields  map[string]json.RawMessage
	Role       string     `json:"role"`
	Content    any        `json:"content"` // string or []ContentPart
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ContentPart represents a content part in a multimodal message.
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL represents an image URL in a content part.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// FunctionDef represents a function definition.
type FunctionDef struct {
	Strict      *bool `json:"strict,omitempty"`
	rawFields   map[string]json.RawMessage
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// Tool represents a tool definition.
type Tool struct {
	rawFields map[string]json.RawMessage
	Type      string      `json:"type"`
	Function  FunctionDef `json:"function"`
}

// ToolCall represents a tool call in a message.
type ToolCall struct {
	Index     *int `json:"index,omitempty"`
	rawFields map[string]json.RawMessage
	ID        string       `json:"id,omitempty"`
	Type      string       `json:"type,omitempty"`
	Function  FunctionCall `json:"function"`
}

// FunctionCall represents a function call.
type FunctionCall struct {
	rawFields map[string]json.RawMessage
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ResponseFormat specifies the response format.
type ResponseFormat struct {
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
	rawFields  map[string]json.RawMessage
	Type       string `json:"type"`
}

// EmbeddingsRequest represents an OpenAI-compatible embeddings request.
type EmbeddingsRequest struct {
	rawFields      map[string]json.RawMessage
	Model          string `json:"model"`
	Input          any    `json:"input"` // string or []string
	User           string `json:"user,omitempty"`
	Dimensions     *int   `json:"dimensions,omitempty"`
	EncodingFormat string `json:"encoding_format,omitempty"`
}

// CompletionRequest represents a legacy completion request.
type CompletionRequest struct {
	Model               string         `json:"model"`
	Prompt              any            `json:"prompt"` // string or []string
	MaxCompletionTokens *int           `json:"max_completion_tokens,omitempty"`
	StreamOptions       *StreamOptions `json:"stream_options,omitempty"`
	MaxTokens           *int           `json:"max_tokens,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	TopP                *float64       `json:"top_p,omitempty"`
	N                   *int           `json:"n,omitempty"`
	Stream              bool           `json:"stream,omitempty"`
	Stop                []string       `json:"stop,omitempty"`
	PresencePenalty     *float64       `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64       `json:"frequency_penalty,omitempty"`
	User                string         `json:"user,omitempty"`
}

// StreamOptions controls whether the upstream includes a final token usage chunk.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
	rawFields    map[string]json.RawMessage
}
