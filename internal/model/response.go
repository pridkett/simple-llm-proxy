package model

// ChatCompletionResponse represents an OpenAI-compatible chat completion response.
type ChatCompletionResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             *Usage   `json:"usage,omitempty"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

// Choice represents a completion choice.
type Choice struct {
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Delta   `json:"delta,omitempty"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

// Delta represents a streaming delta.
type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Usage represents token usage.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// EmbeddingsResponse represents an OpenAI-compatible embeddings response.
type EmbeddingsResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  *Usage          `json:"usage,omitempty"`
}

// EmbeddingData represents a single embedding.
type EmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

// ModelsResponse represents the /v1/models response.
type ModelsResponse struct {
	Object string       `json:"object"`
	Data   []ModelInfo  `json:"data"`
}

// ModelInfo represents a model in the models list.
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// CostsInfo holds cost and context window data for a model.
// All numeric fields are zero-valued when no cost mapping is available.
type CostsInfo struct {
	MaxTokens                       int     `json:"max_tokens"`
	MaxInputTokens                  int     `json:"max_input_tokens"`
	MaxOutputTokens                 int     `json:"max_output_tokens"`
	InputCostPerToken               float64 `json:"input_cost_per_token"`
	OutputCostPerToken              float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost         float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost     float64 `json:"cache_creation_input_token_cost"`
	LiteLLMProvider                 string  `json:"litellm_provider,omitempty"`
	Mode                            string  `json:"mode,omitempty"`
	SupportsFunctionCalling         bool    `json:"supports_function_calling"`
	SupportsParallelFunctionCalling bool    `json:"supports_parallel_function_calling"`
	SupportsVision                  bool    `json:"supports_vision"`
	// Source indicates how costs were resolved:
	// "auto" = matched via deployment actual_model, "override" = cost map key override,
	// "custom" = user-defined custom spec, "" = not found.
	Source     string `json:"source,omitempty"`
	CostMapKey string `json:"cost_map_key,omitempty"`
}

// ModelDetailResponse is the response for GET /v1/models/{model}.
// It extends the basic model info with cost and capability data.
type ModelDetailResponse struct {
	ID      string    `json:"id"`
	Object  string    `json:"object"`
	Created int64     `json:"created"`
	OwnedBy string    `json:"owned_by"`
	Costs   CostsInfo `json:"costs"`
}

// StreamChunk represents a streaming response chunk.
type StreamChunk struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}
