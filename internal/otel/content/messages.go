package content

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

// These types project the client protocols onto the GenAI message schema.
// Only the fields explicitly copied into a part can be exported.
type message struct {
	Role         string `json:"role"`
	Parts        []part `json:"parts"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type part struct {
	Type      string `json:"type"`
	Content   string `json:"content,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments any    `json:"arguments,omitempty"`
	Response  any    `json:"response,omitempty"`
}

type wireItem struct {
	Type       string          `json:"type"`
	Role       string          `json:"role"`
	ID         string          `json:"id"`
	CallID     string          `json:"call_id"`
	ToolUseID  string          `json:"tool_use_id"`
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Text       string          `json:"text"`
	Refusal    string          `json:"refusal"`
	Index      int             `json:"index"`
	Arguments  json.RawMessage `json:"arguments"`
	Input      json.RawMessage `json:"input"`
	Output     json.RawMessage `json:"output"`
	Content    json.RawMessage `json:"content"`
	Function   *wireItem       `json:"function"`
	LegacyCall *wireItem       `json:"function_call"`
	ToolCalls  json.RawMessage `json:"tool_calls"`
}

type messageSet struct {
	messages   []message
	remaining  int
	fieldLimit int
	truncated  bool
}

func newMessageSet(limit int) messageSet {
	return messageSet{remaining: limit - 2, fieldLimit: min(8192, limit/4)}
}

func (s *messageSet) add(role, finish string, build func(func(part) bool)) {
	m := message{Role: s.text(role), Parts: []part{}, FinishReason: s.text(finish)}
	empty, _ := json.Marshal(m)
	remaining := s.remaining - len(empty) - 1
	build(func(p part) bool {
		encoded, err := json.Marshal(p)
		if err != nil || len(encoded)+1 > remaining {
			s.truncated = true
			return false
		}
		remaining -= len(encoded) + 1
		m.Parts = append(m.Parts, p)
		return true
	})
	if len(m.Parts) != 0 {
		s.remaining = remaining
		s.messages = append(s.messages, m)
	}
}

func walkItems(raw json.RawMessage, visit func(wireItem) bool) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return true
	}
	for dec.More() {
		var item wireItem
		if err := dec.Decode(&item); err != nil || !visit(item) {
			return false
		}
	}
	return true
}

func (s *messageSet) textParts(raw json.RawMessage, put func(part) bool) bool {
	var text string
	if len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &text) == nil {
		return put(part{Type: "text", Content: s.text(text)})
	}
	return walkItems(raw, func(item wireItem) bool {
		switch item.Type {
		case "text", "input_text", "output_text":
			return put(part{Type: "text", Content: s.text(item.Text)})
		case "refusal":
			return put(part{Type: "text", Content: s.text(item.Refusal)})
		case "tool_use":
			return put(s.call(item.ID, item.Name, item.Input, false))
		case "tool_result":
			return put(part{Type: "tool_call_response", ID: s.text(item.ToolUseID), Response: s.result(item.Content)})
		default:
			return true
		}
	})
}

func (s *messageSet) call(id, name string, raw json.RawMessage, freeform bool) part {
	p := part{Type: "tool_call", ID: s.text(id), Name: s.text(name)}
	if len(raw) == 0 {
		return p
	}
	// OpenAI encodes JSON function arguments as a JSON string.
	if raw[0] == '"' && !freeform {
		var args string
		if json.Unmarshal(raw, &args) == nil {
			raw = json.RawMessage(args)
		}
	}
	p.Arguments = s.value(raw)
	return p
}

func (s *messageSet) result(raw json.RawMessage) any {
	if len(raw) > 0 && raw[0] == '[' {
		// Multimodal tool results may contain binary parts. Project text only.
		var texts []string
		length := 0
		walkItems(raw, func(item wireItem) bool {
			switch item.Type {
			case "text", "input_text", "output_text":
				text := s.text(item.Text)
				length += len(text) + 1
				if length > s.fieldLimit {
					s.truncated = true
					return false
				}
				texts = append(texts, text)
			}
			return true
		})
		return strings.Join(texts, "\n")
	}
	return s.value(raw)
}

func (s *messageSet) item(item wireItem, put func(part) bool) bool {
	switch item.Type {
	case "function_call":
		return put(s.call(item.CallID, item.Name, item.Arguments, false))
	case "custom_tool_call":
		return put(s.call(item.CallID, item.Name, item.Input, true))
	case "function_call_output", "custom_tool_call_output":
		return put(part{Type: "tool_call_response", ID: s.text(item.CallID), Response: s.result(item.Output)})
	case "", "message":
		if item.Role == "tool" || item.Role == "function" {
			return put(part{Type: "tool_call_response", ID: s.text(item.ToolCallID), Name: s.text(item.Name), Response: s.result(item.Content)})
		}
		if !s.textParts(item.Content, put) {
			return false
		}
		if item.Refusal != "" && !put(part{Type: "text", Content: s.text(item.Refusal)}) {
			return false
		}
		if item.LegacyCall != nil && !put(s.call("", item.LegacyCall.Name, item.LegacyCall.Arguments, false)) {
			return false
		}
		return walkItems(item.ToolCalls, func(call wireItem) bool {
			if call.Function == nil {
				return true
			}
			return put(s.call(call.ID, call.Function.Name, call.Function.Arguments, false))
		})
	}
	return true
}

func (s *messageSet) inputItem(item wireItem) bool {
	if s.remaining < 64 {
		s.truncated = true
		return false
	}
	role := item.Role
	switch item.Type {
	case "function_call", "custom_tool_call":
		role = "assistant"
	case "function_call_output", "custom_tool_call_output":
		role = "tool"
	}
	if role == "function" {
		role = "tool"
	}
	s.add(role, "", func(put func(part) bool) { s.item(item, put) })
	return true
}

var credentials = []*regexp.Regexp{
	regexp.MustCompile(`(?is)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|$)`),
	regexp.MustCompile(`(?i)\b(?:bearer|basic)[ \t]+[a-z0-9._~+/=:-]+`),
	regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]+|gh[pousr]_[a-z0-9_]+|github_pat_[a-z0-9_]+|xox[baprs]-[a-z0-9-]+|(?:AKIA|ASIA)[A-Z0-9]{16})\b`),
	regexp.MustCompile(`(?i)\b(?:[a-z0-9_]*(?:api[_-]?key|password|passwd|secret|token)|authorization|cookie)["']?\s*[:=]\s*(?:"[^"\r\n]*"?|'[^'\r\n]*'?|[^\s,;}]+)`),
}

var secretKeyNormalizer = strings.NewReplacer("_", "", "-", "", " ", "")

func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

func (s *messageSet) text(text string) string {
	// Scan beyond the retained prefix so a credential at the cut is still masked.
	if len(text) > s.fieldLimit+8192 {
		s.truncated = true
		text = clip(text, s.fieldLimit+8192)
	}
	for _, pattern := range credentials {
		text = pattern.ReplaceAllString(text, "[REDACTED]")
	}
	if len(text) > s.fieldLimit {
		s.truncated = true
		text = clip(text, s.fieldLimit)
	}
	return text
}

func (s *messageSet) value(raw json.RawMessage) any {
	if len(raw) > s.fieldLimit && raw[0] != '"' {
		s.truncated = true
		return nil
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if !json.Valid(raw) || dec.Decode(&value) != nil {
		return nil
	}
	return s.redact(value)
}

func (s *messageSet) redact(value any) any {
	switch value := value.(type) {
	case string:
		return s.text(value)
	case []any:
		for i, item := range value {
			value[i] = s.redact(item)
		}
	case map[string]any:
		clean := make(map[string]any, len(value))
		for key, item := range value {
			normalized := secretKeyNormalizer.Replace(strings.ToLower(key))
			switch {
			case strings.HasSuffix(normalized, "apikey"), strings.HasSuffix(normalized, "secret"),
				strings.HasSuffix(normalized, "token"), strings.HasSuffix(normalized, "password"),
				normalized == "authorization", normalized == "cookie", normalized == "passwd",
				normalized == "privatekey", normalized == "credentials", normalized == "accesskey":
				clean[s.text(key)] = "[REDACTED]"
			default:
				clean[s.text(key)] = s.redact(item)
			}
		}
		return clean
	}
	return value
}
