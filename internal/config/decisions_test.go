package config

import (
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
	for _, rate := range []float64{math.Inf(1), math.NaN(), -1} {
		_, err := NormalizeDecision(&DecisionConfig{Protocol: "decisions", Pricing: &DecisionPricing{CostPerRequest: rate}})
		if err == nil || !strings.Contains(err.Error(), "prices") {
			t.Fatalf("accepted invalid rate: %v", rate)
		}
	}
}
