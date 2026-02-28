package router

import (
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

func TestShuffleStrategy(t *testing.T) {
	s := NewShuffle()

	deployments := []*provider.Deployment{
		{ModelName: "model1"},
		{ModelName: "model2"},
		{ModelName: "model3"},
	}

	// Test that it returns a deployment
	d := s.Select(deployments)
	if d == nil {
		t.Error("Expected a deployment, got nil")
	}

	// Test empty slice
	d = s.Select([]*provider.Deployment{})
	if d != nil {
		t.Error("Expected nil for empty slice")
	}

	// Test single deployment
	single := []*provider.Deployment{{ModelName: "only"}}
	d = s.Select(single)
	if d.ModelName != "only" {
		t.Errorf("Expected 'only', got '%s'", d.ModelName)
	}
}

func TestRoundRobinStrategy(t *testing.T) {
	r := NewRoundRobin()

	deployments := []*provider.Deployment{
		{ModelName: "model1"},
		{ModelName: "model2"},
		{ModelName: "model3"},
	}

	// Should cycle through deployments
	seen := make(map[string]int)
	for i := 0; i < 9; i++ {
		d := r.Select(deployments)
		seen[d.ModelName]++
	}

	// Each should be selected 3 times
	for name, count := range seen {
		if count != 3 {
			t.Errorf("Expected %s to be selected 3 times, got %d", name, count)
		}
	}
}

func TestCooldownManager(t *testing.T) {
	cm := NewCooldownManager(100*time.Millisecond, 2)

	d := &provider.Deployment{ModelName: "test"}

	// Initially not in cooldown
	if cm.InCooldown(d) {
		t.Error("Expected not in cooldown initially")
	}

	// First failure - not in cooldown yet
	cm.ReportFailure(d)
	if cm.InCooldown(d) {
		t.Error("Expected not in cooldown after 1 failure")
	}

	// Second failure - should be in cooldown
	cm.ReportFailure(d)
	if !cm.InCooldown(d) {
		t.Error("Expected in cooldown after 2 failures")
	}

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)
	if cm.InCooldown(d) {
		t.Error("Expected cooldown to expire")
	}

	// Report success resets failures
	cm.ReportFailure(d)
	cm.ReportSuccess(d)
	cm.ReportFailure(d)
	if cm.InCooldown(d) {
		t.Error("Expected not in cooldown after success reset")
	}
}
