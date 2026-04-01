package webhook

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
)

// EventType identifies the kind of webhook event.
type EventType string

const (
	// EventProviderFailover fires when a request fails over from one provider to another.
	EventProviderFailover EventType = "provider_failover"

	// EventBudgetExhausted fires when a pool's daily budget is fully consumed.
	EventBudgetExhausted EventType = "budget_exhausted"

	// EventPoolCooldown fires when all members of a pool enter cooldown simultaneously.
	EventPoolCooldown EventType = "pool_cooldown"
)

// Event is the IFTTT-compatible webhook payload. It carries three string values
// (value1, value2, value3) for simple integrations plus a structured context
// map for richer consumers.
type Event struct {
	Type      EventType              `json:"event_type"`
	Timestamp time.Time              `json:"timestamp"`
	Value1    string                 `json:"value1"`
	Value2    string                 `json:"value2"`
	Value3    string                 `json:"value3"`
	Context   map[string]interface{} `json:"context"`
}

// JSON marshals the event to JSON bytes.
func (e Event) JSON() ([]byte, error) {
	return json.Marshal(e)
}

// NewProviderFailoverEvent creates a provider_failover event from a RouteResult.
//
// value1 = model name (e.g., "gpt-4")
// value2 = deployment path joined with " -> " (e.g., "openai/gpt-4 -> anthropic/claude-3-haiku")
// value3 = comma-joined failover reasons
// context includes model, pool_name, providers_tried, provider_used, failover_reasons.
func NewProviderFailoverEvent(model string, result *router.RouteResult) Event {
	// Build providers_tried list and deployment path from DeploymentsTried.
	providersTried := make([]string, 0, len(result.DeploymentsTried))
	for _, d := range result.DeploymentsTried {
		providersTried = append(providersTried, d.ProviderName+"/"+d.ActualModel)
	}

	// Build provider_used from DeploymentUsed (empty if nil).
	providerUsed := ""
	if result.DeploymentUsed != nil {
		providerUsed = result.DeploymentUsed.ProviderName + "/" + result.DeploymentUsed.ActualModel
	}

	// Build failover reasons list.
	failoverReasons := make([]string, 0, len(result.FailoverReasons))
	for _, r := range result.FailoverReasons {
		failoverReasons = append(failoverReasons, string(r))
	}

	return Event{
		Type:      EventProviderFailover,
		Timestamp: time.Now().UTC(),
		Value1:    model,
		Value2:    strings.Join(providersTried, " -> "),
		Value3:    strings.Join(failoverReasons, ", "),
		Context: map[string]interface{}{
			"model":            model,
			"pool_name":        result.PoolName,
			"providers_tried":  providersTried,
			"provider_used":    providerUsed,
			"failover_reasons": failoverReasons,
		},
	}
}

// NewBudgetExhaustedEvent creates a budget_exhausted event from a RouteResult.
//
// value1 = model name
// value2 = pool name
// value3 = "daily budget exhausted"
// context includes model, pool_name, failover_reasons, budget_remaining (always nil).
func NewBudgetExhaustedEvent(model string, result *router.RouteResult) Event {
	failoverReasons := make([]string, 0, len(result.FailoverReasons))
	for _, r := range result.FailoverReasons {
		failoverReasons = append(failoverReasons, string(r))
	}

	return Event{
		Type:      EventBudgetExhausted,
		Timestamp: time.Now().UTC(),
		Value1:    model,
		Value2:    result.PoolName,
		Value3:    "daily budget exhausted",
		Context: map[string]interface{}{
			"model":            model,
			"pool_name":        result.PoolName,
			"failover_reasons": failoverReasons,
			"budget_remaining": nil,
		},
	}
}

// NewPoolCooldownEvent creates a pool_cooldown event when all pool members are in cooldown.
//
// value1 = pool name
// value2 = comma-joined deployment keys of all members
// value3 = "all members in cooldown"
// context includes pool_name, members (deployment keys), member_count.
func NewPoolCooldownEvent(poolName string, members []*provider.Deployment) Event {
	memberKeys := make([]string, 0, len(members))
	for _, m := range members {
		memberKeys = append(memberKeys, m.DeploymentKey())
	}

	return Event{
		Type:      EventPoolCooldown,
		Timestamp: time.Now().UTC(),
		Value1:    poolName,
		Value2:    strings.Join(memberKeys, ", "),
		Value3:    "all members in cooldown",
		Context: map[string]interface{}{
			"pool_name":    poolName,
			"members":      memberKeys,
			"member_count": len(members),
		},
	}
}
