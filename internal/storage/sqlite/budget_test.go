package sqlite

import (
	"context"
	"testing"
)

func TestPoolBudgetGetEmpty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	rows, err := s.GetPoolBudgetState(ctx)
	if err != nil {
		t.Fatalf("GetPoolBudgetState on empty table: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestPoolBudgetUpsertAndGet(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.UpsertPoolBudgetState(ctx, "pool-a", 12.50, "2026-03-31")
	if err != nil {
		t.Fatalf("UpsertPoolBudgetState: %v", err)
	}

	rows, err := s.GetPoolBudgetState(ctx)
	if err != nil {
		t.Fatalf("GetPoolBudgetState: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.PoolName != "pool-a" {
		t.Errorf("PoolName: got %q, want %q", r.PoolName, "pool-a")
	}
	if r.SpendToday != 12.50 {
		t.Errorf("SpendToday: got %f, want 12.50", r.SpendToday)
	}
	if r.ResetDate != "2026-03-31" {
		t.Errorf("ResetDate: got %q, want %q", r.ResetDate, "2026-03-31")
	}
}

func TestPoolBudgetUpsertUpdate(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert initial row
	if err := s.UpsertPoolBudgetState(ctx, "pool-b", 5.00, "2026-03-30"); err != nil {
		t.Fatalf("initial UpsertPoolBudgetState: %v", err)
	}

	// Update same pool_name
	if err := s.UpsertPoolBudgetState(ctx, "pool-b", 25.75, "2026-03-31"); err != nil {
		t.Fatalf("update UpsertPoolBudgetState: %v", err)
	}

	rows, err := s.GetPoolBudgetState(ctx)
	if err != nil {
		t.Fatalf("GetPoolBudgetState: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", len(rows))
	}
	r := rows[0]
	if r.SpendToday != 25.75 {
		t.Errorf("SpendToday after update: got %f, want 25.75", r.SpendToday)
	}
	if r.ResetDate != "2026-03-31" {
		t.Errorf("ResetDate after update: got %q, want %q", r.ResetDate, "2026-03-31")
	}
}

func TestPoolBudgetMultiplePools(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	pools := []struct {
		name  string
		spend float64
		date  string
	}{
		{"pool-alpha", 10.0, "2026-03-31"},
		{"pool-beta", 20.5, "2026-03-31"},
		{"pool-gamma", 0.0, "2026-03-31"},
	}

	for _, p := range pools {
		if err := s.UpsertPoolBudgetState(ctx, p.name, p.spend, p.date); err != nil {
			t.Fatalf("UpsertPoolBudgetState(%q): %v", p.name, err)
		}
	}

	rows, err := s.GetPoolBudgetState(ctx)
	if err != nil {
		t.Fatalf("GetPoolBudgetState: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// Build lookup for verification
	byName := make(map[string]float64)
	for _, r := range rows {
		byName[r.PoolName] = r.SpendToday
	}

	if byName["pool-alpha"] != 10.0 {
		t.Errorf("pool-alpha spend: got %f, want 10.0", byName["pool-alpha"])
	}
	if byName["pool-beta"] != 20.5 {
		t.Errorf("pool-beta spend: got %f, want 20.5", byName["pool-beta"])
	}
	if byName["pool-gamma"] != 0.0 {
		t.Errorf("pool-gamma spend: got %f, want 0.0", byName["pool-gamma"])
	}
}
