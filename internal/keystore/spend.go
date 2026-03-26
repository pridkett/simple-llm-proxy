package keystore

import (
	"context"
	"fmt"
	"sync"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// keySpend holds a per-key spend total protected by a mutex.
// float64 is not atomically incrementable without unsafe; mutex is the clean solution.
type keySpend struct {
	mu    sync.Mutex
	total float64
}

// SpendAccumulator tracks running spend totals in memory.
// Initialized from usage_logs on startup. Source of truth is the DB — this is a
// performance cache that avoids per-request DB reads on the hot path.
type SpendAccumulator struct {
	totals sync.Map // map[int64]*keySpend (keyID -> spend)
}

// NewSpendAccumulator creates a new SpendAccumulator.
func NewSpendAccumulator() *SpendAccumulator {
	return &SpendAccumulator{}
}

// InitFromStorage loads spend totals from usage_logs.
// Must be called at startup before handling any requests.
func (sa *SpendAccumulator) InitFromStorage(ctx context.Context, store storage.Storage) error {
	totals, err := store.GetKeySpendTotals(ctx)
	if err != nil {
		return fmt.Errorf("spend accumulator init: %w", err)
	}
	for keyID, total := range totals {
		ks := &keySpend{total: total}
		sa.totals.Store(keyID, ks)
	}
	return nil
}

// AcceptSpend returns true if accepting the given cost would keep the key within maxBudget.
// Does NOT record the spend — call Credit() after the request succeeds.
// If maxBudget <= 0 (unlimited), always returns true.
func (sa *SpendAccumulator) AcceptSpend(keyID int64, cost float64, maxBudget float64) bool {
	if maxBudget <= 0 {
		return true
	}
	current := sa.CurrentSpend(keyID)
	return current+cost <= maxBudget
}

// Credit adds cost to the in-memory total for the key. Called after a successful request.
func (sa *SpendAccumulator) Credit(keyID int64, cost float64) {
	actual, _ := sa.totals.LoadOrStore(keyID, &keySpend{})
	ks := actual.(*keySpend)
	ks.mu.Lock()
	ks.total += cost
	ks.mu.Unlock()
}

// CurrentSpend returns the current in-memory spend total for the key.
// Returns 0 if the key has no recorded spend.
func (sa *SpendAccumulator) CurrentSpend(keyID int64) float64 {
	if v, ok := sa.totals.Load(keyID); ok {
		ks := v.(*keySpend)
		ks.mu.Lock()
		defer ks.mu.Unlock()
		return ks.total
	}
	return 0
}
