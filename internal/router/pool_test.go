package router

import (
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
)

// makeMockConfigWithPools builds a config with the given models, strategy, and pools.
func makeMockConfigWithPools(models []string, strategy string, pools []config.ProviderPool) *config.Config {
	cfg := makeMockConfig(models, strategy)
	cfg.ProviderPools = pools
	return cfg
}

func TestPoolInit_BasicPool(t *testing.T) {
	cfg := makeMockConfigWithPools(
		[]string{"gpt-4-primary", "gpt-4-fallback"},
		"simple-shuffle",
		[]config.ProviderPool{
			{
				Name:     "gpt-4-pool",
				Strategy: "round-robin",
				Members: []config.PoolMember{
					{ModelName: "gpt-4-primary", Weight: 1},
					{ModelName: "gpt-4-fallback", Weight: 1},
				},
			},
		},
	)

	r, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pools := r.Pools()
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(pools))
	}

	pool, ok := pools["gpt-4-pool"]
	if !ok {
		t.Fatal("expected pool 'gpt-4-pool' to exist")
	}

	if len(pool.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(pool.Members))
	}

	// Check modelToPool maps both model names
	m2p := r.ModelToPool()
	if _, ok := m2p["gpt-4-primary"]; !ok {
		t.Error("expected modelToPool to contain gpt-4-primary")
	}
	if _, ok := m2p["gpt-4-fallback"]; !ok {
		t.Error("expected modelToPool to contain gpt-4-fallback")
	}
}

func TestPoolInit_WeightedStrategy(t *testing.T) {
	cfg := makeMockConfigWithPools(
		[]string{"gpt-4-primary", "gpt-4-fallback"},
		"simple-shuffle",
		[]config.ProviderPool{
			{
				Name:     "gpt-4-pool",
				Strategy: "weighted-round-robin",
				Members: []config.PoolMember{
					{ModelName: "gpt-4-primary", Weight: 80},
					{ModelName: "gpt-4-fallback", Weight: 20},
				},
			},
		},
	)

	r, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pool := r.GetPool("gpt-4-pool")
	if pool == nil {
		t.Fatal("expected pool 'gpt-4-pool' to exist")
	}

	// Assert pool.Strategy is *WeightedRoundRobin
	if _, ok := pool.Strategy.(*WeightedRoundRobin); !ok {
		t.Errorf("expected strategy to be *WeightedRoundRobin, got %T", pool.Strategy)
	}

	// Verify weights are set correctly
	for _, m := range pool.Members {
		key := m.DeploymentKey()
		w, ok := pool.Weights[key]
		if !ok {
			t.Errorf("expected weight for %s", key)
			continue
		}
		if m.ModelName == "gpt-4-primary" && w != 80 {
			t.Errorf("expected weight 80 for gpt-4-primary, got %d", w)
		}
		if m.ModelName == "gpt-4-fallback" && w != 20 {
			t.Errorf("expected weight 20 for gpt-4-fallback, got %d", w)
		}
	}
}

func TestPoolInit_DefaultStrategy(t *testing.T) {
	cfg := makeMockConfigWithPools(
		[]string{"gpt-4-primary", "gpt-4-fallback"},
		"simple-shuffle", // global strategy
		[]config.ProviderPool{
			{
				Name:     "gpt-4-pool",
				Strategy: "", // empty -> should use global
				Members: []config.PoolMember{
					{ModelName: "gpt-4-primary", Weight: 1},
					{ModelName: "gpt-4-fallback", Weight: 1},
				},
			},
		},
	)

	r, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pool := r.GetPool("gpt-4-pool")
	if pool == nil {
		t.Fatal("expected pool 'gpt-4-pool' to exist")
	}

	// When strategy is empty, pool uses the global router strategy.
	// Global is "simple-shuffle" -> *Shuffle.
	if _, ok := pool.Strategy.(*Shuffle); !ok {
		t.Errorf("expected strategy to be *Shuffle (global default), got %T", pool.Strategy)
	}
}

func TestPoolInit_NoPool(t *testing.T) {
	cfg := makeMockConfig([]string{"gpt-4"}, "simple-shuffle")
	// No ProviderPools

	r, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pools := r.Pools()
	if len(pools) != 0 {
		t.Errorf("expected 0 pools, got %d", len(pools))
	}

	m2p := r.ModelToPool()
	if len(m2p) != 0 {
		t.Errorf("expected 0 modelToPool entries, got %d", len(m2p))
	}

	// Deployments should still work normally
	d, err := r.GetDeployment("gpt-4")
	if err != nil {
		t.Errorf("expected deployment for gpt-4, got error: %v", err)
	}
	if d == nil {
		t.Error("expected non-nil deployment for gpt-4")
	}
}

func TestPoolInit_ReloadRebuilds(t *testing.T) {
	// Initial config with 1 pool
	cfg := makeMockConfigWithPools(
		[]string{"gpt-4-primary", "gpt-4-fallback"},
		"simple-shuffle",
		[]config.ProviderPool{
			{
				Name:     "pool-a",
				Strategy: "round-robin",
				Members: []config.PoolMember{
					{ModelName: "gpt-4-primary", Weight: 1},
				},
			},
		},
	)

	r, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if len(r.Pools()) != 1 {
		t.Fatalf("expected 1 pool before reload, got %d", len(r.Pools()))
	}
	if r.GetPool("pool-a") == nil {
		t.Fatal("expected pool-a before reload")
	}

	// Reload with different pool config
	newCfg := &config.Config{
		ModelList: []config.ModelConfig{
			{ModelName: "claude-3", LiteLLMParams: config.LiteLLMParams{Model: "mock/claude-3", APIKey: "key"}},
			{ModelName: "claude-3-sonnet", LiteLLMParams: config.LiteLLMParams{Model: "mock/claude-3-sonnet", APIKey: "key"}},
		},
		RouterSettings: config.RouterSettings{
			RoutingStrategy: "round-robin",
			NumRetries:      2,
			AllowedFails:    3,
			CooldownTime:    30 * time.Second,
		},
		ProviderPools: []config.ProviderPool{
			{
				Name:     "pool-b",
				Strategy: "weighted-round-robin",
				Members: []config.PoolMember{
					{ModelName: "claude-3", Weight: 70},
					{ModelName: "claude-3-sonnet", Weight: 30},
				},
			},
		},
	}

	if err := r.Reload(newCfg); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// pool-a should be gone, pool-b should exist
	pools := r.Pools()
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool after reload, got %d", len(pools))
	}
	if r.GetPool("pool-a") != nil {
		t.Error("expected pool-a to be gone after reload")
	}
	if r.GetPool("pool-b") == nil {
		t.Fatal("expected pool-b to exist after reload")
	}

	// Verify new pool has correct strategy type
	poolB := r.GetPool("pool-b")
	if _, ok := poolB.Strategy.(*WeightedRoundRobin); !ok {
		t.Errorf("expected pool-b strategy to be *WeightedRoundRobin, got %T", poolB.Strategy)
	}

	// Verify modelToPool reflects new config
	m2p := r.ModelToPool()
	if _, ok := m2p["claude-3"]; !ok {
		t.Error("expected modelToPool to contain claude-3 after reload")
	}
	if _, ok := m2p["gpt-4-primary"]; ok {
		t.Error("expected modelToPool to NOT contain gpt-4-primary after reload")
	}
}
