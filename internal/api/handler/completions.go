package handler

import (
	"encoding/json"
	"net/http"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

// Completions handles POST /v1/completions requests (legacy endpoint).
// This is a placeholder for future implementation.
func Completions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		model.WriteError(w, model.ErrBadRequest("legacy completions endpoint not implemented; use /v1/chat/completions instead"))
	}
}

// CompletionResponse represents a legacy completion response.
type CompletionResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []CompletionChoice  `json:"choices"`
	Usage   *model.Usage        `json:"usage,omitempty"`
}

// CompletionChoice represents a legacy completion choice.
type CompletionChoice struct {
	Text         string `json:"text"`
	Index        int    `json:"index"`
	FinishReason string `json:"finish_reason,omitempty"`
}

// Unused but kept for type reference
var _ = json.Marshal
