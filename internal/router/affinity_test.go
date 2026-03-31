package router

import (
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestAffinityStrategy_PrefersProvider(t *testing.T) {
	dA1 := makeDeployment("openai", "gpt-4")
	dA2 := makeDeployment("openai", "gpt-4-turbo")
	dB := makeDeployment("anthropic", "claude-3")

	inner := NewRoundRobin()
	affinity := NewAffinityStrategy("openai", inner)

	deployments := []*provider.Deployment{dA1, dA2, dB}

	for i := 0; i < 10; i++ {
		selected := affinity.Select(deployments)
		if selected == nil {
			t.Fatal("Select returned nil for non-empty slice")
		}
		if selected.ProviderName != "openai" {
			t.Errorf("iteration %d: expected openai, got %s", i, selected.ProviderName)
		}
	}
}

func TestAffinityStrategy_FallsBackWhenPreferredAbsent(t *testing.T) {
	dB1 := makeDeployment("anthropic", "claude-3")
	dB2 := makeDeployment("anthropic", "claude-3-sonnet")

	inner := NewRoundRobin()
	affinity := NewAffinityStrategy("openai", inner)

	deployments := []*provider.Deployment{dB1, dB2}

	for i := 0; i < 10; i++ {
		selected := affinity.Select(deployments)
		if selected == nil {
			t.Fatal("Select returned nil when fallback should work")
		}
		if selected.ProviderName != "anthropic" {
			t.Errorf("iteration %d: expected anthropic fallback, got %s", i, selected.ProviderName)
		}
	}
}

func TestAffinityStrategy_UsesInnerStrategyAmongPreferred(t *testing.T) {
	dA1 := makeDeployment("openai", "gpt-4")
	dA2 := makeDeployment("openai", "gpt-4-turbo")

	weights := map[string]int{
		dA1.DeploymentKey(): 70,
		dA2.DeploymentKey(): 30,
	}
	inner := NewWeightedRoundRobin(weights)
	affinity := NewAffinityStrategy("openai", inner)

	counts := map[string]int{}
	deployments := []*provider.Deployment{dA1, dA2}

	for i := 0; i < 100; i++ {
		selected := affinity.Select(deployments)
		if selected == nil {
			t.Fatal("Select returned nil for non-empty slice")
		}
		counts[selected.DeploymentKey()]++
	}

	a1Count := counts[dA1.DeploymentKey()]
	a2Count := counts[dA2.DeploymentKey()]

	if a1Count < 65 || a1Count > 75 {
		t.Errorf("expected gpt-4 (weight 70) to be selected 65-75 times, got %d", a1Count)
	}
	if a2Count < 25 || a2Count > 35 {
		t.Errorf("expected gpt-4-turbo (weight 30) to be selected 25-35 times, got %d", a2Count)
	}
}

func TestAffinityStrategy_EmptySlice(t *testing.T) {
	inner := NewRoundRobin()
	affinity := NewAffinityStrategy("openai", inner)

	selected := affinity.Select([]*provider.Deployment{})
	if selected != nil {
		t.Errorf("expected nil for empty slice, got %v", selected)
	}

	selected = affinity.Select(nil)
	if selected != nil {
		t.Errorf("expected nil for nil slice, got %v", selected)
	}
}
