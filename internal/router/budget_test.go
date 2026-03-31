package router

import (
	"context"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// mockBudgetStore is a minimal mock implementing only the pool budget storage methods.
type mockBudgetStore struct {
	rows []storage.PoolBudgetRow
}

func (m *mockBudgetStore) GetPoolBudgetState(_ context.Context) ([]storage.PoolBudgetRow, error) {
	return m.rows, nil
}

func (m *mockBudgetStore) UpsertPoolBudgetState(_ context.Context, poolName string, spendToday float64, resetDate string) error {
	// Update existing or append
	for i, r := range m.rows {
		if r.PoolName == poolName {
			m.rows[i].SpendToday = spendToday
			m.rows[i].ResetDate = resetDate
			return nil
		}
	}
	m.rows = append(m.rows, storage.PoolBudgetRow{
		PoolName:   poolName,
		SpendToday: spendToday,
		ResetDate:  resetDate,
	})
	return nil
}

func TestPoolBudgetHasBudgetUnlimited(t *testing.T) {
	// BUDGET-01: HasBudget returns true when BudgetCapDaily is 0 (unlimited)
	m := NewPoolBudgetManager()
	m.SetCaps([]config.ProviderPool{
		{Name: "pool-unlimited", BudgetCapDaily: 0},
	})

	if !m.HasBudget("pool-unlimited") {
		t.Error("HasBudget should return true for unlimited pool (cap=0)")
	}
}

func TestPoolBudgetHasBudgetUnderCap(t *testing.T) {
	// BUDGET-01: HasBudget returns true when spend < cap
	m := NewPoolBudgetManager()
	m.SetCaps([]config.ProviderPool{
		{Name: "pool-a", BudgetCapDaily: 100.0},
	})

	m.Credit("pool-a", 50.0)

	if !m.HasBudget("pool-a") {
		t.Error("HasBudget should return true when spend (50) < cap (100)")
	}
}

func TestPoolBudgetHasBudgetExhausted(t *testing.T) {
	// BUDGET-01: HasBudget returns false when spend >= cap
	m := NewPoolBudgetManager()
	m.SetCaps([]config.ProviderPool{
		{Name: "pool-b", BudgetCapDaily: 10.0},
	})

	m.Credit("pool-b", 10.0)

	if m.HasBudget("pool-b") {
		t.Error("HasBudget should return false when spend (10) >= cap (10)")
	}
}

func TestPoolBudgetCreditAndCurrentSpend(t *testing.T) {
	// Credit adds cost to pool's running total; CurrentSpend reflects it
	m := NewPoolBudgetManager()

	m.Credit("pool-x", 5.0)
	m.Credit("pool-x", 3.5)

	got := m.CurrentSpend("pool-x")
	if got != 8.5 {
		t.Errorf("CurrentSpend: got %f, want 8.5", got)
	}
}

func TestPoolBudgetLazyDailyResetHasBudget(t *testing.T) {
	// BUDGET-06: HasBudget resets spend to 0 when resetDate is yesterday
	m := NewPoolBudgetManager()
	m.SetCaps([]config.ProviderPool{
		{Name: "pool-stale", BudgetCapDaily: 10.0},
	})

	// Manually inject a stale entry with yesterday's date
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	ps := &poolSpend{total: 9.0, resetDate: yesterday}
	m.totals.Store("pool-stale", ps)

	// HasBudget should detect stale date and reset spend to 0
	if !m.HasBudget("pool-stale") {
		t.Error("HasBudget should return true after lazy reset (spend was 9, cap 10, but date is stale)")
	}

	// Verify spend was actually reset
	if spend := m.CurrentSpend("pool-stale"); spend != 0 {
		t.Errorf("CurrentSpend after lazy reset: got %f, want 0", spend)
	}
}

func TestPoolBudgetCreditAfterDateChange(t *testing.T) {
	// BUDGET-06: Credit after date change starts from 0 (not yesterday's total)
	m := NewPoolBudgetManager()

	// Inject stale entry
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	ps := &poolSpend{total: 50.0, resetDate: yesterday}
	m.totals.Store("pool-dated", ps)

	// Credit should detect stale date, reset to 0, then add 3.0
	m.Credit("pool-dated", 3.0)

	got := m.CurrentSpend("pool-dated")
	if got != 3.0 {
		t.Errorf("CurrentSpend after credit on stale date: got %f, want 3.0", got)
	}
}

func TestPoolBudgetFlushToStorage(t *testing.T) {
	// BUDGET-05: FlushToStorage calls UpsertPoolBudgetState for each pool with spend > 0
	m := NewPoolBudgetManager()
	m.Credit("pool-flush-a", 10.0)
	m.Credit("pool-flush-b", 0.0) // zero spend — should be skipped

	mock := &mockBudgetStore{}
	ctx := context.Background()

	if err := m.FlushToStorage(ctx, mock); err != nil {
		t.Fatalf("FlushToStorage: %v", err)
	}

	// Only pool-flush-a should have been flushed (pool-flush-b has 0 spend)
	if len(mock.rows) != 1 {
		t.Fatalf("expected 1 flushed row, got %d", len(mock.rows))
	}
	if mock.rows[0].PoolName != "pool-flush-a" {
		t.Errorf("flushed pool name: got %q, want %q", mock.rows[0].PoolName, "pool-flush-a")
	}
	if mock.rows[0].SpendToday != 10.0 {
		t.Errorf("flushed spend: got %f, want 10.0", mock.rows[0].SpendToday)
	}
	today := time.Now().UTC().Format("2006-01-02")
	if mock.rows[0].ResetDate != today {
		t.Errorf("flushed reset_date: got %q, want %q", mock.rows[0].ResetDate, today)
	}
}

func TestPoolBudgetInitFromStorage(t *testing.T) {
	// BUDGET-05: InitFromStorage loads spend_today from storage; skips rows with stale reset_date
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	mock := &mockBudgetStore{
		rows: []storage.PoolBudgetRow{
			{PoolName: "pool-fresh", SpendToday: 42.0, ResetDate: today},
			{PoolName: "pool-stale", SpendToday: 99.0, ResetDate: yesterday},
		},
	}

	m := NewPoolBudgetManager()
	ctx := context.Background()

	if err := m.InitFromStorage(ctx, mock); err != nil {
		t.Fatalf("InitFromStorage: %v", err)
	}

	// Fresh pool should be loaded
	if got := m.CurrentSpend("pool-fresh"); got != 42.0 {
		t.Errorf("pool-fresh spend: got %f, want 42.0", got)
	}

	// Stale pool should be skipped (start at 0)
	if got := m.CurrentSpend("pool-stale"); got != 0 {
		t.Errorf("pool-stale spend: got %f, want 0 (stale date should be skipped)", got)
	}
}

func TestPoolBudgetHasBudgetUnknownPool(t *testing.T) {
	// HasBudget returns true for unknown pool names (no cap = unlimited)
	m := NewPoolBudgetManager()

	if !m.HasBudget("nonexistent-pool") {
		t.Error("HasBudget should return true for unknown pool (no cap configured)")
	}
}

func TestPoolBudgetSetCapsUpdates(t *testing.T) {
	// SetCaps updates caps; HasBudget reflects new cap values
	m := NewPoolBudgetManager()

	// Initial cap: 100
	m.SetCaps([]config.ProviderPool{
		{Name: "pool-cap", BudgetCapDaily: 100.0},
	})
	m.Credit("pool-cap", 50.0)

	if !m.HasBudget("pool-cap") {
		t.Error("HasBudget should return true (50 < 100)")
	}

	// Lower cap to 30 — now 50 >= 30
	m.SetCaps([]config.ProviderPool{
		{Name: "pool-cap", BudgetCapDaily: 30.0},
	})

	if m.HasBudget("pool-cap") {
		t.Error("HasBudget should return false after cap lowered (50 >= 30)")
	}
}
