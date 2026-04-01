package router

import (
	"context"
	"fmt"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// splitHeaderEntries splits a comma-separated header value into trimmed entries.
func splitHeaderEntries(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Integration-scoped mock provider: records which deployment was called and
// allows per-call control of success/failure.
// ---------------------------------------------------------------------------

type integrationMockProvider struct {
	name string
}

func (p *integrationMockProvider) Name() string { return p.name }

func (p *integrationMockProvider) ChatCompletion(_ context.Context, _ *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{ID: "int-resp"}, nil
}

func (p *integrationMockProvider) ChatCompletionStream(_ context.Context, _ *model.ChatCompletionRequest) (provider.Stream, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *integrationMockProvider) Embeddings(_ context.Context, _ *model.EmbeddingsRequest) (*model.EmbeddingsResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *integrationMockProvider) SupportsEmbeddings() bool { return false }

// makeIntegrationDeployment creates a deployment with a specific provider name,
// model name, and actual model. Uses the integrationMockProvider so the
// deployment can be used in integration tests without hitting real APIs.
func makeIntegrationDeployment(modelName, providerName, actualModel, apiBase string) *provider.Deployment {
	return &provider.Deployment{
		ModelName:    modelName,
		Provider:     &integrationMockProvider{name: providerName},
		ProviderName: providerName,
		ActualModel:  actualModel,
		APIBase:      apiBase,
	}
}

// ---------------------------------------------------------------------------
// Test 1: Pool weighted routing — verify distribution matches configured weights
// ---------------------------------------------------------------------------

func TestIntegration_PoolWeightedRouting(t *testing.T) {
	dA := makeIntegrationDeployment("gpt-4-a", "openai", "gpt-4", "")
	dB := makeIntegrationDeployment("gpt-4-b", "anthropic", "claude-3", "")

	weights := map[string]int{
		dA.DeploymentKey(): 80,
		dB.DeploymentKey(): 20,
	}

	r := &Router{
		deployments: map[string][]*provider.Deployment{
			"gpt-4-a": {dA},
			"gpt-4-b": {dB},
		},
		settings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		strategy:    NewShuffle(),
		cooldown:    NewCooldownManager(30*time.Second, 3),
		backoff:     NewBackoffManager(),
		pools:       make(map[string]*Pool),
		modelToPool: make(map[string]*Pool),
		sticky:      NewStickySessionManager(nil),
	}

	pool := &Pool{
		Name:      "gpt-4-pool",
		Strategy:  NewWeightedRoundRobin(weights),
		Members:   []*provider.Deployment{dA, dB},
		Weights:   weights,
		ModelName: "gpt-4-a",
	}
	r.pools["gpt-4-pool"] = pool
	r.modelToPool["gpt-4-a"] = pool
	r.modelToPool["gpt-4-b"] = pool

	countA := 0
	countB := 0
	total := 100

	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "resp"}, nil, nil
	}

	for i := 0; i < total; i++ {
		result := r.Route(context.Background(), "gpt-4-a", "", cb)
		if result.Error != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, result.Error)
		}
		switch result.DeploymentUsed {
		case dA:
			countA++
		case dB:
			countB++
		default:
			t.Fatalf("iteration %d: unexpected deployment: %v", i, result.DeploymentUsed)
		}
	}

	// Assert deployment A selected ~80 times (+/-10 tolerance for smooth WRR)
	if math.Abs(float64(countA)-80) > 10 {
		t.Errorf("expected ~80 selections for A (weight 80), got %d", countA)
	}
	if math.Abs(float64(countB)-20) > 10 {
		t.Errorf("expected ~20 selections for B (weight 20), got %d", countB)
	}
	t.Logf("weighted distribution: A=%d B=%d (expected ~80/20)", countA, countB)
}

// ---------------------------------------------------------------------------
// Test 2: Pool failover on error — first deployment fails, second succeeds
// ---------------------------------------------------------------------------

func TestIntegration_PoolFailoverOnError(t *testing.T) {
	dA := makeIntegrationDeployment("gpt-4-a", "openai", "gpt-4", "")
	dB := makeIntegrationDeployment("gpt-4-b", "anthropic", "claude-3", "")

	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{dA, dB})

	// First call fails regardless of which deployment is selected;
	// second call succeeds. This avoids depending on round-robin ordering.
	callCount := 0
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		callCount++
		if callCount == 1 {
			return nil, nil, fmt.Errorf("provider is down")
		}
		return &model.ChatCompletionResponse{ID: "failover-resp"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4-a", "", cb)

	if result.Error != nil {
		t.Fatalf("expected success after failover, got: %v", result.Error)
	}
	if len(result.DeploymentsTried) != 2 {
		t.Errorf("expected 2 deployments tried, got %d", len(result.DeploymentsTried))
	}
	if len(result.FailoverReasons) != 1 || result.FailoverReasons[0] != FailoverError {
		t.Errorf("expected [FailoverError], got %v", result.FailoverReasons)
	}
	// The deployment used should be different from the first one tried
	if result.DeploymentUsed == result.DeploymentsTried[0] {
		t.Error("expected failover to use a different deployment than the first tried")
	}
}

// ---------------------------------------------------------------------------
// Test 3: Pool failover on 429 — first deployment rate limited, second succeeds
// ---------------------------------------------------------------------------

func TestIntegration_PoolFailoverOn429(t *testing.T) {
	dA := makeIntegrationDeployment("gpt-4-a", "openai", "gpt-4", "")
	dB := makeIntegrationDeployment("gpt-4-b", "anthropic", "claude-3", "")

	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{dA, dB})

	// First call returns 429 regardless of which deployment is selected;
	// second call succeeds. Track which deployment was rate-limited.
	callCount := 0
	var rateLimitedDep *provider.Deployment
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		callCount++
		if callCount == 1 {
			rateLimitedDep = d
			return nil, nil, &provider.RateLimitError{
				Provider:   d.ProviderName,
				RetryAfter: 10 * time.Second,
			}
		}
		return &model.ChatCompletionResponse{ID: "429-failover-resp"}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4-a", "", cb)

	if result.Error != nil {
		t.Fatalf("expected success after 429 failover, got: %v", result.Error)
	}
	if len(result.FailoverReasons) != 1 || result.FailoverReasons[0] != FailoverRateLimited {
		t.Errorf("expected [FailoverRateLimited], got %v", result.FailoverReasons)
	}
	if result.DeploymentUsed == rateLimitedDep {
		t.Error("expected a different deployment after 429 failover")
	}
	// Verify backoff was applied to the rate-limited deployment
	if rateLimitedDep != nil && !r.backoff.InBackoff(rateLimitedDep.DeploymentKey()) {
		t.Error("expected rate-limited deployment to be in backoff after 429")
	}
}

// ---------------------------------------------------------------------------
// Test 4: Sticky session — same key routes to same deployment
// ---------------------------------------------------------------------------

func TestIntegration_StickySession(t *testing.T) {
	dA := makeIntegrationDeployment("gpt-4", "openai", "gpt-4", "")
	dB := makeIntegrationDeployment("gpt-4", "anthropic", "claude-3", "")

	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{dA, dB})

	stickyKey := "user-key-hash-abc123"
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "sticky-resp"}, nil, nil
	}

	// First call: establishes sticky mapping
	result1 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result1.Error != nil {
		t.Fatalf("first call: %v", result1.Error)
	}
	firstUsed := result1.DeploymentUsed
	if firstUsed == nil {
		t.Fatal("expected DeploymentUsed on first call")
	}

	// Second call with same key: must route to same deployment
	result2 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result2.Error != nil {
		t.Fatalf("second call: %v", result2.Error)
	}
	if result2.DeploymentUsed != firstUsed {
		t.Errorf("sticky session broken: first=%s, second=%s",
			firstUsed.DeploymentKey(), result2.DeploymentUsed.DeploymentKey())
	}

	// Third call with same key: still sticky
	result3 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result3.Error != nil {
		t.Fatalf("third call: %v", result3.Error)
	}
	if result3.DeploymentUsed != firstUsed {
		t.Errorf("sticky session broken on third call: first=%s, third=%s",
			firstUsed.DeploymentKey(), result3.DeploymentUsed.DeploymentKey())
	}

	// Call with different key: may get either deployment (not necessarily the same)
	differentKey := "different-user-hash-xyz789"
	result4 := r.Route(context.Background(), "gpt-4", differentKey, cb)
	if result4.Error != nil {
		t.Fatalf("different key call: %v", result4.Error)
	}
	// Just verify it succeeds — the specific deployment depends on strategy
	if result4.DeploymentUsed == nil {
		t.Error("expected a deployment for different key")
	}
}

// ---------------------------------------------------------------------------
// Test 5: Sticky session failover — preferred deployment enters cooldown
// ---------------------------------------------------------------------------

func TestIntegration_StickySessionFailover(t *testing.T) {
	dA := makeIntegrationDeployment("gpt-4", "openai", "gpt-4", "")
	dB := makeIntegrationDeployment("gpt-4", "anthropic", "claude-3", "")

	r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{dA, dB})
	// Use AllowedFails=1 so a single failure triggers cooldown
	r.cooldown = NewCooldownManager(30*time.Second, 1)

	stickyKey := "sticky-failover-key"
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "ok"}, nil, nil
	}

	// First call: establishes sticky mapping to A (round-robin picks A first)
	result1 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result1.Error != nil {
		t.Fatalf("first call: %v", result1.Error)
	}
	stickyDep := result1.DeploymentUsed

	// Put the sticky deployment into cooldown
	r.ReportFailure(stickyDep)
	if !r.cooldown.InCooldown(stickyDep) {
		t.Fatal("expected sticky deployment to be in cooldown")
	}

	// Second call: sticky is in cooldown, should fail over
	result2 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result2.Error != nil {
		t.Fatalf("second call: %v", result2.Error)
	}
	if result2.DeploymentUsed == stickyDep {
		t.Error("expected failover to different deployment when sticky is in cooldown")
	}

	// Verify sticky mapping was updated to the new deployment
	result3 := r.Route(context.Background(), "gpt-4", stickyKey, cb)
	if result3.Error != nil {
		t.Fatalf("third call: %v", result3.Error)
	}
	if result3.DeploymentUsed != result2.DeploymentUsed {
		t.Error("expected sticky mapping to persist with new deployment")
	}
}

// ---------------------------------------------------------------------------
// Test 6: Backward compatibility — no provider_pools uses legacy routing
// ---------------------------------------------------------------------------

func TestIntegration_BackwardCompatNoPool(t *testing.T) {
	// Create router via full config path (no provider_pools)
	cfg := makeMockConfig([]string{"gpt-4", "claude-3"}, "round-robin")
	r, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Verify no pools were created
	if len(r.Pools()) != 0 {
		t.Errorf("expected 0 pools for config without provider_pools, got %d", len(r.Pools()))
	}

	// Route() should use legacy path
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "legacy-" + d.ModelName}, nil, nil
	}

	result := r.Route(context.Background(), "gpt-4", "", cb)
	if result.Error != nil {
		t.Fatalf("legacy route error: %v", result.Error)
	}
	if result.DeploymentUsed == nil {
		t.Fatal("expected DeploymentUsed to be set")
	}
	if result.Response.ID != "legacy-gpt-4" {
		t.Errorf("expected legacy-gpt-4, got %s", result.Response.ID)
	}

	// Verify failover works in legacy mode too
	callCount := 0
	failCb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		callCount++
		if callCount == 1 {
			return nil, nil, fmt.Errorf("first attempt fails")
		}
		return &model.ChatCompletionResponse{ID: "retry-ok"}, nil, nil
	}

	// Need a model with multiple deployments for retry.
	// Use "gpt-4" which has only 1 deployment, so configure with 2.
	multiCfg := &config.Config{
		ModelList: []config.ModelConfig{
			{
				ModelName:     "multi",
				LiteLLMParams: config.LiteLLMParams{Model: "mock/multi-a", APIKey: "k1"},
			},
			{
				ModelName:     "multi",
				LiteLLMParams: config.LiteLLMParams{Model: "mock/multi-b", APIKey: "k2"},
			},
		},
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "round-robin",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
	}
	rMulti, err := New(multiCfg, nil)
	if err != nil {
		t.Fatalf("New multi: %v", err)
	}

	callCount = 0
	resultMulti := rMulti.Route(context.Background(), "multi", "", failCb)
	if resultMulti.Error != nil {
		t.Fatalf("expected legacy retry to succeed, got: %v", resultMulti.Error)
	}
	if len(resultMulti.DeploymentsTried) < 2 {
		t.Errorf("expected at least 2 deployments tried in legacy retry, got %d", len(resultMulti.DeploymentsTried))
	}
}

// ---------------------------------------------------------------------------
// Test 7: Headers end-to-end — verify response headers from RouteResult
// ---------------------------------------------------------------------------

func TestIntegration_HeadersEndToEnd(t *testing.T) {
	t.Run("success headers", func(t *testing.T) {
		dA := makeIntegrationDeployment("gpt-4-a", "openai", "gpt-4", "https://custom.api.com/v1")
		r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{dA})

		cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
			return &model.ChatCompletionResponse{ID: "hdr-resp"}, nil, nil
		}

		result := r.Route(context.Background(), "gpt-4-a", "", cb)
		if result.Error != nil {
			t.Fatalf("route error: %v", result.Error)
		}

		w := httptest.NewRecorder()
		SetRouteHeaders(w, result)

		// X-Provider-Used: "provider/model"
		providerUsed := w.Header().Get(HeaderProviderUsed)
		if providerUsed != "openai/gpt-4" {
			t.Errorf("X-Provider-Used: got %q, want %q", providerUsed, "openai/gpt-4")
		}

		// X-Provider-URL-Used: custom base URL
		urlUsed := w.Header().Get(HeaderProviderURLUsed)
		if urlUsed != "https://custom.api.com/v1" {
			t.Errorf("X-Provider-URL-Used: got %q, want %q", urlUsed, "https://custom.api.com/v1")
		}

		// X-Providers-Tried: single entry
		tried := w.Header().Get(HeaderProvidersTried)
		if tried != "openai/gpt-4" {
			t.Errorf("X-Providers-Tried: got %q, want %q", tried, "openai/gpt-4")
		}

		// X-Failover-Reason: absent on success
		failover := w.Header().Get(HeaderFailoverReason)
		if failover != "" {
			t.Errorf("X-Failover-Reason: expected empty, got %q", failover)
		}
	})

	t.Run("failover headers", func(t *testing.T) {
		dA := makeIntegrationDeployment("gpt-4-a", "openai", "gpt-4", "")
		dB := makeIntegrationDeployment("gpt-4-b", "anthropic", "claude-3", "https://api.anthropic.com")
		r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{dA, dB})

		// First call fails, second succeeds — independent of ordering.
		callCount := 0
		cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
			callCount++
			if callCount == 1 {
				return nil, nil, fmt.Errorf("provider error")
			}
			return &model.ChatCompletionResponse{ID: "hdr-failover-resp"}, nil, nil
		}

		result := r.Route(context.Background(), "gpt-4-a", "", cb)
		if result.Error != nil {
			t.Fatalf("route error: %v", result.Error)
		}

		w := httptest.NewRecorder()
		SetRouteHeaders(w, result)

		// X-Provider-Used: should be the successful deployment
		providerUsed := w.Header().Get(HeaderProviderUsed)
		if providerUsed == "" {
			t.Error("X-Provider-Used: expected non-empty value")
		}

		// X-Providers-Tried: should list both deployments
		tried := w.Header().Get(HeaderProvidersTried)
		if tried == "" {
			t.Error("X-Providers-Tried: expected non-empty value")
		}
		// Should contain exactly 2 entries (comma-separated)
		parts := splitHeaderEntries(tried)
		if len(parts) != 2 {
			t.Errorf("X-Providers-Tried: expected 2 entries, got %d: %q", len(parts), tried)
		}

		// X-Failover-Reason: should contain "error"
		failover := w.Header().Get(HeaderFailoverReason)
		if failover != "error" {
			t.Errorf("X-Failover-Reason: got %q, want %q", failover, "error")
		}
	})

	t.Run("default URL for known providers", func(t *testing.T) {
		dA := makeIntegrationDeployment("gpt-4-a", "openai", "gpt-4", "")
		r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{dA})

		cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
			return &model.ChatCompletionResponse{ID: "resp"}, nil, nil
		}

		result := r.Route(context.Background(), "gpt-4-a", "", cb)
		if result.Error != nil {
			t.Fatalf("route error: %v", result.Error)
		}

		w := httptest.NewRecorder()
		SetRouteHeaders(w, result)

		// X-Provider-URL-Used: should be the default OpenAI URL
		urlUsed := w.Header().Get(HeaderProviderURLUsed)
		if urlUsed != "https://api.openai.com/v1" {
			t.Errorf("X-Provider-URL-Used: got %q, want %q", urlUsed, "https://api.openai.com/v1")
		}
	})

	t.Run("429 failover reason in headers", func(t *testing.T) {
		dA := makeIntegrationDeployment("gpt-4-a", "openai", "gpt-4", "")
		dB := makeIntegrationDeployment("gpt-4-b", "anthropic", "claude-3", "")
		r := makeRouterWithPool("gpt-4-pool", []*provider.Deployment{dA, dB})

		callCount := 0
		cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
			callCount++
			if callCount == 1 {
				return nil, nil, &provider.RateLimitError{Provider: d.ProviderName, RetryAfter: 5 * time.Second}
			}
			return &model.ChatCompletionResponse{ID: "resp-429"}, nil, nil
		}

		result := r.Route(context.Background(), "gpt-4-a", "", cb)
		if result.Error != nil {
			t.Fatalf("route error: %v", result.Error)
		}

		w := httptest.NewRecorder()
		SetRouteHeaders(w, result)

		failover := w.Header().Get(HeaderFailoverReason)
		if failover != "rate_limited" {
			t.Errorf("X-Failover-Reason: got %q, want %q", failover, "rate_limited")
		}
	})

	t.Run("nil result produces no headers", func(t *testing.T) {
		w := httptest.NewRecorder()
		SetRouteHeaders(w, nil)
		if w.Header().Get(HeaderProviderUsed) != "" {
			t.Error("expected no X-Provider-Used for nil result")
		}
	})
}

// ---------------------------------------------------------------------------
// Test: Full config pipeline — config with provider_pools builds correct pools
// ---------------------------------------------------------------------------

func TestIntegration_ConfigToPoolPipeline(t *testing.T) {
	cfg := &config.Config{
		ModelList: []config.ModelConfig{
			{
				ModelName:     "gpt-4-primary",
				LiteLLMParams: config.LiteLLMParams{Model: "mock/gpt-4-primary", APIKey: "key1"},
			},
			{
				ModelName:     "gpt-4-fallback",
				LiteLLMParams: config.LiteLLMParams{Model: "mock/gpt-4-fallback", APIKey: "key2"},
			},
		},
		ProviderPools: []config.ProviderPool{
			{
				Name:     "gpt-4-pool",
				Strategy: "weighted-round-robin",
				Members: []config.PoolMember{
					{ModelName: "gpt-4-primary", Weight: 80},
					{ModelName: "gpt-4-fallback", Weight: 20},
				},
			},
		},
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "simple-shuffle",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
	}

	r, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Verify pool was created
	pools := r.Pools()
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(pools))
	}
	pool, ok := pools["gpt-4-pool"]
	if !ok {
		t.Fatal("expected pool named gpt-4-pool")
	}
	if len(pool.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(pool.Members))
	}

	// Verify model-to-pool mapping
	mtp := r.ModelToPool()
	if _, ok := mtp["gpt-4-primary"]; !ok {
		t.Error("expected gpt-4-primary mapped to pool")
	}
	if _, ok := mtp["gpt-4-fallback"]; !ok {
		t.Error("expected gpt-4-fallback mapped to pool")
	}

	// Route through pool — should succeed
	cb := func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		return &model.ChatCompletionResponse{ID: "config-pipeline-" + d.ModelName}, nil, nil
	}
	result := r.Route(context.Background(), "gpt-4-primary", "", cb)
	if result.Error != nil {
		t.Fatalf("route error: %v", result.Error)
	}
	if result.DeploymentUsed == nil {
		t.Fatal("expected a deployment to be used")
	}
}
