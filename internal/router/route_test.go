package router

import (
	"context"
	"fmt"
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
	r, err := New(cfg)
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
