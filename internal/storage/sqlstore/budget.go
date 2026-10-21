package sqlstore

import (
	"context"
	"fmt"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// GetPoolBudgetState returns all pool budget rows from the pool_budget_state table.
// Used at startup to initialize the PoolBudgetManager in-memory accumulator.
func (s *Storage) GetPoolBudgetState(ctx context.Context) ([]storage.PoolBudgetRow, error) {
	resetDate := "date(reset_date)"
	if s.dialect == PostgreSQL {
		resetDate = "to_char(reset_date, 'YYYY-MM-DD')"
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT pool_name, spend_today, `+resetDate+` FROM pool_budget_state`)
	if err != nil {
		return nil, fmt.Errorf("query pool_budget_state: %w", err)
	}
	defer rows.Close()

	var result []storage.PoolBudgetRow
	for rows.Next() {
		var r storage.PoolBudgetRow
		if err := rows.Scan(&r.PoolName, &r.SpendToday, &r.ResetDate); err != nil {
			return nil, fmt.Errorf("scan pool_budget_state row: %w", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pool_budget_state: %w", err)
	}
	return result, nil
}

// UpsertPoolBudgetState creates or updates the budget state for a pool.
// Uses ON CONFLICT on the pool_name primary key. The resetDate
// parameter must be a "2006-01-02" formatted UTC date string.
func (s *Storage) UpsertPoolBudgetState(ctx context.Context, poolName string, spendToday float64, resetDate string) error {
	_, err := s.db.ExecContext(ctx,
		s.bind(`INSERT INTO pool_budget_state (pool_name, spend_today, reset_date)
		 VALUES (?, ?, ?) ON CONFLICT(pool_name) DO UPDATE SET
		 spend_today = excluded.spend_today, reset_date = excluded.reset_date`),
		poolName, spendToday, resetDate)
	if err != nil {
		return fmt.Errorf("upsert pool_budget_state(%q): %w", poolName, err)
	}
	return nil
}
