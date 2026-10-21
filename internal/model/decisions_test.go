package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func decisionRequest(t *testing.T, body, format string) *DecisionRequest {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatal(err)
	}
	r, err := ParseDecisionRequest(fields, format)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDecisionTranslationOrderAndTypedAnswers(t *testing.T) {
	r := decisionRequest(t, `{"model":"alias","input":"urgent ticket","questions":[{"name":"urgent","type":"predicate","instructions":"Urgent?"},{"type":"choice","choices":[{"value":"a","description":"A"},{"value":"b"}]},{"name":"severity","type":"score","levels":[{"label":"low"},{"label":"high"}]}]}`, DecisionsFormat)
	wire, err := r.Wire(SystemOneFormat, "jev")
	if err != nil {
		t.Fatal(err)
	}
	if string(wire["model"]) != `"jev"` || wire["input"] != nil || string(r.Fields["model"]) != `"alias"` {
		t.Fatalf("model rewriting mutated client body: %s", wire)
	}
	var fields map[string]json.RawMessage
	json.Unmarshal([]byte(`{"model":"jev","provider_extra":true,"answers":{"q_2":{"type":"score","score":0.8,"probabilities":{"0":0.2,"1":0.8},"legend":{"0":"low","1":"high"}},"q_1":{"type":"choice","choice":"b","confidence":0.8,"probabilities":{"a":0.2,"b":0.8}},"q_0":{"type":"noul","noul":0.9,"extension":"kept"}},"usage":{"input_tokens":100,"output_tokens":0}}`), &fields)
	response, err := r.ResponseFromWire(fields, SystemOneFormat)
	if err != nil {
		t.Fatal(err)
	}
	var answers []map[string]any
	json.Unmarshal(response.Fields["answers"], &answers)
	if answers[0]["name"] != "urgent" || answers[0]["probability"] != 0.9 || answers[0]["extension"] != "kept" || answers[1]["choice"] != "b" || answers[2]["name"] != "severity" || response.Usage.PromptTokens != 100 || response.Fields["provider_extra"] == nil {
		t.Fatalf("bad response: %s", response.Fields)
	}
	probabilities := answers[2]["probabilities"].([]any)
	if probabilities[1].(map[string]any)["value"] != float64(1) {
		t.Fatal("score probability is missing its required numeric level index")
	}
	if name, present := answers[1]["name"]; !present || name != nil {
		t.Fatal("unnamed Decisions answer must include name:null")
	}
	if _, err := r.ResponseFromWire(map[string]json.RawMessage{"answers": json.RawMessage(`{"q_0":{"type":"noul"}}`)}, SystemOneFormat); err == nil {
		t.Fatal("accepted incomplete answers")
	}
}

func TestSystemOneToDecisions(t *testing.T) {
	r := decisionRequest(t, `{"model":"alias","state":"ticket","questions":{"z":{"type":"noul","instructions":"Urgent?"},"a":{"type":"score","instructions":"severity","criteria":["low","high"]}}}`, SystemOneFormat)
	wire, err := r.Wire(DecisionsFormat, "luna")
	if err != nil {
		t.Fatal(err)
	}
	var questions []map[string]any
	json.Unmarshal(wire["questions"], &questions)
	if questions[0]["name"] != "a" || questions[1]["name"] != "z" || questions[1]["type"] != "predicate" {
		t.Fatalf("unstable translation: %s", wire["questions"])
	}
	fields := map[string]json.RawMessage{"answers": json.RawMessage(`[{"type":"score","name":"a","score":0.2,"probabilities":[{"label":"low","value":0,"probability":0.8},{"label":"high","value":1,"probability":0.2}]},{"type":"predicate","name":"z","probability":0.9}]`)}
	response, err := r.ResponseFromWire(fields, DecisionsFormat)
	if err != nil {
		t.Fatal(err)
	}
	var answers map[string]map[string]any
	json.Unmarshal(response.Fields["answers"], &answers)
	if answers["z"]["noul"] != 0.9 || answers["a"]["legend"].(map[string]any)["1"] != "high" {
		t.Fatalf("bad translated response: %s", response.Fields)
	}
}

func TestNativeDecisionExtensionsAndRichSystemOne(t *testing.T) {
	body := `{"model":"jev","state":{"photo":"img"},"assets":{"img":{"type":"image","format":"png","data_base64":"eA=="}},"custom":{"kept":true},"questions":{"box":{"type":"box","instructions":["locate","img"],"provider_extra":true},"color":{"type":"choice","instructions":{"color":"img"},"criteria":{"red":{"swatch":"red"},"blue":"blue"}}}}`
	r := decisionRequest(t, body, SystemOneFormat)
	wire, err := r.Wire(SystemOneFormat, "jev")
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	json.Unmarshal([]byte(body), &want)
	encoded, _ := json.Marshal(wire)
	json.Unmarshal(encoded, &got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native extension loss: %s", encoded)
	}
	if _, err := r.Wire(DecisionsFormat, "luna"); err == nil {
		t.Fatal("lossy native translation accepted")
	}
}

func TestDecisionImagesBecomeReferencedAssets(t *testing.T) {
	r := decisionRequest(t, `{"model":"jev","input":[{"role":"user","content":[{"type":"input_text","text":"before"},{"type":"input_image","image_url":"data:image/png;base64,eA=="},{"type":"input_text","text":"after"}]}],"questions":[{"type":"box","instructions":"locate"}]}`, DecisionsFormat)
	wire, err := r.Wire(SystemOneFormat, "jev")
	if err != nil {
		t.Fatal(err)
	}
	var assets map[string]any
	json.Unmarshal(wire["assets"], &assets)
	if len(assets) != 1 {
		t.Fatalf("missing assets: %s", wire)
	}
	for key := range assets {
		if !strings.Contains(string(wire["state"]), key) {
			t.Fatal("unreferenced asset")
		}
	}
	if !strings.Contains(string(wire["state"]), `"before","proxy_image_`) || !strings.Contains(string(wire["state"]), `,"after"`) {
		t.Fatalf("lost content order: %s", wire["state"])
	}
	wire, err = r.Wire(SystemOneFormat, "clef", "images")
	if err != nil || wire["images"] == nil || wire["assets"] != nil {
		t.Fatalf("Cloudflare media translation: %s %v", wire, err)
	}
	r.Fields["input"] = json.RawMessage(`[{"role":"user","content":[{"type":"input_image","image_url":"http://127.0.0.1/private"}]}]`)
	if _, err := r.Wire(SystemOneFormat, "jev"); err == nil {
		t.Fatal("remote asset should require native provider support")
	}
}

func TestDecisionRequestValidation(t *testing.T) {
	for _, body := range []string{
		`{"model":"m","input":"x","questions":[]}`,
		`{"model":"m","input":"x","stream":true,"questions":[{"type":"predicate","instructions":"x"}]}`,
		`{"model":"m","input":null,"questions":[{"type":"predicate","instructions":"x"}]}`,
		`{"model":"m","input":{},"questions":[{"type":"predicate","instructions":"x"}]}`,
		`{"model":"m","input":"x","questions":[{"type":"choice","choices":[{"value":1}]}]}`,
		`{"model":"m","input":"x","questions":[{"type":"choice","choices":[{"value":"a"},{"value":"a"}]}]}`,
		`{"model":"m","input":"x","questions":[{"type":"predicate","name":"a","instructions":"x"},{"type":"predicate","name":"a","instructions":"x"}]}`,
	} {
		var fields map[string]json.RawMessage
		json.Unmarshal([]byte(body), &fields)
		if _, err := ParseDecisionRequest(fields, DecisionsFormat); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
	r := decisionRequest(t, `{"model":"m","input":"x","questions":[{"type":"choice","choices":[{"value":true},{"value":"true"}]}]}`, DecisionsFormat)
	if _, err := r.Wire(SystemOneFormat, "jev"); err == nil {
		t.Fatal("boolean choices must not be coerced to strings")
	}
}

func TestDecisionUsageNormalization(t *testing.T) {
	u, err := NormalizedDecisionUsage(json.RawMessage(`{"input_tokens":100,"output_tokens":2,"input_tokens_details":{"cached_tokens":30,"cache_write_tokens":20},"extension":true}`))
	if err != nil || u.PromptTokens != 50 || u.CacheReadTokens != 30 || u.CacheWriteTokens != 20 || u.TotalTokens != 102 {
		t.Fatalf("usage=%+v err=%v", u, err)
	}
	for _, raw := range []string{`null`, `{}`, `{"output_tokens":0}`} {
		u, err := NormalizedDecisionUsage(json.RawMessage(raw))
		if err != nil || u != nil {
			t.Fatalf("unknown usage became known: %s", raw)
		}
	}
	for _, raw := range []string{`{"input_tokens":-1}`, `{"input_tokens":1,"input_tokens_details":{"cached_tokens":2}}`, `{"input_tokens":1.5}`} {
		if _, err := NormalizedDecisionUsage(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	u, err = NormalizedDecisionUsage(json.RawMessage(`{"input_tokens":0,"output_tokens":0}`))
	if err != nil || u == nil {
		t.Fatal("measured zero usage became unknown")
	}
}
