package router

import (
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestWeightedRoundRobin_ProportionalDistribution(t *testing.T) {
	dA := makeDeployment("gpt-4", "openai", "gpt-4")
	dB := makeDeployment("claude-3", "anthropic", "claude-3")

	weights := map[string]int{
		dA.DeploymentKey(): 80,
		dB.DeploymentKey(): 20,
	}
	wrr := NewWeightedRoundRobin(weights)

	counts := map[string]int{}
	deployments := []*provider.Deployment{dA, dB}

	for i := 0; i < 100; i++ {
		selected := wrr.Select(deployments)
		if selected == nil {
			t.Fatal("Select returned nil for non-empty slice")
		}
		counts[selected.DeploymentKey()]++
	}

	aCount := counts[dA.DeploymentKey()]
	bCount := counts[dB.DeploymentKey()]

	if aCount < 75 || aCount > 85 {
		t.Errorf("expected A (weight 80) to be selected 75-85 times, got %d", aCount)
	}
	if bCount < 15 || bCount > 25 {
		t.Errorf("expected B (weight 20) to be selected 15-25 times, got %d", bCount)
	}
}

func TestWeightedRoundRobin_SingleDeployment(t *testing.T) {
	d := makeDeployment("gpt-4", "openai", "gpt-4")
	wrr := NewWeightedRoundRobin(map[string]int{d.DeploymentKey(): 50})

	for i := 0; i < 10; i++ {
		selected := wrr.Select([]*provider.Deployment{d})
		if selected != d {
			t.Fatalf("expected the single deployment, got %v", selected)
		}
	}
}

func TestWeightedRoundRobin_EqualWeights(t *testing.T) {
	dA := makeDeployment("gpt-4", "openai", "gpt-4")
	dB := makeDeployment("claude-3", "anthropic", "claude-3")
	dC := makeDeployment("llama-3", "openrouter", "llama-3")

	weights := map[string]int{
		dA.DeploymentKey(): 1,
		dB.DeploymentKey(): 1,
		dC.DeploymentKey(): 1,
	}
	wrr := NewWeightedRoundRobin(weights)

	counts := map[string]int{}
	deployments := []*provider.Deployment{dA, dB, dC}

	for i := 0; i < 30; i++ {
		selected := wrr.Select(deployments)
		if selected == nil {
			t.Fatal("Select returned nil for non-empty slice")
		}
		counts[selected.DeploymentKey()]++
	}

	// With equal weights and smooth WRR, each should get exactly 10
	for _, d := range deployments {
		key := d.DeploymentKey()
		if counts[key] != 10 {
			t.Errorf("expected deployment %s to be selected 10 times, got %d", key, counts[key])
		}
	}
}

func TestWeightedRoundRobin_UnknownDeployment(t *testing.T) {
	dA := makeDeployment("gpt-4", "openai", "gpt-4")
	dB := makeDeployment("claude-3", "anthropic", "claude-3") // Not in weights map

	weights := map[string]int{
		dA.DeploymentKey(): 1,
		// dB not in weights -> defaults to 1
	}
	wrr := NewWeightedRoundRobin(weights)

	counts := map[string]int{}
	deployments := []*provider.Deployment{dA, dB}

	for i := 0; i < 20; i++ {
		selected := wrr.Select(deployments)
		if selected == nil {
			t.Fatal("Select returned nil for non-empty slice")
		}
		counts[selected.DeploymentKey()]++
	}

	// Both weight 1 -> should each get 10
	if counts[dA.DeploymentKey()] != 10 {
		t.Errorf("expected A to be selected 10 times, got %d", counts[dA.DeploymentKey()])
	}
	if counts[dB.DeploymentKey()] != 10 {
		t.Errorf("expected B to be selected 10 times, got %d", counts[dB.DeploymentKey()])
	}
}

func TestWeightedRoundRobin_EmptySlice(t *testing.T) {
	wrr := NewWeightedRoundRobin(map[string]int{})

	selected := wrr.Select([]*provider.Deployment{})
	if selected != nil {
		t.Errorf("expected nil for empty slice, got %v", selected)
	}

	selected = wrr.Select(nil)
	if selected != nil {
		t.Errorf("expected nil for nil slice, got %v", selected)
	}
}
