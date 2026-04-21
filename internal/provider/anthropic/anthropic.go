package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

const (
	defaultBaseURL   = "https://api.anthropic.com/v1"
	anthropicVersion = "2023-06-01"
	defaultMaxTokens = 4096
)

// Provider implements the Anthropic provider.
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// New creates a new Anthropic provider.
func New(opts provider.ProviderOptions) provider.Provider {
	baseURL := defaultBaseURL
	if opts.APIBase != "" {
		baseURL = strings.TrimSuffix(opts.APIBase, "/")
	}
	return &Provider{
		apiKey:  opts.APIKey,
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

func (p *Provider) Name() string {
	return "anthropic"
}

func (p *Provider) SupportsEmbeddings() bool {
	return false
}

// Anthropic request/response types

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	StopSeq     []string           `json:"stop_sequences,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContent
}

type anthropicContent struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     any    `json:"input,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type anthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []anthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   string             `json:"stop_reason"`
	StopSequence string             `json:"stop_sequence,omitempty"`
	Usage        anthropicUsage     `json:"usage"`
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type anthropicStreamEvent struct {
	Type         string             `json:"type"`
	Index        int                `json:"index,omitempty"`
	Delta        *anthropicDelta    `json:"delta,omitempty"`
	ContentBlock *anthropicContent  `json:"content_block,omitempty"`
	Message      *anthropicResponse `json:"message,omitempty"`
	Usage        *anthropicUsage    `json:"usage,omitempty"`
}

type anthropicDelta struct {
	Type         string `json:"type,omitempty"`
	Text         string `json:"text,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	PartialJSON  string `json:"partial_json,omitempty"`
}

type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *Provider) ChatCompletion(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	anthReq, err := p.translateRequest(req)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(anthReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &provider.RateLimitError{
			Provider:   p.Name(),
			RetryAfter: provider.ParseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode != http.StatusOK {
		var apiErr anthropicError
		if err := json.Unmarshal(respBody, &apiErr); err == nil {
			return nil, fmt.Errorf("anthropic error (status %d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("anthropic error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var anthResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthResp); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return p.translateResponse(&anthResp, req.Model), nil
}

func (p *Provider) ChatCompletionStream(ctx context.Context, req *model.ChatCompletionRequest) (provider.Stream, error) {
	anthReq, err := p.translateRequest(req)
	if err != nil {
		return nil, err
	}
	anthReq.Stream = true

	body, err := json.Marshal(anthReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := provider.ParseRetryAfter(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		return nil, &provider.RateLimitError{
			Provider:   p.Name(),
			RetryAfter: retryAfter,
		}
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		var apiErr anthropicError
		if err := json.Unmarshal(respBody, &apiErr); err == nil {
			return nil, fmt.Errorf("anthropic error (status %d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("anthropic error (status %d): %s", resp.StatusCode, string(respBody))
	}

	chunks := make(chan *model.StreamChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)
		responseID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())

		var inputTokens int      // accumulated from message_start event
		var cacheReadTokens int  // accumulated from message_start event
		var cacheWriteTokens int // accumulated from message_start event

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					errs <- err
				}
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}

			var event anthropicStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue // Skip malformed events
			}

			chunk := p.translateStreamEvent(&event, responseID, req.Model, &inputTokens, &cacheReadTokens, &cacheWriteTokens)
			if chunk != nil {
				select {
				case chunks <- chunk:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return provider.NewStream(chunks, errs, func() { resp.Body.Close() }), nil
}

func (p *Provider) Embeddings(ctx context.Context, req *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, fmt.Errorf("anthropic does not support embeddings")
}

func (p *Provider) translateRequest(req *model.ChatCompletionRequest) (*anthropicRequest, error) {
	anthReq := &anthropicRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		StopSeq:     req.Stop,
	}

	// Handle max_tokens - Anthropic requires it
	if req.MaxTokens != nil {
		anthReq.MaxTokens = *req.MaxTokens
	} else {
		anthReq.MaxTokens = defaultMaxTokens
	}

	// Extract system message and convert messages
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			content := extractTextContent(msg.Content)
			if anthReq.System != "" {
				anthReq.System += "\n\n"
			}
			anthReq.System += content
		} else {
			anthMsg := anthropicMessage{
				Role:    mapRole(msg.Role),
				Content: convertContent(msg),
			}
			anthReq.Messages = append(anthReq.Messages, anthMsg)
		}
	}

	// Convert tools if present
	if len(req.Tools) > 0 {
		for _, tool := range req.Tools {
			if tool.Type == "function" {
				anthReq.Tools = append(anthReq.Tools, anthropicTool{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					InputSchema: tool.Function.Parameters,
				})
			}
		}
	}

	return anthReq, nil
}

func (p *Provider) translateResponse(resp *anthropicResponse, requestModel string) *model.ChatCompletionResponse {
	var content string
	var toolCalls []model.ToolCall

	for _, c := range resp.Content {
		switch c.Type {
		case "text":
			content += c.Text
		case "tool_use":
			inputJSON, _ := json.Marshal(c.Input)
			toolCalls = append(toolCalls, model.ToolCall{
				ID:   c.ID,
				Type: "function",
				Function: model.FunctionCall{
					Name:      c.Name,
					Arguments: string(inputJSON),
				},
			})
		}
	}

	return &model.ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   requestModel,
		Choices: []model.Choice{
			{
				Index: 0,
				Message: &model.Message{
					Role:      "assistant",
					Content:   content,
					ToolCalls: toolCalls,
				},
				FinishReason: mapStopReason(resp.StopReason),
			},
		},
		Usage: &model.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
			CacheReadTokens:  resp.Usage.CacheReadInputTokens,     // Anthropic cache read hits
			CacheWriteTokens: resp.Usage.CacheCreationInputTokens, // Anthropic cache writes
		},
	}
}

// translateStreamEvent converts an Anthropic SSE event to an OpenAI StreamChunk.
// inputTokens is a pointer to the accumulated input token count from message_start;
// cacheReadTokens and cacheWriteTokens are pointers to cache token counts from message_start.
// All three pointers are updated in-place when processing message_start and read when processing message_delta.
func (p *Provider) translateStreamEvent(event *anthropicStreamEvent, id, requestModel string, inputTokens *int, cacheReadTokens *int, cacheWriteTokens *int) *model.StreamChunk {
	switch event.Type {
	case "content_block_delta":
		if event.Delta != nil && event.Delta.Text != "" {
			return &model.StreamChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   requestModel,
				Choices: []model.Choice{
					{
						Index: 0,
						Delta: &model.Delta{
							Content: event.Delta.Text,
						},
					},
				},
			}
		}
	case "message_start":
		// Extract input token count and cache token counts for later use in message_delta.
		if event.Message != nil && inputTokens != nil {
			*inputTokens = event.Message.Usage.InputTokens
			if cacheReadTokens != nil {
				*cacheReadTokens = event.Message.Usage.CacheReadInputTokens
			}
			if cacheWriteTokens != nil {
				*cacheWriteTokens = event.Message.Usage.CacheCreationInputTokens
			}
		}
		return &model.StreamChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   requestModel,
			Choices: []model.Choice{
				{
					Index: 0,
					Delta: &model.Delta{
						Role: "assistant",
					},
				},
			},
		}
	case "message_delta":
		if event.Delta != nil && event.Delta.StopReason != "" {
			outputTokens := 0
			if event.Usage != nil {
				outputTokens = event.Usage.OutputTokens
			}
			// Populate Usage when token counts are available.
			// input tokens and cache tokens were captured from the preceding message_start event.
			var usage *model.Usage
			in := 0
			if inputTokens != nil {
				in = *inputTokens
			}
			cr := 0
			if cacheReadTokens != nil {
				cr = *cacheReadTokens
			}
			cw := 0
			if cacheWriteTokens != nil {
				cw = *cacheWriteTokens
			}
			if outputTokens > 0 || in > 0 {
				usage = &model.Usage{
					PromptTokens:     in,
					CompletionTokens: outputTokens,
					TotalTokens:      in + outputTokens,
					CacheReadTokens:  cr, // from message_start event
					CacheWriteTokens: cw, // from message_start event
				}
			}
			return &model.StreamChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   requestModel,
				Choices: []model.Choice{
					{
						Index:        0,
						Delta:        &model.Delta{},
						FinishReason: mapStopReason(event.Delta.StopReason),
					},
				},
				Usage: usage,
			}
		}
	}
	return nil
}

func extractTextContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var text string
		for _, part := range c {
			if m, ok := part.(map[string]any); ok {
				if t, ok := m["type"].(string); ok && t == "text" {
					if txt, ok := m["text"].(string); ok {
						text += txt
					}
				}
			}
		}
		return text
	}
	return ""
}

func convertContent(msg model.Message) any {
	// Handle tool results
	if msg.Role == "tool" && msg.ToolCallID != "" {
		return []anthropicContent{
			{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   extractTextContent(msg.Content),
			},
		}
	}

	// Handle assistant messages with tool calls
	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		var content []anthropicContent
		text := extractTextContent(msg.Content)
		if text != "" {
			content = append(content, anthropicContent{
				Type: "text",
				Text: text,
			})
		}
		for _, tc := range msg.ToolCalls {
			var input any
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
			content = append(content, anthropicContent{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
		return content
	}

	// Simple text content
	return extractTextContent(msg.Content)
}

func mapRole(role string) string {
	switch role {
	case "tool":
		return "user"
	default:
		return role
	}
}

func mapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}

func init() {
	provider.Register("anthropic", func(opts provider.ProviderOptions) provider.Provider {
		return New(opts)
	})
}
