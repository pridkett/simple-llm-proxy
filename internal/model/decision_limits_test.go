package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func parseDecision(body, format string) (*DecisionRequest, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		return nil, err
	}
	return ParseDecisionRequest(fields, format)
}

// The proxy's own limits are the widest any upstream accepts (Gemini), so a
// native request is never refused before its provider can judge it.
func TestDecisionLimitsFollowTheWidestUpstream(t *testing.T) {
	question := `{"type":"predicate","instructions":"?"},`
	many := func(n int) string {
		return `{"model":"m","input":"x","questions":[` + strings.TrimSuffix(strings.Repeat(question, n), ",") + `]}`
	}
	if _, err := parseDecision(many(MaxDecisionQuestions), DecisionsFormat); err != nil {
		t.Fatalf("rejected %d questions: %v", MaxDecisionQuestions, err)
	}
	if _, err := parseDecision(many(MaxDecisionQuestions+1), DecisionsFormat); err == nil {
		t.Fatal("accepted more questions than any upstream takes")
	}

	levels := func(n int) string { return strings.TrimSuffix(strings.Repeat(`"level",`, n), ",") }
	rubric := func(n int) string {
		return `{"model":"m","state":"x","questions":{"s":{"type":"score","instructions":"?","criteria":[` + levels(n) + `]}}}`
	}
	if _, err := parseDecision(rubric(255), SystemOneFormat); err != nil {
		t.Fatalf("rejected a 255-level System One rubric: %v", err)
	}
	if _, err := parseDecision(rubric(256), SystemOneFormat); err == nil {
		t.Fatal("accepted a 256-level rubric")
	}
	// OpenAI's format keeps OpenAI's limit.
	labels := strings.TrimSuffix(strings.Repeat(`{"label":"l"},`, 11), ",")
	if _, err := parseDecision(`{"model":"m","input":"x","questions":[{"type":"score","instructions":"?","levels":[`+labels+`]}]}`, DecisionsFormat); err == nil {
		t.Fatal("accepted 11 Decisions levels")
	}
}

func TestDecisionsInputJSONPart(t *testing.T) {
	r, err := parseDecision(`{"model":"m","input":[{"role":"user","content":[{"type":"input_json","data":{"order":7}}]}],"questions":[{"type":"predicate","instructions":"?"}]}`, DecisionsFormat)
	if err != nil {
		t.Fatalf("input_json is a documented Decisions part: %v", err)
	}
	// It reaches a native deployment untouched and is refused, not dropped,
	// where the System One bridge has no equivalent.
	if wire, err := r.Wire(DecisionsFormat, "luna"); err != nil || !strings.Contains(string(wire["input"]), `"input_json"`) {
		t.Fatalf("native pass-through: %s %v", wire["input"], err)
	}
	if _, err := r.Wire(SystemOneFormat, "jev"); err == nil {
		t.Fatal("input_json was silently dropped in translation")
	}
}
