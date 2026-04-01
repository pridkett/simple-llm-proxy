package webhook

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
)

// mockProvider implements the provider.Provider interface for testing.
type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) ChatCompletion(_ interface{}, _ interface{}) (interface{}, error) {
	return nil, nil
}
func (m *mockProvider) ChatCompletionStream(_ interface{}, _ interface{}) (interface{}, error) {
	return nil, nil
}
func (m *mockProvider) Embeddings(_ interface{}, _ interface{}) (interface{}, error) {
	return nil, nil
}
func (m *mockProvider) SupportsEmbeddings() bool { return false }

func TestNewProviderFailoverEvent(t *testing.T) {
	dep1 := &provider.Deployment{
		ModelName:    "gpt-4",
		ProviderName: "openai",
		ActualModel:  "gpt-4",
		APIBase:      "https://api.openai.com",
	}
	dep2 := &provider.Deployment{
		ModelName:    "gpt-4",
		ProviderName: "anthropic",
		ActualModel:  "claude-3-haiku",
		APIBase:      "https://api.anthropic.com",
	}

	result := &router.RouteResult{
		DeploymentUsed:   dep2,
		DeploymentsTried: []*provider.Deployment{dep1, dep2},
		FailoverReasons:  []router.FailoverReason{router.FailoverCooldown},
		PoolName:         "primary-pool",
	}

	event := NewProviderFailoverEvent("gpt-4", result)

	// Check event type
	if event.Type != EventProviderFailover {
		t.Errorf("expected type %q, got %q", EventProviderFailover, event.Type)
	}

	// Check timestamp is recent
	if time.Since(event.Timestamp) > 5*time.Second {
		t.Errorf("timestamp too old: %v", event.Timestamp)
	}
	if event.Timestamp.IsZero() {
		t.Error("timestamp is zero")
	}

	// Check value1 = model name
	if event.Value1 != "gpt-4" {
		t.Errorf("expected value1 %q, got %q", "gpt-4", event.Value1)
	}

	// Check value2 = deployment path
	if !strings.Contains(event.Value2, "openai/gpt-4") {
		t.Errorf("value2 should contain openai/gpt-4, got %q", event.Value2)
	}
	if !strings.Contains(event.Value2, "anthropic/claude-3-haiku") {
		t.Errorf("value2 should contain anthropic/claude-3-haiku, got %q", event.Value2)
	}
	if !strings.Contains(event.Value2, " -> ") {
		t.Errorf("value2 should contain ' -> ' separator, got %q", event.Value2)
	}

	// Check value3 = failover reasons
	if !strings.Contains(event.Value3, "cooldown") {
		t.Errorf("value3 should contain 'cooldown', got %q", event.Value3)
	}

	// Check context
	ctx := event.Context
	if ctx["model"] != "gpt-4" {
		t.Errorf("context model should be gpt-4, got %v", ctx["model"])
	}
	if ctx["pool_name"] != "primary-pool" {
		t.Errorf("context pool_name should be primary-pool, got %v", ctx["pool_name"])
	}

	providersTried, ok := ctx["providers_tried"].([]string)
	if !ok {
		t.Fatal("context providers_tried should be []string")
	}
	if len(providersTried) != 2 {
		t.Errorf("expected 2 providers tried, got %d", len(providersTried))
	}

	providerUsed, ok := ctx["provider_used"].(string)
	if !ok {
		t.Fatal("context provider_used should be string")
	}
	if providerUsed != "anthropic/claude-3-haiku" {
		t.Errorf("expected provider_used anthropic/claude-3-haiku, got %q", providerUsed)
	}
}

func TestNewProviderFailoverEventNilDeploymentUsed(t *testing.T) {
	dep1 := &provider.Deployment{
		ModelName:    "gpt-4",
		ProviderName: "openai",
		ActualModel:  "gpt-4",
	}

	result := &router.RouteResult{
		DeploymentUsed:   nil,
		DeploymentsTried: []*provider.Deployment{dep1},
		FailoverReasons:  []router.FailoverReason{router.FailoverError},
	}

	event := NewProviderFailoverEvent("gpt-4", result)

	// provider_used should be empty when no deployment succeeded
	providerUsed, ok := event.Context["provider_used"].(string)
	if !ok {
		t.Fatal("context provider_used should be string")
	}
	if providerUsed != "" {
		t.Errorf("expected empty provider_used when DeploymentUsed is nil, got %q", providerUsed)
	}
}

func TestNewBudgetExhaustedEvent(t *testing.T) {
	dep1 := &provider.Deployment{
		ModelName:    "gpt-4",
		ProviderName: "openai",
		ActualModel:  "gpt-4",
	}

	result := &router.RouteResult{
		DeploymentsTried: []*provider.Deployment{dep1},
		FailoverReasons:  []router.FailoverReason{router.FailoverBudgetExhausted},
		PoolName:         "budget-pool",
	}

	event := NewBudgetExhaustedEvent("gpt-4", result)

	// Check event type
	if event.Type != EventBudgetExhausted {
		t.Errorf("expected type %q, got %q", EventBudgetExhausted, event.Type)
	}

	// Check timestamp
	if event.Timestamp.IsZero() {
		t.Error("timestamp is zero")
	}

	// Check value1 = model name
	if event.Value1 != "gpt-4" {
		t.Errorf("expected value1 %q, got %q", "gpt-4", event.Value1)
	}

	// Check value2 = pool name
	if event.Value2 != "budget-pool" {
		t.Errorf("expected value2 %q, got %q", "budget-pool", event.Value2)
	}

	// Check value3
	if event.Value3 != "daily budget exhausted" {
		t.Errorf("expected value3 'daily budget exhausted', got %q", event.Value3)
	}

	// Check context
	ctx := event.Context
	if ctx["model"] != "gpt-4" {
		t.Errorf("context model should be gpt-4, got %v", ctx["model"])
	}
	if ctx["pool_name"] != "budget-pool" {
		t.Errorf("context pool_name should be budget-pool, got %v", ctx["pool_name"])
	}
	if ctx["budget_remaining"] != nil {
		t.Errorf("budget_remaining should be nil, got %v", ctx["budget_remaining"])
	}
	// Ensure budget_remaining key exists
	if _, exists := ctx["budget_remaining"]; !exists {
		t.Error("context should have budget_remaining key")
	}
}

func TestNewPoolCooldownEvent(t *testing.T) {
	members := []*provider.Deployment{
		{
			ModelName:    "gpt-4",
			ProviderName: "openai",
			ActualModel:  "gpt-4",
			APIBase:      "https://api.openai.com",
		},
		{
			ModelName:    "gpt-4",
			ProviderName: "anthropic",
			ActualModel:  "claude-3-haiku",
			APIBase:      "",
		},
	}

	event := NewPoolCooldownEvent("production-pool", members)

	// Check event type
	if event.Type != EventPoolCooldown {
		t.Errorf("expected type %q, got %q", EventPoolCooldown, event.Type)
	}

	// Check timestamp
	if event.Timestamp.IsZero() {
		t.Error("timestamp is zero")
	}

	// Check value1 = pool name
	if event.Value1 != "production-pool" {
		t.Errorf("expected value1 %q, got %q", "production-pool", event.Value1)
	}

	// Check value2 = comma-joined deployment keys
	key1 := members[0].DeploymentKey()
	key2 := members[1].DeploymentKey()
	if !strings.Contains(event.Value2, key1) {
		t.Errorf("value2 should contain %q, got %q", key1, event.Value2)
	}
	if !strings.Contains(event.Value2, key2) {
		t.Errorf("value2 should contain %q, got %q", key2, event.Value2)
	}

	// Check value3
	if event.Value3 != "all members in cooldown" {
		t.Errorf("expected value3 'all members in cooldown', got %q", event.Value3)
	}

	// Check context
	ctx := event.Context
	if ctx["pool_name"] != "production-pool" {
		t.Errorf("context pool_name should be production-pool, got %v", ctx["pool_name"])
	}

	contextMembers, ok := ctx["members"].([]string)
	if !ok {
		t.Fatal("context members should be []string")
	}
	if len(contextMembers) != 2 {
		t.Errorf("expected 2 members, got %d", len(contextMembers))
	}

	memberCount, ok := ctx["member_count"].(int)
	if !ok {
		t.Fatal("context member_count should be int")
	}
	if memberCount != 2 {
		t.Errorf("expected member_count 2, got %d", memberCount)
	}
}

func TestEventJSON(t *testing.T) {
	dep1 := &provider.Deployment{
		ModelName:    "gpt-4",
		ProviderName: "openai",
		ActualModel:  "gpt-4",
	}
	dep2 := &provider.Deployment{
		ModelName:    "gpt-4",
		ProviderName: "anthropic",
		ActualModel:  "claude-3-haiku",
	}

	result := &router.RouteResult{
		DeploymentUsed:   dep2,
		DeploymentsTried: []*provider.Deployment{dep1, dep2},
		FailoverReasons:  []router.FailoverReason{router.FailoverCooldown},
		PoolName:         "test-pool",
	}

	event := NewProviderFailoverEvent("gpt-4", result)

	data, err := event.JSON()
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}

	// Parse back and verify all required keys
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	requiredKeys := []string{"event_type", "timestamp", "value1", "value2", "value3", "context"}
	for _, key := range requiredKeys {
		if _, ok := parsed[key]; !ok {
			t.Errorf("JSON missing required key %q", key)
		}
	}

	// Verify event_type value
	if parsed["event_type"] != "provider_failover" {
		t.Errorf("JSON event_type should be 'provider_failover', got %v", parsed["event_type"])
	}
}

func TestEventTimestampsAreUTC(t *testing.T) {
	members := []*provider.Deployment{
		{ProviderName: "openai", ActualModel: "gpt-4"},
	}
	result := &router.RouteResult{
		DeploymentsTried: []*provider.Deployment{members[0]},
		PoolName:         "pool",
	}

	events := []Event{
		NewProviderFailoverEvent("gpt-4", result),
		NewBudgetExhaustedEvent("gpt-4", result),
		NewPoolCooldownEvent("pool", members),
	}

	for i, event := range events {
		if event.Timestamp.Location() != time.UTC {
			t.Errorf("event[%d] timestamp should be UTC, got %v", i, event.Timestamp.Location())
		}
		if time.Since(event.Timestamp) > 5*time.Second {
			t.Errorf("event[%d] timestamp too old: %v", i, event.Timestamp)
		}
	}
}
