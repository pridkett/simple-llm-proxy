package config

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestDecisionConfig(t *testing.T) {
	base := "model_list:\n - model_name: classify\n   litellm_params:\n     model: typesafe/jev-latest\n   decision:\n"
	cfg, err := Parse([]byte(base + "     protocol: systemone\n     pricing: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.ModelList[0].Decision
	if d.Protocol != "systemone" || len(d.Modalities) != 1 || d.Modalities[0] != "text" || len(d.QuestionTypes) != 3 || d.Pricing == nil {
		t.Fatalf("defaults: %+v", d)
	}
	for _, body := range []string{
		"     protocol: systemone\n",
		"     protocol: chat\n     pricing: {}\n",
		"     protocol: systemone\n     modalities: [typo]\n     pricing: {}\n",
		"     protocol: systemone\n     question_types: [noul]\n     pricing: {}\n",
		"     protocol: systemone\n     pricing: {input_cost_per_token: -1}\n",
		"     protocol: systemone\n     pricing: {input_cost_per_token: .inf}\n",
		"     protocol: systemone\n     pricing: {input_cost_per_tokens: 1}\n",
		"     protocol: systemone\n     path: https://other.test/steal\n     pricing: {}\n",
		"     protocol: systemone\n     path: //other.test/steal\n     pricing: {}\n",
		"     protocol: systemone\n     path: /decisions?secret=yes\n     pricing: {}\n",
	} {
		if _, err := Parse([]byte(base + body)); err == nil {
			t.Errorf("accepted invalid config %s", body)
		}
	}
	for _, protocol := range []string{"gemini", "ollama"} {
		if _, err := Parse([]byte(base + "     protocol: " + protocol + "\n     pricing: {}\n")); err != nil {
			t.Errorf("rejected %s: %v", protocol, err)
		}
	}
	t.Setenv("DECISION_TEST_SALT", "tenant-a")
	cfg, err = Parse([]byte(base + "     protocol: systemone\n     extra_body: {cache_salt: os.environ/DECISION_TEST_SALT, nested: {keep: [1, 2]}}\n     pricing: {}\n"))
	if err != nil || cfg.ModelList[0].Decision.ExtraBody["cache_salt"] != "tenant-a" {
		t.Fatalf("extra_body: %+v %v", cfg, err)
	}
	// A cache salt separates tenants, so it must not leak through discovery.
	if data, _ := json.Marshal(cfg.ModelList[0].Decision); strings.Contains(string(data), "tenant-a") {
		t.Fatalf("extra_body exposed in JSON: %s", data)
	}
	for _, reserved := range []string{"model", "questions", "state", "images", "stream"} {
		if _, err := Parse([]byte(base + "     protocol: systemone\n     extra_body: {" + reserved + ": x}\n     pricing: {}\n")); err == nil {
			t.Errorf("extra_body accepted proxy-owned field %s", reserved)
		}
	}
	for _, rate := range []float64{math.Inf(1), math.NaN(), -1} {
		_, err := NormalizeDecision(&DecisionConfig{Protocol: "decisions", Pricing: &DecisionPricing{CostPerRequest: rate}})
		if err == nil || !strings.Contains(err.Error(), "prices") {
			t.Fatalf("accepted invalid rate: %v", rate)
		}
	}
}
