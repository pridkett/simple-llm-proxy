package router

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// BudgetStore is the subset of storage.Storage that PoolBudgetManager needs.
// Accepting an interface rather than the full Storage type makes testing trivial
// and keeps the coupling narrow.
type BudgetStore interface {
	GetPoolBudgetState(ctx context.Context) ([]storage.PoolBudgetRow, error)
	UpsertPoolBudgetState(ctx context.Context, poolName string, spendToday float64, resetDate string) error
}

// poolSpend holds a per-pool spend total and the date it applies to.
// The mutex protects both fields — callers must hold the lock when reading or writing.
type poolSpend struct {
	mu        sync.Mutex
	total     float64
	resetDate string // "2006-01-02" UTC
}

// PoolBudgetManager tracks per-pool daily spend in memory with sub-millisecond reads.
// It mirrors the SpendAccumulator pattern from internal/keystore/spend.go.
// - caps: loaded from config.ProviderPool.BudgetCapDaily (0 = unlimited)
// - totals: running spend per pool; lazily reset when the date changes
type PoolBudgetManager struct {
	totals sync.Map // map[string]*poolSpend (poolName -> spend)
	caps   sync.Map // map[string]float64 (poolName -> daily cap; 0 = unlimited)
}

// NewPoolBudgetManager creates a new PoolBudgetManager with no caps or spend loaded.
// Call SetCaps() to load budget caps from config and InitFromStorage() to restore state.
func NewPoolBudgetManager() *PoolBudgetManager {
	return &PoolBudgetManager{}
}

// SetCaps loads BudgetCapDaily from config for each pool. Called from Router.New()
// and Router.Reload(). Caps of 0 mean unlimited (no enforcement).
func (m *PoolBudgetManager) SetCaps(pools []config.ProviderPool) {
	for _, p := range pools {
		m.caps.Store(p.Name, p.BudgetCapDaily)
	}
}

// HasBudget returns true if the pool has remaining budget for today.
// Returns true for pools with no cap (BudgetCapDaily == 0) or unknown pools.
// Performs lazy daily reset: if the stored resetDate is stale (not today), the
// spend total is zeroed before comparison.
func (m *PoolBudgetManager) HasBudget(poolName string) bool {
	// Look up cap. Unknown pool or cap <= 0 means unlimited.
	capVal, ok := m.caps.Load(poolName)
	if !ok {
		return true
	}
	cap := capVal.(float64)
	if cap <= 0 {
		return true
	}

	// Look up current spend. No entry means no spend yet.
	val, ok := m.totals.Load(poolName)
	if !ok {
		return true
	}

	ps := val.(*poolSpend)
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Lazy daily reset: if the date is stale, zero the total.
	today := time.Now().UTC().Format("2006-01-02")
	if ps.resetDate != today {
		ps.total = 0
		ps.resetDate = today
	}

	return ps.total < cap
}

// Credit adds cost to the pool's running total for today.
// If the stored resetDate is stale (not today), the total is zeroed before
// adding the cost — ensuring a clean daily boundary.
func (m *PoolBudgetManager) Credit(poolName string, cost float64) {
	today := time.Now().UTC().Format("2006-01-02")

	actual, _ := m.totals.LoadOrStore(poolName, &poolSpend{resetDate: today})
	ps := actual.(*poolSpend)

	ps.mu.Lock()
	// Lazy reset on date change
	if ps.resetDate != today {
		ps.total = 0
		ps.resetDate = today
	}
	ps.total += cost
	ps.mu.Unlock()
}

// CurrentSpend returns the current in-memory spend total for the pool.
// Returns 0 if the pool has no recorded spend.
func (m *PoolBudgetManager) CurrentSpend(poolName string) float64 {
	val, ok := m.totals.Load(poolName)
	if !ok {
		return 0
	}
	ps := val.(*poolSpend)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.total
}

// InitFromStorage loads spend totals from the pool_budget_state table.
// Rows with a stale resetDate (not today) are skipped — the pool starts fresh.
// Must be called at startup before handling any requests.
func (m *PoolBudgetManager) InitFromStorage(ctx context.Context, store BudgetStore) error {
	rows, err := store.GetPoolBudgetState(ctx)
	if err != nil {
		return fmt.Errorf("pool budget init: %w", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	for _, r := range rows {
		if r.ResetDate != today {
			continue // stale — start fresh
		}
		ps := &poolSpend{
			total:     r.SpendToday,
			resetDate: r.ResetDate,
		}
		m.totals.Store(r.PoolName, ps)
	}
	return nil
}

// FlushToStorage persists all pool spend totals to the pool_budget_state table.
// Called periodically by the flush loop in main.go and once on graceful shutdown.
// Mirrors the SpendAccumulator.FlushToStorage pattern.
func (m *PoolBudgetManager) FlushToStorage(ctx context.Context, store BudgetStore) error {
	var firstErr error
	m.totals.Range(func(k, v any) bool {
		poolName := k.(string)
		ps := v.(*poolSpend)

		ps.mu.Lock()
		total := ps.total
		resetDate := ps.resetDate
		ps.mu.Unlock()

		if total <= 0 {
			return true // skip zero-spend pools
		}

		if err := store.UpsertPoolBudgetState(ctx, poolName, total, resetDate); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
		return true
	})
	return firstErr
}
