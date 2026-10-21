package model

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
)

const (
	DecisionsFormat = "decisions"
	SystemOneFormat = "systemone"
)

// DecisionRequest retains the complete wire object. A fresh object is built for
// every attempt, so translations and model rewriting cannot contaminate retries.
type DecisionRequest struct {
	Model      string
	Format     string
	Fields     map[string]json.RawMessage
	Questions  []DecisionQuestion
	Modalities []string
}

type DecisionQuestion struct {
	NativeOnly   bool // structured native instructions/criteria require native transport
	Name         string
	Type         string // predicate, choice, score, box
	Instructions string
	Choices      []DecisionChoice
	Levels       []DecisionLevel
	Raw          map[string]json.RawMessage
}

type DecisionChoice struct {
	Value       any    `json:"value"`
	Description string `json:"description,omitempty"`
}
type DecisionLevel struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// DecisionResponse preserves provider extensions while exposing normalized usage
// to the common router, budget, telemetry, and logging paths.
type DecisionResponse struct {
	Fields map[string]json.RawMessage
	Usage  *Usage
}

func ParseDecisionRequest(fields map[string]json.RawMessage, format string) (*DecisionRequest, error) {
	r := &DecisionRequest{Fields: fields, Format: format, Modalities: []string{"text"}}
	if err := json.Unmarshal(fields["model"], &r.Model); err != nil || r.Model == "" {
		return nil, fmt.Errorf("model is required and must be a string")
	}
	for _, flag := range []string{"stream", "background"} {
		if raw, ok := fields[flag]; ok {
			var enabled bool
			if json.Unmarshal(raw, &enabled) != nil || enabled {
				return nil, fmt.Errorf("%s is not supported for decision requests", flag)
			}
			delete(fields, flag)
		}
	}
	inputKey := "input"
	if format == SystemOneFormat {
		inputKey = "state"
	}
	var input any
	if err := json.Unmarshal(fields[inputKey], &input); err != nil || input == nil {
		return nil, fmt.Errorf("%s is required", inputKey)
	}
	switch value := input.(type) {
	case string:
	case []any:
		if format == DecisionsFormat {
			if len(value) == 0 {
				return nil, fmt.Errorf("input messages must not be empty")
			}
			if err := r.scanInput(value); err != nil {
				return nil, err
			}
		}
	case map[string]any:
		if format != SystemOneFormat {
			return nil, fmt.Errorf("input must be a string or message array")
		}
	default:
		return nil, fmt.Errorf("%s must be a string, object, or array", inputKey)
	}
	for key, modality := range map[string]string{"images": "image", "assets": "image", "audio": "audio", "videos": "video"} {
		if raw, exists := fields[key]; exists {
			if format != SystemOneFormat {
				return nil, fmt.Errorf("%s requires the System One endpoint", key)
			}
			if key == "assets" {
				var assets map[string]struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(raw, &assets) != nil || len(assets) == 0 {
					return nil, fmt.Errorf("assets must be a non-empty object")
				}
				for _, asset := range assets {
					if !slices.Contains([]string{"image", "audio", "video"}, asset.Type) {
						return nil, fmt.Errorf("unsupported asset type %q", asset.Type)
					}
					r.addModality(asset.Type)
				}
			} else {
				var media []string
				if json.Unmarshal(raw, &media) != nil || len(media) == 0 {
					return nil, fmt.Errorf("%s must be a non-empty array of media references", key)
				}
				for _, ref := range media {
					if ref == "" {
						return nil, fmt.Errorf("%s contains an empty media reference", key)
					}
				}
				r.addModality(modality)
			}
		}
	}
	var rawQuestions []map[string]json.RawMessage
	if format == DecisionsFormat {
		if err := json.Unmarshal(fields["questions"], &rawQuestions); err != nil {
			return nil, fmt.Errorf("questions must be an array")
		}
	} else {
		var questions map[string]map[string]json.RawMessage
		if err := json.Unmarshal(fields["questions"], &questions); err != nil {
			return nil, fmt.Errorf("questions must be an object")
		}
		names := make([]string, 0, len(questions))
		for name := range questions {
			names = append(names, name)
		}
		sort.Strings(names) // stable order for an unordered System One object
		for _, name := range names {
			if name == "" {
				return nil, fmt.Errorf("question names must not be empty")
			}
			q := questions[name]
			if q == nil {
				return nil, fmt.Errorf("question %q must be an object", name)
			}
			// The map key, not an optional extension named 'name', identifies the answer.
			copy := cloneFields(q)
			copy["name"], _ = json.Marshal(name)
			rawQuestions = append(rawQuestions, copy)
		}
	}
	if len(rawQuestions) < 1 || len(rawQuestions) > 400 {
		return nil, fmt.Errorf("questions must contain 1 to 400 entries; upstream models may have lower limits")
	}
	seen := map[string]bool{}
	for i, raw := range rawQuestions {
		q, err := parseDecisionQuestion(raw, format)
		if err != nil {
			return nil, fmt.Errorf("questions[%d]: %w", i, err)
		}
		if q.Name != "" {
			if seen[q.Name] {
				return nil, fmt.Errorf("duplicate question name %q", q.Name)
			}
			seen[q.Name] = true
		}
		r.Questions = append(r.Questions, q)
	}
	return r, nil
}

func (r *DecisionRequest) addModality(m string) {
	if !slices.Contains(r.Modalities, m) {
		r.Modalities = append(r.Modalities, m)
	}
}

func (r *DecisionRequest) scanInput(messages []any) error {
	for _, value := range messages {
		message, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("input items must be objects")
		}
		content, exists := message["content"]
		if !exists {
			continue
		} // native tool calls/outputs retain their wire fields
		if _, ok := content.(string); ok {
			continue
		}
		parts, ok := content.([]any)
		if !ok {
			return fmt.Errorf("message content must be text or an array")
		}
		for _, value := range parts {
			part, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("input parts must be objects")
			}
			switch part["type"] {
			case "input_text", "output_text", "refusal":
			case "input_image":
				r.addModality("image")
			case "input_audio":
				r.addModality("audio")
			case "input_video":
				r.addModality("video")
			default:
				return fmt.Errorf("unsupported decision input part type %v", part["type"])
			}
		}
	}
	return nil
}

func parseDecisionQuestion(raw map[string]json.RawMessage, format string) (DecisionQuestion, error) {
	q := DecisionQuestion{Raw: raw}
	if json.Unmarshal(raw["type"], &q.Type) != nil {
		return q, fmt.Errorf("question type is required")
	}
	if name, exists := raw["name"]; exists {
		if json.Unmarshal(name, &q.Name) != nil {
			return q, fmt.Errorf("name must be a string")
		}
	}
	if instructions, exists := raw["instructions"]; exists {
		if json.Unmarshal(instructions, &q.Instructions) != nil {
			if format != SystemOneFormat || !structuredDecisionValue(instructions) {
				return q, fmt.Errorf("instructions must be text (or structured native System One content)")
			}
			q.NativeOnly = true
		}
	}
	if format == SystemOneFormat && q.Type == "noul" {
		q.Type = "predicate"
	}
	if !slices.Contains([]string{"predicate", "choice", "score", "box"}, q.Type) {
		return q, fmt.Errorf("unsupported question type %q", q.Type)
	}
	if q.Type == "predicate" && q.Instructions == "" && !q.NativeOnly && (format != SystemOneFormat || len(raw["criteria"]) == 0) {
		return q, fmt.Errorf("predicate requires instructions (or native noul criteria)")
	}
	if q.Type == "choice" {
		if format == DecisionsFormat {
			if json.Unmarshal(raw["choices"], &q.Choices) != nil {
				return q, fmt.Errorf("choices must be an array")
			}
		} else {
			var criteria map[string]json.RawMessage
			if json.Unmarshal(raw["criteria"], &criteria) != nil {
				return q, fmt.Errorf("choice criteria must be an object of string descriptions")
			}
			keys := make([]string, 0, len(criteria))
			for key := range criteria {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				var description string
				if json.Unmarshal(criteria[key], &description) != nil {
					if !structuredDecisionValue(criteria[key]) {
						return q, fmt.Errorf("invalid choice criterion")
					}
					q.NativeOnly = true
				}
				q.Choices = append(q.Choices, DecisionChoice{Value: key, Description: description})
			}
		}
		if len(q.Choices) < 1 || len(q.Choices) > 255 {
			return q, fmt.Errorf("choice requires 1 to 255 choices")
		}
		seen := map[string]bool{}
		for _, choice := range q.Choices {
			switch choice.Value.(type) {
			case string, bool:
			default:
				return q, fmt.Errorf("choice values must be strings or booleans")
			}
			encoded, _ := json.Marshal(choice.Value)
			if seen[string(encoded)] {
				return q, fmt.Errorf("choice values must be unique")
			}
			seen[string(encoded)] = true
		}
	}
	if q.Type == "score" {
		if format == DecisionsFormat {
			if json.Unmarshal(raw["levels"], &q.Levels) != nil {
				return q, fmt.Errorf("levels must be an array")
			}
		} else {
			var criteria []json.RawMessage
			if json.Unmarshal(raw["criteria"], &criteria) != nil {
				return q, fmt.Errorf("score criteria must be an array of strings")
			}
			for _, label := range criteria {
				var text string
				if json.Unmarshal(label, &text) != nil {
					if !structuredDecisionValue(label) {
						return q, fmt.Errorf("invalid score criterion")
					}
					q.NativeOnly = true
				}
				q.Levels = append(q.Levels, DecisionLevel{Label: text})
			}
		}
		if len(q.Levels) < 1 || len(q.Levels) > 10 {
			return q, fmt.Errorf("score requires 1 to 10 levels")
		}
	}
	return q, nil
}

func cloneFields(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func structuredDecisionValue(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch value.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

// Wire returns an upstream request, preserving native extensions. Cross-format
// translation rejects unknown fields and rich input rather than losing meaning.
func (r *DecisionRequest) Wire(format, actualModel string, imageStyle ...string) (map[string]json.RawMessage, error) {
	out := cloneFields(r.Fields)
	out["model"], _ = json.Marshal(actualModel)
	if format == r.Format {
		return out, nil
	}
	for key := range out {
		if !slices.Contains([]string{"model", "input", "state", "questions"}, key) {
			return nil, fmt.Errorf("cannot translate extension %q; use a model with the matching native protocol", key)
		}
	}
	if r.Format == DecisionsFormat {
		var input string
		if json.Unmarshal(out["input"], &input) != nil {
			style := "assets"
			if len(imageStyle) > 0 {
				style = imageStyle[0]
			}
			state, media, err := r.systemOneInput(style)
			if err != nil {
				return nil, err
			}
			out["state"] = state
			if media != nil {
				out[style] = media
			}
		} else {
			out["state"] = out["input"]
		}
		delete(out, "input")
	} else {
		var text string
		if json.Unmarshal(out["state"], &text) != nil {
			return nil, fmt.Errorf("structured state translation is unsupported; use a native System One model")
		}
		out["input"] = out["state"]
		delete(out, "state")
	}
	questions := make([]map[string]any, 0, len(r.Questions))
	named := map[string]any{}
	for i, q := range r.Questions {
		if q.NativeOnly {
			return nil, fmt.Errorf("structured question content requires a native System One model")
		}
		// Typed decoding must not silently remove provider extensions nested in
		// choices/levels when a caller requests a cross-protocol translation.
		for field, keys := range map[string][]string{"choices": {"value", "description"}, "levels": {"label", "description"}} {
			if raw := q.Raw[field]; raw != nil {
				var items []map[string]json.RawMessage
				if json.Unmarshal(raw, &items) != nil {
					return nil, fmt.Errorf("invalid %s", field)
				}
				for _, item := range items {
					for key := range item {
						if !slices.Contains(keys, key) {
							return nil, fmt.Errorf("cannot translate %s extension %q", field, key)
						}
					}
				}
			}
		}
		labels := map[string]bool{}
		for _, level := range q.Levels {
			if labels[level.Label] {
				return nil, fmt.Errorf("duplicate level labels require native transport")
			}
			labels[level.Label] = true
		}
		allowed := []string{"name", "type", "instructions", "choices", "levels"}
		if r.Format == SystemOneFormat {
			allowed = []string{"name", "type", "instructions", "criteria"}
		}
		for key := range q.Raw {
			if !slices.Contains(allowed, key) {
				return nil, fmt.Errorf("cannot translate question extension %q", key)
			}
		}
		if q.Type == "predicate" && q.Raw["criteria"] != nil {
			return nil, fmt.Errorf("noul criteria cannot be translated without changing its meaning")
		}
		m := map[string]any{"type": q.Type, "instructions": q.Instructions}
		if format == SystemOneFormat {
			if q.Type == "predicate" {
				m["type"] = "noul"
			}
			if q.Type == "choice" {
				criteria := map[string]string{}
				for _, c := range q.Choices {
					value, ok := c.Value.(string)
					if !ok {
						return nil, fmt.Errorf("boolean choices require a native Decisions model")
					}
					criteria[value] = c.Description
				}
				m["criteria"] = criteria
			}
			if q.Type == "score" {
				criteria := []string{}
				for _, level := range q.Levels {
					if level.Description != "" {
						return nil, fmt.Errorf("level descriptions require a native Decisions model")
					}
					criteria = append(criteria, level.Label)
				}
				m["criteria"] = criteria
			}
			named[fmt.Sprintf("q_%d", i)] = m
		} else {
			m["name"] = q.Name
			if q.Type == "choice" {
				m["choices"] = q.Choices
			}
			if q.Type == "score" {
				m["levels"] = q.Levels
			}
			questions = append(questions, m)
		}
	}
	if format == SystemOneFormat {
		out["questions"], _ = json.Marshal(named)
	} else {
		out["questions"], _ = json.Marshal(questions)
	}
	return out, nil
}

func (r *DecisionRequest) EstimatedTokens() int { data, _ := json.Marshal(r.Fields); return len(data) }
