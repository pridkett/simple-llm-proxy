package model

import "encoding/json"

// EstimatedTokens reserves input plus output capacity before generation. Native
// tokenizers differ, so TPM admission is approximate and reconciled to reported
// usage. Serialized input bytes provide a conservative estimate for text input.
func (r *ChatCompletionRequest) EstimatedTokens() int {
	data, _ := json.Marshal(struct {
		Messages []Message `json:"messages"`
		Tools    []Tool    `json:"tools,omitempty"`
	}{r.Messages, r.Tools})
	output := 4096
	if r.MaxCompletionTokens != nil {
		output = *r.MaxCompletionTokens
	} else if r.MaxTokens != nil {
		output = *r.MaxTokens
	}
	if output < 0 {
		output = 0
	}
	return len(data) + output
}
func (r *EmbeddingsRequest) EstimatedTokens() int { data, _ := json.Marshal(r.Input); return len(data) }
