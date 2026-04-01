package router

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// makeRouterWithPool creates a Router with a pool containing the given deployments.
// The pool strategy uses shuffle for deterministic tests with a single member.
func makeRouterWithPool(poolName string, deployments []*provider.Deployment) *Router {
	r := &Router{
		deployments: make(map[string][]*provider.Deployment),
		settings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		strategy:    NewRoundRobin(), // deterministic ordering for pool member iteration
		cooldown:    NewCooldownManager(30*time.Second, 3),
		backoff:     NewBackoffManager(),
		pools:       make(map[string]*Pool),
		modelToPool: make(map[string]*Pool),
		sticky:      NewStickySessionManager(nil),
		budget:      NewPoolBudgetManager(),
	}

	pool := &Pool{
		Name:     poolName,
		Strategy: NewRoundRobin(),
		Members:  deployments,
	}
	r.pools[poolName] = pool

	// Map each deployment's model name to this pool.
	seen := make(map[string]bool)
	for _, d := range deployments {
		if !seen[d.ModelName] {
			r.modelToPool[d.ModelName] = pool
			seen[d.ModelName] = true
		}
		r.deployments[d.ModelName] = append(r.deployments[d.ModelName], d)
	}

	return r
}

func makeDeployment(modelName, providerName, actualModel string) *provider.Deployment {
	return &provider.Deployment{
		ModelName:    modelName,
		Provider:     &mockProvider{name: providerName},
		ProviderName: providerName,
		ActualModel:  actualModel,
	}
}

func TestRoute_PoolSuccess(t *testing.T) {
	d1 := makeDeployment("gpt-4-a", "openai", "gpt-4")
	d2 := makeDeployment("gpt-4-b", "openai", "gpt-4")
	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{d1, d2})

	callCount := 0
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		callCount++
		return &model.ChatCompletionResponse{ID: "resp-1"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4-a", "", cb)

	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if result.DeploymentUsed == nil {
		t.Fatal("expected DeploymentUsed to be set")
	}
	if result.Response == nil || result.Response.ID != "resp-1" {
		t.Error("expected Response with ID resp-1")
	}
	if len(result.DeploymentsTried) != 1 {
		t.Errorf("expected 1 deployment tried, got %d", len(result.DeploymentsTried))
	}
	if len(result.FailoverReasons) != 0 {
		t.Errorf("expected 0 failover reasons, got %d", len(result.FailoverReasons))
	}
	if callCount != 1 {
		t.Errorf("expected callback called once, got %d", callCount)
	}
}

func TestRoute_PoolFailover(t *testing.T) {
	d1 := makeDeployment("gpt-4-a", "openai", "gpt-4")
	d2 := makeDeployment("gpt-4-b", "anthropic", "claude-3")
	d3 := makeDeployment("gpt-4-c", "openai", "gpt-4-turbo")
	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{d1, d2, d3})

	callCount := 0
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		callCount++
		if callCount == 1 {
			return nil, nil, fmt.Errorf("provider error")
		}
		return &model.ChatCompletionResponse{ID: "resp-2"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4-a", "", cb)

	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if len(result.DeploymentsTried) != 2 {
		t.Errorf("expected 2 deployments tried, got %d", len(result.DeploymentsTried))
	}
	if len(result.FailoverReasons) != 1 {
		t.Errorf("expected 1 failover reason, got %d", len(result.FailoverReasons))
	}
	if result.FailoverReasons[0] != FailoverError {
		t.Errorf("expected FailoverError, got %s", result.FailoverReasons[0])
	}
	if result.DeploymentUsed == result.DeploymentsTried[0] {
		t.Error("expected DeploymentUsed to be the second deployment, not the first")
	}
	if result.Response == nil || result.Response.ID != "resp-2" {
		t.Error("expected Response with ID resp-2")
	}
}

func TestRoute_PoolRateLimit(t *testing.T) {
	d1 := makeDeployment("gpt-4-a", "openai", "gpt-4")
	d2 := makeDeployment("gpt-4-b", "anthropic", "claude-3")
	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{d1, d2})

	callCount := 0
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		callCount++
		if callCount == 1 {
			return nil, nil, &provider.RateLimitError{
				Provider:   "openai",
				RetryAfter: 5 * time.Second,
			}
		}
		return &model.ChatCompletionResponse{ID: "resp-3"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4-a", "", cb)

	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if len(result.FailoverReasons) != 1 {
		t.Fatalf("expected 1 failover reason, got %d", len(result.FailoverReasons))
	}
	if result.FailoverReasons[0] != FailoverRateLimited {
		t.Errorf("expected FailoverRateLimited, got %s", result.FailoverReasons[0])
	}
	if result.Response == nil || result.Response.ID != "resp-3" {
		t.Error("expected Response with ID resp-3")
	}
}

func TestRoute_PoolAllExhausted(t *testing.T) {
	d1 := makeDeployment("gpt-4-a", "openai", "gpt-4")
	d2 := makeDeployment("gpt-4-b", "anthropic", "claude-3")
	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{d1, d2})

	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return nil, nil, fmt.Errorf("all down")
	}

	result := r.Route(context.Background(), "gpt-4-a", "", cb)

	if result.Error == nil {
		t.Fatal("expected error when all pool members exhausted")
	}
	if len(result.DeploymentsTried) != 2 {
		t.Errorf("expected 2 deployments tried, got %d", len(result.DeploymentsTried))
	}
	if result.DeploymentUsed != nil {
		t.Error("expected DeploymentUsed to be nil")
	}
}

func TestRoute_LegacyPathNoPool(t *testing.T) {
	// Create a router with model_list entries but no pool.
	cfg := makeMockConfig([]string{"gpt-4"}, "simple-shuffle")
	r, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "legacy-resp"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4", "", cb)

	if result.Error != nil {
		t.Fatalf("expected no error, got: %v", result.Error)
	}
	if result.DeploymentUsed == nil {
		t.Fatal("expected DeploymentUsed to be set")
	}
	if result.Response == nil || result.Response.ID != "legacy-resp" {
		t.Error("expected Response with ID legacy-resp")
	}
	if len(result.DeploymentsTried) != 1 {
		t.Errorf("expected 1 deployment tried, got %d", len(result.DeploymentsTried))
	}
}

func TestRoute_ContextCancelled(t *testing.T) {
	d1 := makeDeployment("gpt-4-a", "openai", "gpt-4")
	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{d1})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	callCount := 0
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		callCount++
		return &model.ChatCompletionResponse{ID: "should-not-reach"}, nil, nil
	}

	result := r.Route(ctx, "gpt-4-a", "", cb)

	if result.Error == nil {
		t.Fatal("expected error for cancelled context")
	}
	if result.Error != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", result.Error)
	}
	if callCount != 0 {
		t.Errorf("expected callback not called, got %d calls", callCount)
	}
}

func TestRoute_StickySessionPersists(t *testing.T) {
	// Pool of 2 deployments with distinct DeploymentKey() values.
	d1 := makeDeployment("gpt-4", "openai", "gpt-4")
	d2 := makeDeployment("gpt-4", "anthropic", "claude-3")
	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{d1, d2})

	stickyKey := "hash-key-abc"
	var firstUsed *provider.Deployment

	// First call: establish sticky mapping.
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "resp"}, nil, nil
	}

	result1 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result1.Error != nil {
		t.Fatalf("first call: %v", result1.Error)
	}
	firstUsed = result1.DeploymentUsed
	if firstUsed == nil {
		t.Fatal("expected DeploymentUsed to be set on first call")
	}

	// Second call with same stickyKey: should get the same deployment.
	result2 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result2.Error != nil {
		t.Fatalf("second call: %v", result2.Error)
	}
	if result2.DeploymentUsed != firstUsed {
		t.Errorf("sticky session did not persist: first=%s second=%s",
			firstUsed.DeploymentKey(), result2.DeploymentUsed.DeploymentKey())
	}
}

func TestRoute_StickySessionFailover(t *testing.T) {
	// Pool of 2 deployments.
	d1 := makeDeployment("gpt-4", "openai", "gpt-4")
	d2 := makeDeployment("gpt-4", "anthropic", "claude-3")
	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{d1, d2})

	// Use AllowedFails=1 so failures trigger cooldown immediately.
	r.cooldown = NewCooldownManager(30*time.Second, 1)

	stickyKey := "hash-key-xyz"

	// First call: establish sticky mapping.
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "ok"}, nil, nil
	}
	result1 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result1.Error != nil {
		t.Fatalf("first call: %v", result1.Error)
	}
	stickyDep := result1.DeploymentUsed

	// Put the sticky deployment into cooldown by reporting failures.
	r.ReportFailure(stickyDep)

	// Now the sticky deployment should be in cooldown.
	if !r.cooldown.InCooldown(stickyDep) {
		t.Fatal("expected sticky deployment to be in cooldown")
	}

	// Second call: sticky deployment is unhealthy, should failover.
	result2 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result2.Error != nil {
		t.Fatalf("second call: %v", result2.Error)
	}
	if result2.DeploymentUsed == stickyDep {
		t.Error("expected failover to different deployment when sticky is in cooldown")
	}
	if result2.DeploymentUsed == nil {
		t.Fatal("expected a deployment to be used after failover")
	}

	// Third call: sticky should have been updated to the new deployment.
	result3 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result3.Error != nil {
		t.Fatalf("third call: %v", result3.Error)
	}
	if result3.DeploymentUsed != result2.DeploymentUsed {
		t.Error("expected sticky to persist with failover deployment")
	}
}

func TestRoute_StickySessionNoKeySkips(t *testing.T) {
	d1 := makeDeployment("gpt-4", "openai", "gpt-4")
	d2 := makeDeployment("gpt-4", "anthropic", "claude-3")
	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{d1, d2})

	// Route with empty stickyKey: no sticky logic should be invoked.
	var deployments []*provider.Deployment
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		deployments = append(deployments, d)
		return &model.ChatCompletionResponse{ID: "resp"}, nil, nil
	}

	// Call multiple times — without sticky, round-robin will alternate.
	for i := 0; i < 4; i++ {
		result := r.Route(context.Background(), "gpt-4", "", cb)
		if result.Error != nil {
			t.Fatalf("call %d: %v", i, result.Error)
		}
	}

	// Verify sticky cache is empty (no entries for empty key).
	got := r.sticky.Get("", "gpt-4-pool")
	if got != "" {
		t.Errorf("expected empty sticky cache for empty key, got %q", got)
	}
}

// makeRouterWithPoolAndBudget creates a Router with a pool and a PoolBudgetManager
// that has the given cap set. Used by budget-related route tests.
func makeRouterWithPoolAndBudget(poolName string, deployments []*provider.Deployment, budgetCap float64) *Router {
	r := makeRouterWithPool(poolName, deployments)
	r.budget = NewPoolBudgetManager()
	r.budget.SetCaps([]config.ProviderPool{
		{Name: poolName, BudgetCapDaily: budgetCap},
	})
	return r
}

func TestRoute_PoolBudgetExhausted(t *testing.T) {
	d1 := makeDeployment("gpt-4", "openai", "gpt-4")
	d2 := makeDeployment("gpt-4", "anthropic", "claude-3")
	r := makeRouterWithPoolAndBudget("gpt-4-pool", []*provider.Deployment{d1, d2}, 10.0)

	// Credit beyond the cap to exhaust the budget.
	r.budget.Credit("gpt-4-pool", 15.0)

	callCount := 0
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		callCount++
		return &model.ChatCompletionResponse{ID: "resp"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4", "", cb)

	if result.Error == nil {
		t.Fatal("expected error for budget-exhausted pool")
	}
	if callCount != 0 {
		t.Errorf("expected callback NOT called (budget check is pre-request), got %d calls", callCount)
	}

	foundBudgetReason := false
	for _, reason := range result.FailoverReasons {
		if reason == FailoverBudgetExhausted {
			foundBudgetReason = true
			break
		}
	}
	if !foundBudgetReason {
		t.Error("expected FailoverBudgetExhausted in FailoverReasons")
	}
}

func TestRoute_PoolBudgetFailover(t *testing.T) {
	// Two pools, each with one deployment, both serving the same model name.
	d1 := makeDeployment("gpt-4", "openai", "gpt-4")
	d2 := makeDeployment("gpt-4", "anthropic", "claude-3")

	r := &Router{
		deployments: make(map[string][]*provider.Deployment),
		settings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		strategy:    NewRoundRobin(),
		cooldown:    NewCooldownManager(30*time.Second, 3),
		backoff:     NewBackoffManager(),
		pools:       make(map[string]*Pool),
		modelToPool: make(map[string]*Pool),
		sticky:      NewStickySessionManager(nil),
		budget:      NewPoolBudgetManager(),
	}

	pool1 := &Pool{
		Name:     "pool-primary",
		Strategy: NewRoundRobin(),
		Members:  []*provider.Deployment{d1},
	}
	pool2 := &Pool{
		Name:     "pool-fallback",
		Strategy: NewRoundRobin(),
		Members:  []*provider.Deployment{d2},
	}

	r.pools["pool-primary"] = pool1
	r.pools["pool-fallback"] = pool2
	// Map model name to the primary pool (Route() looks up primary first).
	r.modelToPool["gpt-4"] = pool1
	r.deployments["gpt-4"] = []*provider.Deployment{d1, d2}

	// Set budgets: primary exhausted, fallback has budget.
	r.budget.SetCaps([]config.ProviderPool{
		{Name: "pool-primary", BudgetCapDaily: 10.0},
		{Name: "pool-fallback", BudgetCapDaily: 100.0},
	})
	r.budget.Credit("pool-primary", 15.0) // exceed cap

	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "resp-fallback"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4", "", cb)

	if result.Error != nil {
		t.Fatalf("expected no error (failover to pool-fallback), got: %v", result.Error)
	}
	if result.DeploymentUsed != d2 {
		t.Errorf("expected deployment from pool-fallback, got %s", result.DeploymentUsed.ProviderName)
	}
	if result.PoolName != "pool-fallback" {
		t.Errorf("expected PoolName=pool-fallback, got %q", result.PoolName)
	}

	// Verify budget_exhausted reason was recorded for the primary pool.
	foundBudgetReason := false
	for _, reason := range result.FailoverReasons {
		if reason == FailoverBudgetExhausted {
			foundBudgetReason = true
			break
		}
	}
	if !foundBudgetReason {
		t.Error("expected FailoverBudgetExhausted in FailoverReasons from primary pool")
	}
}

func TestRoute_AllPoolsBudgetExhausted(t *testing.T) {
	d1 := makeDeployment("gpt-4", "openai", "gpt-4")
	d2 := makeDeployment("gpt-4", "anthropic", "claude-3")

	r := &Router{
		deployments: make(map[string][]*provider.Deployment),
		settings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		strategy:    NewRoundRobin(),
		cooldown:    NewCooldownManager(30*time.Second, 3),
		backoff:     NewBackoffManager(),
		pools:       make(map[string]*Pool),
		modelToPool: make(map[string]*Pool),
		sticky:      NewStickySessionManager(nil),
		budget:      NewPoolBudgetManager(),
	}

	pool1 := &Pool{Name: "pool-a", Strategy: NewRoundRobin(), Members: []*provider.Deployment{d1}}
	pool2 := &Pool{Name: "pool-b", Strategy: NewRoundRobin(), Members: []*provider.Deployment{d2}}

	r.pools["pool-a"] = pool1
	r.pools["pool-b"] = pool2
	r.modelToPool["gpt-4"] = pool1
	r.deployments["gpt-4"] = []*provider.Deployment{d1, d2}

	// Exhaust both pools.
	r.budget.SetCaps([]config.ProviderPool{
		{Name: "pool-a", BudgetCapDaily: 10.0},
		{Name: "pool-b", BudgetCapDaily: 10.0},
	})
	r.budget.Credit("pool-a", 15.0)
	r.budget.Credit("pool-b", 15.0)

	callCount := 0
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		callCount++
		return &model.ChatCompletionResponse{ID: "should-not-reach"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4", "", cb)

	if result.Error == nil {
		t.Fatal("expected error when all pools budget-exhausted")
	}
	if callCount != 0 {
		t.Errorf("expected callback NOT called, got %d calls", callCount)
	}
	if !strings.Contains(result.Error.Error(), "budget exhausted") {
		t.Errorf("expected error message to contain 'budget exhausted', got: %v", result.Error)
	}
}

func TestRoute_BudgetUnlimitedWhenNoCap(t *testing.T) {
	d1 := makeDeployment("gpt-4", "openai", "gpt-4")
	// Cap = 0 means unlimited.
	r := makeRouterWithPoolAndBudget("gpt-4-pool", []*provider.Deployment{d1}, 0)

	// Credit a large amount — should still have budget.
	r.budget.Credit("gpt-4-pool", 999999.0)

	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "unlimited"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4", "", cb)

	if result.Error != nil {
		t.Fatalf("expected no error with unlimited budget, got: %v", result.Error)
	}
	if result.Response == nil || result.Response.ID != "unlimited" {
		t.Error("expected successful response")
	}
}

func TestRoute_PoolNameOnRouteResult(t *testing.T) {
	d1 := makeDeployment("gpt-4", "openai", "gpt-4")
	r := makeRouterWithPoolAndBudget("my-pool", []*provider.Deployment{d1}, 100.0)

	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "resp"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4", "", cb)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.PoolName != "my-pool" {
		t.Errorf("expected PoolName='my-pool', got %q", result.PoolName)
	}
}
