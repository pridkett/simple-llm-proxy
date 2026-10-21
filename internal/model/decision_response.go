package model

import (
	"encoding/json"
	"fmt"
	"math"
)

// NormalizedDecisionUsage splits included cache tokens out of total input.
// Wire usage is retained separately in DecisionResponse.Fields.
func NormalizedDecisionUsage(raw json.RawMessage) (*Usage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var u struct {
		Input   *int `json:"input_tokens"`
		Output  int  `json:"output_tokens"`
		Details struct {
			Read  int `json:"cached_tokens"`
			Write int `json:"cache_write_tokens"`
		} `json:"input_tokens_details"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil, fmt.Errorf("invalid decision usage: %w", err)
	}
	if u.Input == nil {
		return nil, nil
	}
	if *u.Input < 0 || u.Output < 0 || u.Details.Read < 0 || u.Details.Write < 0 || u.Details.Read > *u.Input || u.Details.Write > *u.Input-u.Details.Read {
		return nil, fmt.Errorf("invalid decision token counts")
	}
	return &Usage{PromptTokens: *u.Input - u.Details.Read - u.Details.Write, CompletionTokens: u.Output, CacheReadTokens: u.Details.Read, CacheWriteTokens: u.Details.Write, TotalTokens: *u.Input + u.Output}, nil
}

// ResponseFromWire checks that each question has an answer and translates only
// when needed. Unknown answer/response fields survive both native and translated
// calls. Refusals are preserved as typed answers rather than treated as zero.
func (r *DecisionRequest) ResponseFromWire(fields map[string]json.RawMessage, format string) (*DecisionResponse, error) {
	usage, err := NormalizedDecisionUsage(fields["usage"])
	if err != nil {
		return nil, err
	}
	var answers []map[string]json.RawMessage
	if format == DecisionsFormat {
		if json.Unmarshal(fields["answers"], &answers) != nil || len(answers) != len(r.Questions) {
			return nil, fmt.Errorf("decision response has missing or invalid answers")
		}
	} else {
		var named map[string]map[string]json.RawMessage
		if json.Unmarshal(fields["answers"], &named) != nil || len(named) != len(r.Questions) {
			return nil, fmt.Errorf("System One response has missing or invalid answers")
		}
		for i, q := range r.Questions {
			name := q.Name
			if r.Format != format {
				name = fmt.Sprintf("q_%d", i)
			}
			answer, exists := named[name]
			if !exists {
				return nil, fmt.Errorf("System One response is missing answer %q", name)
			}
			answers = append(answers, answer)
		}
	}
	for i, a := range answers {
		if len(a) == 0 || a["type"] == nil {
			return nil, fmt.Errorf("decision response contains an invalid answer")
		}
		if err := validateDecisionAnswer(a, r.Questions[i], format); err != nil {
			return nil, err
		}
	}
	if r.Format == format {
		return &DecisionResponse{Fields: fields, Usage: usage}, nil
	}
	converted := make([]map[string]json.RawMessage, 0, len(answers))
	named := map[string]any{}
	for i, answer := range answers {
		q := r.Questions[i]
		a := cloneFields(answer)
		var kind string
		_ = json.Unmarshal(a["type"], &kind)
		if r.Format == DecisionsFormat {
			if kind == "noul" {
				a["type"] = json.RawMessage(`"predicate"`)
				a["probability"] = a["noul"]
				delete(a, "noul")
			}
			if name, exists := q.Raw["name"]; exists {
				a["name"] = name
			} else {
				a["name"] = json.RawMessage("null")
			}
			if raw := a["probabilities"]; raw != nil {
				var probs map[string]float64
				if err := json.Unmarshal(raw, &probs); err != nil {
					return nil, fmt.Errorf("invalid System One probabilities")
				}
				values := []map[string]any{}
				if q.Type == "choice" {
					for _, c := range q.Choices {
						label := c.Value.(string)
						p, ok := probs[label]
						if !ok {
							return nil, fmt.Errorf("missing choice probability %q", label)
						}
						values = append(values, map[string]any{"value": c.Value, "probability": p})
					}
				}
				if q.Type == "score" {
					for j, l := range q.Levels {
						p, ok := probs[fmt.Sprint(j)]
						if !ok {
							return nil, fmt.Errorf("missing level probability %d", j)
						}
						values = append(values, map[string]any{"label": l.Label, "value": j, "probability": p})
					}
				}
				a["probabilities"], _ = json.Marshal(values)
				delete(a, "legend")
			}
			converted = append(converted, a)
		} else {
			delete(a, "name")
			if kind == "predicate" {
				a["type"] = json.RawMessage(`"noul"`)
				a["noul"] = a["probability"]
				delete(a, "probability")
			}
			if raw := a["probabilities"]; raw != nil {
				var probs []struct {
					Value       json.RawMessage `json:"value"`
					Label       string          `json:"label"`
					Probability float64         `json:"probability"`
				}
				if json.Unmarshal(raw, &probs) != nil {
					return nil, fmt.Errorf("invalid Decisions probabilities")
				}
				values := map[string]float64{}
				for _, p := range probs {
					var key string
					if q.Type == "score" {
						var index *int
						if json.Unmarshal(p.Value, &index) != nil || index == nil || *index < 0 || *index >= len(q.Levels) || q.Levels[*index].Label != p.Label {
							return nil, fmt.Errorf("invalid score probability index/label")
						}
						key = fmt.Sprint(*index)
					} else if json.Unmarshal(p.Value, &key) != nil {
						return nil, fmt.Errorf("invalid choice probability value")
					}
					values[key] = p.Probability
				}
				a["probabilities"], _ = json.Marshal(values)
				if q.Type == "score" {
					legend := map[string]string{}
					for j, l := range q.Levels {
						legend[fmt.Sprint(j)] = l.Label
					}
					a["legend"], _ = json.Marshal(legend)
				}
			}
			named[q.Name] = a
		}
	}
	out := cloneFields(fields)
	if r.Format == DecisionsFormat {
		out["answers"], _ = json.Marshal(converted)
	} else {
		out["answers"], _ = json.Marshal(named)
	}
	return &DecisionResponse{Fields: out, Usage: usage}, nil
}

func validateDecisionAnswer(a map[string]json.RawMessage, q DecisionQuestion, format string) error {
	var kind string
	if json.Unmarshal(a["type"], &kind) != nil {
		return fmt.Errorf("invalid decision answer type")
	}
	if kind == "refusal" {
		return nil
	}
	if kind == "noul" && format == SystemOneFormat {
		kind = "predicate"
	}
	if kind != q.Type {
		return fmt.Errorf("decision answer type %q does not match %q", kind, q.Type)
	}
	if kind == "predicate" || kind == "score" {
		field := "probability"
		max := 1.0
		if kind == "predicate" && format == SystemOneFormat {
			field = "noul"
		}
		if kind == "score" {
			field = "score"
			max = float64(len(q.Levels) - 1)
		}
		var value *float64
		if json.Unmarshal(a[field], &value) != nil || value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > max {
			return fmt.Errorf("invalid decision %s", field)
		}
	}
	if kind == "choice" {
		var value any
		if json.Unmarshal(a["choice"], &value) != nil {
			return fmt.Errorf("missing decision choice")
		}
		found := false
		for _, c := range q.Choices {
			if c.Value == value {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("decision selected an unknown choice")
		}
	}
	if kind == "box" {
		var box map[string]*float64
		if json.Unmarshal(a["box"], &box) != nil || box == nil {
			return fmt.Errorf("invalid decision box")
		}
		for _, side := range []string{"top", "bottom", "left", "right"} {
			if box[side] == nil || *box[side] < 0 || *box[side] > 1 {
				return fmt.Errorf("invalid decision box edge %s", side)
			}
		}
		if *box["bottom"] < *box["top"] || *box["right"] < *box["left"] {
			return fmt.Errorf("inverted decision box")
		}
	}
	return nil
}
