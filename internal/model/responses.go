package model

// ResponsesRequest represents an OpenAI Responses API request (POST /v1/responses).
// Input accepts either a plain string or a structured array of input items
// (OpenAI's union type) — the proxy passes it through largely unmodified.
type ResponsesRequest struct {
	Model              string   `json:"model"`
	Input              any      `json:"input"`
	Instructions       string   `json:"instructions,omitempty"`
	PreviousResponseID *string  `json:"previous_response_id,omitempty"`
	Background         bool     `json:"background,omitempty"`
	Stream             bool     `json:"stream,omitempty"`
	Temperature        *float64 `json:"temperature,omitempty"`
	TopP               *float64 `json:"top_p,omitempty"`
	MaxOutputTokens    *int     `json:"max_output_tokens,omitempty"`
	Tools              []Tool   `json:"tools,omitempty"`
	ToolChoice         any      `json:"tool_choice,omitempty"`
	ParallelToolCalls  *bool    `json:"parallel_tool_calls,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	User               string   `json:"user,omitempty"`
}

// ResponsesResponse represents an OpenAI Responses API response, returned both
// from a synchronous create call and from GET /v1/responses/{id}.
type ResponsesResponse struct {
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
// forwards the raw item rather than fully modeling every OpenAI item variant.
type ResponseOutputItem struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Role    string          `json:"role,omitempty"`
	Status  string          `json:"status,omitempty"`
	Content []ResponseContentPart `json:"content,omitempty"`

	// Function/tool call fields (Type == "function_call").
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ResponseContentPart is a single content part within a ResponseOutputItem
// (e.g. {"type": "output_text", "text": "..."}).
type ResponseContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ResponsesStreamEvent represents one SSE event from a streaming Responses API
// call (e.g. "response.created", "response.output_text.delta", "response.completed").
// The proxy forwards these largely verbatim to the client.
type ResponsesStreamEvent struct {
	Type           string             `json:"type"`
	SequenceNumber int                `json:"sequence_number,omitempty"`
	Response       *ResponsesResponse `json:"response,omitempty"`
	Delta          string             `json:"delta,omitempty"`
	ItemID         string             `json:"item_id,omitempty"`
	OutputIndex    *int               `json:"output_index,omitempty"`
}
