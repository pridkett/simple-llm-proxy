package model

import "encoding/json"

// ResponsesRequest represents an OpenAI Responses API request (POST /v1/responses).
// Input accepts either a plain string or a structured array of input items
// (OpenAI's union type) — the proxy passes it through largely unmodified.
type ResponsesRequest struct {
	rawFields          map[string]json.RawMessage
	Model              string            `json:"model"`
	Input              any               `json:"input"`
	Instructions       string            `json:"instructions,omitempty"`
	PreviousResponseID *string           `json:"previous_response_id,omitempty"`
	Background         bool              `json:"background,omitempty"`
	Stream             bool              `json:"stream,omitempty"`
	Temperature        *float64          `json:"temperature,omitempty"`
	TopP               *float64          `json:"top_p,omitempty"`
	MaxOutputTokens    *int              `json:"max_output_tokens,omitempty"`
	Tools              []Tool            `json:"tools,omitempty"`
	ToolChoice         any               `json:"tool_choice,omitempty"`
	ParallelToolCalls  *bool             `json:"parallel_tool_calls,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	User               string            `json:"user,omitempty"`
}

// ResponsesResponse represents an OpenAI Responses API response, returned both
// from a synchronous create call and from GET /v1/responses/{id}.
type ResponsesResponse struct {
	rawFields          map[string]json.RawMessage
	ProxyAccounting    any                  `json:"proxy_accounting,omitempty"`
	ID                 string               `json:"id"`
	Object             string               `json:"object"`
	Model              string               `json:"model,omitempty"`
	Status             string               `json:"status"` // queued|in_progress|completed|failed|cancelled|incomplete
	CreatedAt          int64                `json:"created_at"`
	CompletedAt        *int64               `json:"completed_at,omitempty"`
	Background         bool                 `json:"background,omitempty"`
	Output             []ResponseOutputItem `json:"output,omitempty"`
	OutputText         string               `json:"output_text,omitempty"`
	Usage              *Usage               `json:"usage,omitempty"`
	Error              *ErrorDetail         `json:"error,omitempty"`
	PreviousResponseID *string              `json:"previous_response_id,omitempty"`
	Metadata           map[string]string    `json:"metadata,omitempty"`
}

// IsTerminal reports whether the response's status will not change further
// without external action (i.e. no more polling is needed).
func (r *ResponsesResponse) IsTerminal() bool {
	switch r.Status {
	case "completed", "failed", "cancelled", "incomplete":
		return true
	default:
		return false
	}
}

// ResponseOutputItem is one entry in ResponsesResponse.Output. Type discriminates
// the shape (e.g. "message", "reasoning", "function_call"); the proxy stores and
// preserves extension fields such as the result of an image generation call.
type ResponseOutputItem struct {
	rawFields map[string]json.RawMessage
	ID        string                `json:"id,omitempty"`
	Type      string                `json:"type"`
	Role      string                `json:"role,omitempty"`
	Status    string                `json:"status,omitempty"`
	Content   []ResponseContentPart `json:"content,omitempty"`

	// Function/tool call fields (Type == "function_call").
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
}

// ResponseContentPart is a single content part within a ResponseOutputItem
// (e.g. {"type": "output_text", "text": "..."}).
type ResponseContentPart struct {
	rawFields map[string]json.RawMessage
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Refusal   string `json:"refusal,omitempty"`
}

// ResponsesStreamEvent represents one SSE event from a streaming Responses API
// call (e.g. "response.created", "response.output_text.delta", "response.completed").
// The proxy forwards these largely verbatim to the client.
type ResponsesStreamEvent struct {
	rawFields      map[string]json.RawMessage
	Type           string              `json:"type"`
	SequenceNumber int                 `json:"sequence_number,omitempty"`
	Response       *ResponsesResponse  `json:"response,omitempty"`
	Delta          string              `json:"delta,omitempty"`
	ItemID         string              `json:"item_id,omitempty"`
	OutputIndex    *int                `json:"output_index,omitempty"`
	ContentIndex   *int                `json:"content_index,omitempty"`
	Item           *ResponseOutputItem `json:"item,omitempty"`
}

func (x *ResponsesRequest) UnmarshalJSON(data []byte) error {
	type wire ResponsesRequest
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}

func (x ResponsesRequest) MarshalJSON() ([]byte, error) {
	type wire ResponsesRequest
	return writeObject(wire(x), x.rawFields)
}

func (x *ResponsesResponse) UnmarshalJSON(data []byte) error {
	type wire ResponsesResponse
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}

func (x ResponsesResponse) MarshalJSON() ([]byte, error) {
	type wire ResponsesResponse
	return writeObject(wire(x), x.rawFields)
}

func (x *ResponseOutputItem) UnmarshalJSON(data []byte) error {
	type wire ResponseOutputItem
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}

func (x ResponseOutputItem) MarshalJSON() ([]byte, error) {
	type wire ResponseOutputItem
	return writeObject(wire(x), x.rawFields)
}

func (x *ResponseContentPart) UnmarshalJSON(data []byte) error {
	type wire ResponseContentPart
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}

func (x ResponseContentPart) MarshalJSON() ([]byte, error) {
	type wire ResponseContentPart
	return writeObject(wire(x), x.rawFields)
}

func (x *ResponsesStreamEvent) UnmarshalJSON(data []byte) error {
	type wire ResponsesStreamEvent
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}

func (x ResponsesStreamEvent) MarshalJSON() ([]byte, error) {
	type wire ResponsesStreamEvent
	return writeObject(wire(x), x.rawFields)
}
