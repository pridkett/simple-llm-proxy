package gemini

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

const defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// Provider implements the Gemini AI Studio provider.
// It translates OpenAI-format requests to Gemini GenerateContent format
// and translates Gemini responses back to OpenAI format.
type Provider struct {
	apiKey         string
	baseURL        string
	client         *http.Client
	safetySettings []geminiSafetySetting
}

// --- Gemini-native request types ---

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
	SafetySettings    []geminiSafetySetting   `json:"safetySettings,omitempty"`
	Tools             []geminiTool            `json:"tools,omitempty"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	TopK            *int     `json:"topK,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type geminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiFunctionDeclaration struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// --- Gemini-native response types ---

type geminiResponse struct {
	Candidates    []geminiCandidate    `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content       *geminiContent       `json:"content,omitempty"`
	FinishReason  string               `json:"finishReason,omitempty"`
	SafetyRatings []geminiSafetyRating `json:"safetyRatings,omitempty"`
}

type geminiSafetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// New creates a new Gemini provider from the given options.
func New(opts provider.ProviderOptions) provider.Provider {
	baseURL := defaultBaseURL
	if opts.APIBase != "" {
		baseURL = strings.TrimSuffix(opts.APIBase, "/")
	}
	var settings []geminiSafetySetting
	for _, s := range opts.SafetySettings {
		settings = append(settings, geminiSafetySetting{
			Category:  s.Category,
			Threshold: s.Threshold,
		})
	}
	return &Provider{
		apiKey:         opts.APIKey,
		baseURL:        baseURL,
		client:         &http.Client{},
		safetySettings: settings,
	}
}

func (p *Provider) Name() string {
	return "gemini"
}

func (p *Provider) SupportsEmbeddings() bool {
	return false
}

func (p *Provider) Embeddings(_ context.Context, _ *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, fmt.Errorf("gemini: embeddings not supported")
}

// ChatCompletion performs a non-streaming chat completion against the Gemini API.
func (p *Provider) ChatCompletion(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	gReq := buildGeminiRequest(req, p.safetySettings)

	body, err := json.Marshal(gReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshaling request: %w", err)
	}

	url := p.baseURL + "/models/" + req.Model + ":generateContent"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini: reading response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &provider.RateLimitError{
			Provider:   p.Name(),
			RetryAfter: provider.ParseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode != http.StatusOK {
		var apiErr geminiError
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("gemini error (status %d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("gemini error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return nil, fmt.Errorf("gemini: unmarshaling response: %w", err)
	}

	return translateGeminiResponse(&gemResp, req.Model), nil
}

// ChatCompletionStream performs a streaming chat completion against the Gemini API.
// Gemini streaming uses the ?alt=sse query parameter and returns SSE data lines
// containing geminiResponse JSON. The stream ends on connection close (EOF),
// not a [DONE] sentinel.
func (p *Provider) ChatCompletionStream(ctx context.Context, req *model.ChatCompletionRequest) (provider.Stream, error) {
	gReq := buildGeminiRequest(req, p.safetySettings)

	body, err := json.Marshal(gReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshaling request: %w", err)
	}

	url := p.baseURL + "/models/" + req.Model + ":streamGenerateContent?alt=sse"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: making request: %w", err)
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
		var apiErr geminiError
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("gemini error (status %d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("gemini error (status %d): %s", resp.StatusCode, string(respBody))
	}

	chunks := make(chan *model.StreamChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					errs <- err
				}
				return // stream ends on EOF (no [DONE] sentinel for Gemini)
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")

			var gemResp geminiResponse
			if err := json.Unmarshal([]byte(data), &gemResp); err != nil {
				errs <- fmt.Errorf("gemini: unmarshaling stream chunk: %w", err)
				return
			}

			chunk := translateGeminiStreamChunk(&gemResp, req.Model)

			select {
			case chunks <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return provider.NewStream(chunks, errs, func() { resp.Body.Close() }), nil
}

// buildGeminiRequest translates an OpenAI ChatCompletionRequest to a Gemini-native request.
func buildGeminiRequest(req *model.ChatCompletionRequest, safetySettings []geminiSafetySetting) *geminiRequest {
	gReq := &geminiRequest{}

	// Collect system messages into systemInstruction
	var systemText string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			text := extractTextContent(msg.Content)
			if systemText != "" {
				systemText += "\n\n"
			}
			systemText += text
		}
	}
	if systemText != "" {
		gReq.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: systemText}},
		}
	}

	// Convert non-system messages to Gemini contents
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			continue
		}

		switch msg.Role {
		case "user":
			gReq.Contents = append(gReq.Contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: extractTextContent(msg.Content)}},
			})
		case "assistant":
			content := geminiContent{Role: "model"}
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					content.Parts = append(content.Parts, geminiPart{
						FunctionCall: &geminiFunctionCall{
							Name: tc.Function.Name,
							Args: json.RawMessage(tc.Function.Arguments),
						},
					})
				}
			} else {
				content.Parts = append(content.Parts, geminiPart{
					Text: extractTextContent(msg.Content),
				})
			}
			gReq.Contents = append(gReq.Contents, content)
		case "tool":
			// Tool result messages map to user role with functionResponse
			name := msg.Name
			responseText := extractTextContent(msg.Content)
			// Wrap response text in JSON if it is not already valid JSON
			var responseJSON json.RawMessage
			if json.Valid([]byte(responseText)) {
				responseJSON = json.RawMessage(responseText)
			} else {
				responseJSON, _ = json.Marshal(map[string]string{"result": responseText})
			}
			gReq.Contents = append(gReq.Contents, geminiContent{
				Role: "user",
				Parts: []geminiPart{
					{
						FunctionResponse: &geminiFunctionResponse{
							Name:     name,
							Response: responseJSON,
						},
					},
				},
			})
		}
	}

	// Build generationConfig from request parameters
	hasConfig := req.Temperature != nil || req.TopP != nil || req.MaxTokens != nil || len(req.Stop) > 0
	if hasConfig {
		gc := &geminiGenerationConfig{}
		if req.Temperature != nil {
			gc.Temperature = req.Temperature
		}
		if req.TopP != nil {
			gc.TopP = req.TopP
		}
		if req.MaxTokens != nil {
			gc.MaxOutputTokens = req.MaxTokens
		}
		if len(req.Stop) > 0 {
			gc.StopSequences = req.Stop
		}
		gReq.GenerationConfig = gc
	}

	// Convert tools to Gemini functionDeclarations
	if len(req.Tools) > 0 {
		var declarations []geminiFunctionDeclaration
		for _, tool := range req.Tools {
			if tool.Type == "function" {
				declarations = append(declarations, geminiFunctionDeclaration{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters:  tool.Function.Parameters,
				})
			}
		}
		if len(declarations) > 0 {
			gReq.Tools = []geminiTool{{FunctionDeclarations: declarations}}
		}
	}

	// Include safety settings from provider options (D-16)
	if len(safetySettings) > 0 {
		gReq.SafetySettings = safetySettings
	}

	return gReq
}

// translateGeminiResponse translates a Gemini response to an OpenAI ChatCompletionResponse.
func translateGeminiResponse(resp *geminiResponse, modelName string) *model.ChatCompletionResponse {
	result := &model.ChatCompletionResponse{
		ID:      fmt.Sprintf("gemini-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   modelName,
	}

	for i, candidate := range resp.Candidates {
		choice := model.Choice{
			Index:        i,
			FinishReason: mapFinishReason(candidate.FinishReason),
		}

		msg := &model.Message{
			Role: "assistant",
		}

		if candidate.Content != nil {
			var textContent string
			var toolCalls []model.ToolCall
			callIdx := 0

			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					textContent += part.Text
				}
				if part.FunctionCall != nil {
					toolCalls = append(toolCalls, model.ToolCall{
						ID:   fmt.Sprintf("call_%d", callIdx),
						Type: "function",
						Function: model.FunctionCall{
							Name:      part.FunctionCall.Name,
							Arguments: string(part.FunctionCall.Args),
						},
					})
					callIdx++
				}
			}

			msg.Content = textContent
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
				// Set finish_reason to "tool_calls" when tool calls present
				// unless already "content_filter" (from SAFETY)
				if choice.FinishReason != "content_filter" {
					choice.FinishReason = "tool_calls"
				}
			}
		} else {
			// nil content (e.g., SAFETY block) - create empty message
			msg.Content = ""
		}

		choice.Message = msg
		result.Choices = append(result.Choices, choice)
	}

	// Map usage metadata
	if resp.UsageMetadata != nil {
		result.Usage = &model.Usage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		}
	}

	return result
}

// translateGeminiStreamChunk translates a Gemini streaming response to an OpenAI StreamChunk.
func translateGeminiStreamChunk(resp *geminiResponse, modelName string) *model.StreamChunk {
	chunk := &model.StreamChunk{
		ID:      fmt.Sprintf("gemini-%d", time.Now().UnixNano()),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   modelName,
	}

	for i, candidate := range resp.Candidates {
		choice := model.Choice{
			Index:        i,
			FinishReason: mapFinishReason(candidate.FinishReason),
		}

		delta := &model.Delta{}

		if candidate.Content != nil {
			delta.Role = candidate.Content.Role
			if delta.Role == "model" {
				delta.Role = "assistant"
			}

			var textContent string
			var toolCalls []model.ToolCall
			callIdx := 0

			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					textContent += part.Text
				}
				if part.FunctionCall != nil {
					toolCalls = append(toolCalls, model.ToolCall{
						ID:   fmt.Sprintf("call_%d", callIdx),
						Type: "function",
						Function: model.FunctionCall{
							Name:      part.FunctionCall.Name,
							Arguments: string(part.FunctionCall.Args),
						},
					})
					callIdx++
				}
			}

			delta.Content = textContent
			if len(toolCalls) > 0 {
				delta.ToolCalls = toolCalls
				if choice.FinishReason != "content_filter" {
					choice.FinishReason = "tool_calls"
				}
			}
		}

		choice.Delta = delta
		chunk.Choices = append(chunk.Choices, choice)
	}

	// Map usage metadata (usually present in final chunk)
	if resp.UsageMetadata != nil {
		chunk.Usage = &model.Usage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		}
	}

	return chunk
}

// extractTextContent extracts text from a message content field,
// which can be either a string or a []ContentPart.
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

// mapFinishReason translates Gemini finish reasons to OpenAI finish reasons.
func mapFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "RECITATION":
		return "stop"
	case "MALFORMED_FUNCTION_CALL":
		return "stop"
	default:
		return "stop"
	}
}

func init() {
	provider.Register("gemini", func(opts provider.ProviderOptions) provider.Provider {
		return New(opts)
	})
}
