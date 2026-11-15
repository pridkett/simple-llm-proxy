package responsesbridge

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

func newID(prefix string) string {
	return prefix + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func messageItem(id, text, status string) map[string]any {
	return map[string]any{
		"id": id, "type": "message", "role": "assistant", "status": status,
		"content": []any{textPart(text)},
	}
}

func textPart(text string) map[string]any {
	return map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
}

func functionItem(id, callID, name, arguments, status string) map[string]any {
	return map[string]any{
		"id": id, "type": "function_call", "status": status,
		"call_id": callID, "name": name, "arguments": arguments,
	}
}

func usageFields(u *model.Usage) map[string]any {
	if u == nil {
		return nil
	}
	total := u.TotalTokens
	if total == 0 {
		total = u.PromptTokens + u.CompletionTokens
	}
	out := map[string]any{
		"input_tokens":          u.PromptTokens,
		"output_tokens":         u.CompletionTokens,
		"total_tokens":          total,
		"input_tokens_details":  map[string]any{"cached_tokens": u.CacheReadTokens},
		"output_tokens_details": map[string]any{"reasoning_tokens": 0},
	}
	// Keep the proxy's cache counters so cost accounting matches the chat route.
	if u.CacheReadTokens > 0 {
		out["cache_read_tokens"] = u.CacheReadTokens
	}
	if u.CacheWriteTokens > 0 {
		out["cache_write_tokens"] = u.CacheWriteTokens
	}
	return out
}

// envelope describes one translated response across its lifetime.
type envelope struct {
	id      string
	created int64
	model   string
	echo    map[string]any
}

func newEnvelope(modelName string, echo map[string]any) *envelope {
	return &envelope{id: newID("resp"), created: time.Now().Unix(), model: modelName, echo: echo}
}

// response renders the Responses object. finish is the chat finish reason, or
// empty while the response is still in progress.
func (e *envelope) response(finish string, output []map[string]any, usage *model.Usage) (*model.ResponsesResponse, error) {
	body := maps.Clone(e.echo)
	if body == nil {
		body = map[string]any{}
	}
	body["id"] = e.id
	body["object"] = "response"
	body["created_at"] = e.created
	body["model"] = e.model
	body["output"] = output
	switch finish {
	case "":
		body["status"] = "in_progress"
	case "length":
		body["status"] = "incomplete"
		body["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
	case "content_filter":
		body["status"] = "incomplete"
		body["incomplete_details"] = map[string]any{"reason": "content_filter"}
	default:
		body["status"] = "completed"
		body["completed_at"] = time.Now().Unix()
	}
	if u := usageFields(usage); u != nil {
		body["usage"] = u
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var out model.ResponsesResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func contentText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []model.ContentPart:
		var b strings.Builder
		for _, part := range c {
			b.WriteString(part.Text)
		}
		return b.String()
	case []any:
		var b strings.Builder
		for _, part := range c {
			if m, ok := part.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					b.WriteString(text)
				}
			}
		}
		return b.String()
	}
	return ""
}

func arguments(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "{}"
	}
	return raw
}

// fromChatResponse converts a completed chat response into a Responses object.
func fromChatResponse(chat *model.ChatCompletionResponse, env *envelope) (*model.ResponsesResponse, error) {
	if chat == nil || len(chat.Choices) == 0 || chat.Choices[0].Message == nil {
		return nil, fmt.Errorf("upstream returned no assistant message")
	}
	if chat.Model != "" {
		env.model = chat.Model
	}
	choice := chat.Choices[0]
	var output []map[string]any
	if text := contentText(choice.Message.Content); text != "" {
		output = append(output, messageItem(newID("msg"), text, "completed"))
	}
	for _, call := range choice.Message.ToolCalls {
		if call.ID == "" || call.Function.Name == "" {
			return nil, fmt.Errorf("upstream tool call is missing id or name")
		}
		output = append(output, functionItem(newID("fc"), call.ID, call.Function.Name, arguments(call.Function.Arguments), "completed"))
	}
	if output == nil {
		output = []map[string]any{}
	}
	finish := choice.FinishReason
	if finish == "" {
		finish = "stop"
	}
	return env.response(finish, output, chat.Usage)
}
