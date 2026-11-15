package model

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Gemini's Decisions API is a System One dialect: named questions and the same
// answer shapes, but typed input blocks, renamed noul criteria, and its own
// usage names. Only the differences live here; answers go through the common
// System One validation and translation in ResponseFromWire.

var geminiQuestionKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// geminiMediaTypes lets a remote reference be forwarded without fetching it:
// Gemini needs a concrete MIME type and the proxy never downloads client URLs.
var geminiMediaTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif",
	".heic": "image/heic", ".heif": "image/heif", ".bmp": "image/bmp", ".tif": "image/tiff", ".tiff": "image/tiff",
	".wav": "audio/wav", ".mp3": "audio/mp3", ".aac": "audio/aac", ".ogg": "audio/ogg", ".flac": "audio/flac",
	".m4a": "audio/m4a", ".opus": "audio/opus", ".aiff": "audio/aiff",
	".mp4": "video/mp4", ".mpeg": "video/mpeg", ".mov": "video/mov", ".avi": "video/avi", ".webm": "video/webm",
	".wmv": "video/wmv", ".flv": "video/x-flv", ".3gp": "video/3gpp",
}

// GeminiWire builds a native Gemini Decisions request from either public format.
func (r *DecisionRequest) GeminiWire(actualModel string) (map[string]json.RawMessage, error) {
	allowed := []string{"model", "input", "questions"}
	if r.Format == SystemOneFormat {
		allowed = []string{"model", "state", "questions", "images", "audio", "videos"}
	}
	for key := range r.Fields {
		if key == "assets" {
			return nil, fmt.Errorf("Gemini does not support TypeSafe assets; use images, audio, or videos")
		}
		if !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("cannot translate extension %q for Gemini", key)
		}
	}
	input, err := r.geminiInput()
	if err != nil {
		return nil, err
	}
	questions, err := r.geminiQuestions()
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{"input": input, "questions": questions}
	out["model"], _ = json.Marshal(actualModel)
	return out, nil
}

func (r *DecisionRequest) geminiQuestions() (json.RawMessage, error) {
	named := map[string]map[string]json.RawMessage{}
	if r.Format == DecisionsFormat {
		raw, err := r.translatedQuestions(SystemOneFormat)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &named); err != nil {
			return nil, err
		}
	} else {
		for _, q := range r.Questions {
			if q.NativeOnly {
				return nil, fmt.Errorf("question %q: Gemini requires text instructions and criteria", q.Name)
			}
			if !geminiQuestionKey.MatchString(q.Name) {
				return nil, fmt.Errorf("question name %q is not valid for Gemini (1-64 of A-Z, a-z, 0-9, _ or -)", q.Name)
			}
			for key := range q.Raw {
				if !slices.Contains([]string{"name", "type", "instructions", "criteria"}, key) {
					return nil, fmt.Errorf("cannot translate question extension %q for Gemini", key)
				}
			}
			kind := q.Type
			if kind == "predicate" {
				kind = "noul"
			}
			question := map[string]json.RawMessage{}
			question["type"], _ = json.Marshal(kind)
			question["instructions"], _ = json.Marshal(q.Instructions)
			if criteria := q.Raw["criteria"]; criteria != nil {
				question["criteria"] = criteria
			}
			named[q.Name] = question
		}
	}
	for name, question := range named {
		var kind, instructions string
		_ = json.Unmarshal(question["type"], &kind)
		_ = json.Unmarshal(question["instructions"], &instructions)
		if instructions == "" {
			return nil, fmt.Errorf("Gemini requires instructions for every question")
		}
		if question["criteria"] == nil {
			continue
		}
		switch kind {
		case "noul":
			// System One keys noul criteria by the literal answers.
			var criteria map[string]string
			if json.Unmarshal(question["criteria"], &criteria) != nil {
				return nil, fmt.Errorf("question %q: noul criteria must describe \"true\" and \"false\"", name)
			}
			renamed := map[string]string{}
			for key, description := range criteria {
				if key != "true" && key != "false" {
					return nil, fmt.Errorf("question %q: noul criteria must describe \"true\" and \"false\"", name)
				}
				renamed[key+"_description"] = description
			}
			question["criteria"], _ = json.Marshal(renamed)
		case "choice":
			// A bare System One label is null; Gemini spells that as "".
			var criteria map[string]*string
			if json.Unmarshal(question["criteria"], &criteria) != nil {
				return nil, fmt.Errorf("question %q: choice criteria must be text", name)
			}
			described := map[string]string{}
			for key, description := range criteria {
				if description != nil {
					described[key] = *description
				} else {
					described[key] = ""
				}
			}
			question["criteria"], _ = json.Marshal(described)
		}
	}
	return json.Marshal(named)
}

// geminiInput keeps content order. Text-only input stays a plain string.
func (r *DecisionRequest) geminiInput() (json.RawMessage, error) {
	var blocks []map[string]any
	addText := func(text string) {
		// Gemini concatenates adjacent text blocks with no separator.
		if n := len(blocks); n > 0 && blocks[n-1]["type"] == "text" {
			blocks[n-1]["text"] = blocks[n-1]["text"].(string) + "\n" + text
			return
		}
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	addMedia := func(kind, ref string) error {
		block, err := geminiMedia(kind, ref)
		if err == nil {
			blocks = append(blocks, block)
		}
		return err
	}
	if r.Format == SystemOneFormat {
		var text string
		if json.Unmarshal(r.Fields["state"], &text) != nil {
			// Structured state is JSON text to a System One model.
			var compact bytes.Buffer
			if err := json.Compact(&compact, r.Fields["state"]); err != nil {
				return nil, fmt.Errorf("invalid state")
			}
			text = compact.String()
		}
		addText(text)
		for _, field := range []struct{ key, kind string }{{"images", "image"}, {"audio", "audio"}, {"videos", "video"}} {
			raw := r.Fields[field.key]
			if raw == nil {
				continue
			}
			var refs []string
			if json.Unmarshal(raw, &refs) != nil {
				return nil, fmt.Errorf("%s must be an array", field.key)
			}
			for _, ref := range refs {
				if err := addMedia(field.kind, ref); err != nil {
					return nil, err
				}
			}
		}
	} else {
		var text string
		if json.Unmarshal(r.Fields["input"], &text) == nil {
			return r.Fields["input"], nil
		}
		var messages []map[string]json.RawMessage
		if err := json.Unmarshal(r.Fields["input"], &messages); err != nil {
			return nil, fmt.Errorf("input must contain messages")
		}
		if !userMessagesOnly(messages) {
			// Gemini blocks carry no role, so a transcript is sent as JSON text.
			state, media, err := r.systemOneInput("images")
			if err != nil {
				return nil, err
			}
			if media != nil {
				return nil, fmt.Errorf("Gemini cannot take images inside a multi-role transcript; send them in user messages only")
			}
			return json.Marshal(string(state))
		}
		for _, message := range messages {
			if json.Unmarshal(message["content"], &text) == nil {
				addText(text)
				continue
			}
			var parts []map[string]json.RawMessage
			if json.Unmarshal(message["content"], &parts) != nil {
				return nil, fmt.Errorf("message content must be text or parts")
			}
			for _, part := range parts {
				if err := geminiPart(part, addText, addMedia); err != nil {
					return nil, err
				}
			}
		}
		if len(blocks) == 0 {
			return nil, fmt.Errorf("input messages must contain content")
		}
	}
	if len(blocks) == 1 && blocks[0]["type"] == "text" {
		return json.Marshal(blocks[0]["text"])
	}
	return json.Marshal(blocks)
}

// geminiPart translates one Decisions content part of a user message.
func geminiPart(part map[string]json.RawMessage, addText func(string), addMedia func(kind, ref string) error) error {
	var kind string
	_ = json.Unmarshal(part["type"], &kind)
	field := map[string]string{"input_text": "text", "input_json": "data", "input_image": "image_url"}[kind]
	if field == "" {
		return fmt.Errorf("%s parts cannot be translated for Gemini", kind)
	}
	for key := range part {
		if key != "type" && key != field {
			return fmt.Errorf("%s field %q requires native Decisions transport", kind, key)
		}
	}
	if kind == "input_json" {
		var compact bytes.Buffer
		if part[field] == nil || json.Compact(&compact, part[field]) != nil {
			return fmt.Errorf("input_json requires data")
		}
		addText(compact.String())
		return nil
	}
	var value string
	if json.Unmarshal(part[field], &value) != nil {
		return fmt.Errorf("%s requires a %s string", kind, field)
	}
	if kind == "input_text" {
		addText(value)
		return nil
	}
	return addMedia("image", value)
}

func userMessagesOnly(messages []map[string]json.RawMessage) bool {
	for _, message := range messages {
		var role, kind string
		_ = json.Unmarshal(message["role"], &role)
		_ = json.Unmarshal(message["type"], &kind)
		if role != "user" || (kind != "" && kind != "message") {
			return false
		}
		for key := range message {
			if key != "type" && key != "role" && key != "content" {
				return false
			}
		}
	}
	return true
}

// geminiMedia turns a data URL, bare base64, or HTTPS URL into a typed block.
func geminiMedia(kind, ref string) (map[string]any, error) {
	if strings.HasPrefix(ref, "http://") {
		return nil, fmt.Errorf("Gemini %s URLs must use HTTPS", kind)
	}
	if strings.HasPrefix(ref, "https://") {
		u, err := url.Parse(ref)
		if err != nil {
			return nil, fmt.Errorf("invalid %s URL", kind)
		}
		mediaType := geminiMediaTypes[strings.ToLower(path.Ext(u.Path))]
		if !strings.HasPrefix(mediaType, kind+"/") {
			return nil, fmt.Errorf("cannot tell the %s type from its URL; embed it as a data URL instead (remote media is never fetched)", kind)
		}
		return map[string]any{"type": kind, "uri": ref, "mime_type": mediaType}, nil
	}
	mediaType, encoded := "", ref
	if rest, ok := strings.CutPrefix(ref, "data:"); ok {
		if mediaType, encoded, ok = strings.Cut(rest, ";base64,"); !ok {
			return nil, fmt.Errorf("%s data URLs must be base64-encoded", kind)
		}
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("%s must be a data URL, an HTTPS URL, or base64", kind)
	}
	if mediaType == "" {
		// Bare base64 (the Ollama convention) has no declared type.
		mediaType, _, _ = strings.Cut(http.DetectContentType(data), ";")
		if mediaType == "audio/wave" {
			mediaType = "audio/wav"
		}
	}
	if !strings.HasPrefix(mediaType, kind+"/") {
		return nil, fmt.Errorf("%s has media type %q; send it as a data URL with its real type", kind, mediaType)
	}
	return map[string]any{"type": kind, "data": encoded, "mime_type": mediaType}, nil
}

// GeminiDecisionFields makes a Gemini response readable as System One: it names
// the model and adds the usage counters the common accounting expects. Google's
// own usage fields are kept beside them.
func GeminiDecisionFields(fields map[string]json.RawMessage, actualModel string) (map[string]json.RawMessage, error) {
	out := cloneFields(fields)
	if out["model"] == nil {
		out["model"], _ = json.Marshal(actualModel)
	}
	if raw := out["usage"]; raw != nil && string(raw) != "null" {
		var usage map[string]json.RawMessage
		if err := json.Unmarshal(raw, &usage); err != nil {
			return nil, fmt.Errorf("invalid decision usage: %w", err)
		}
		for common, native := range map[string]string{"input_tokens": "total_input_tokens", "output_tokens": "total_output_tokens"} {
			if usage[common] == nil && usage[native] != nil {
				usage[common] = usage[native]
			}
		}
		out["usage"], _ = json.Marshal(usage)
	}
	return out, nil
}
