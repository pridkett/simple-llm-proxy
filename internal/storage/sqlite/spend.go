package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// GetSpendSummary returns aggregated spend per active key for the given date range and filters.
// Flush rows (model='_flush') are excluded to prevent double-counting.
// Keys with zero spend in the date range are included (LEFT JOIN + COALESCE returns 0).
//
// NOTE: Only active keys (k.is_active = TRUE) are included. This means deactivated keys
// do not appear in historical spend views even if they had usage in the queried date range.
// This is an intentional simplification for the initial Cost view. If historical reporting
// for deactivated keys is needed in a future iteration, remove or make this filter configurable.
// GetModelSpend returns spend grouped by model for the given date range and filters.
// Uses the same JOIN and filter pattern as GetSpendSummary for consistency.
func (s *Storage) GetModelSpend(ctx context.Context, from, to time.Time, filters storage.SpendFilters) ([]storage.ModelSpendRow, error) {
	const q = `
        SELECT
            ul.model,
            COALESCE(SUM(ul.total_cost), 0) AS total_spend,
            COUNT(*)                         AS request_count
        FROM usage_logs ul
        JOIN api_keys k    ON k.id = ul.api_key_id
        JOIN applications a ON a.id = k.application_id
        JOIN teams t        ON t.id = a.team_id
        WHERE ul.model != '_flush'
          AND ul.request_time >= ?
          AND ul.request_time < ?
          AND k.is_active = TRUE
          AND (? IS NULL OR t.id = ?)
          AND (? IS NULL OR a.id = ?)
          AND (? IS NULL OR k.id = ?)
        GROUP BY ul.model
        ORDER BY total_spend DESC
    `
	args := []any{
		from, to,
		filters.TeamID, filters.TeamID,
		filters.AppID, filters.AppID,
		filters.KeyID, filters.KeyID,
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get model spend: %w", err)
	}
	defer rows.Close()

	result := make([]storage.ModelSpendRow, 0)
	for rows.Next() {
		var r storage.ModelSpendRow
		if err := rows.Scan(&r.Model, &r.TotalSpend, &r.RequestCount); err != nil {
			return nil, fmt.Errorf("get model spend scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// GetDailySpend returns spend grouped by calendar day for the given date range and filters.
// Days with zero spend are not returned — the frontend fills gaps.
func (s *Storage) GetDailySpend(ctx context.Context, from, to time.Time, filters storage.SpendFilters) ([]storage.DailySpendRow, error) {
	const q = `
        SELECT
            DATE(ul.request_time)            AS day,
            COALESCE(SUM(ul.total_cost), 0) AS total_spend,
            COUNT(*)                         AS request_count
        FROM usage_logs ul
        JOIN api_keys k    ON k.id = ul.api_key_id
        JOIN applications a ON a.id = k.application_id
        JOIN teams t        ON t.id = a.team_id
        WHERE ul.model != '_flush'
          AND ul.request_time >= ?
          AND ul.request_time < ?
          AND k.is_active = TRUE
          AND (? IS NULL OR t.id = ?)
          AND (? IS NULL OR a.id = ?)
          AND (? IS NULL OR k.id = ?)
        GROUP BY DATE(ul.request_time)
        ORDER BY day
    `
	args := []any{
		from, to,
		filters.TeamID, filters.TeamID,
		filters.AppID, filters.AppID,
		filters.KeyID, filters.KeyID,
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get daily spend: %w", err)
	}
	defer rows.Close()

	result := make([]storage.DailySpendRow, 0)
	for rows.Next() {
		var r storage.DailySpendRow
		if err := rows.Scan(&r.Day, &r.TotalSpend, &r.RequestCount); err != nil {
			return nil, fmt.Errorf("get daily spend scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Storage) GetSpendSummary(ctx context.Context, from, to time.Time, filters storage.SpendFilters) ([]storage.SpendRow, error) {
	// Build the query with optional filter predicates.
	// Uses the double-bind pattern: (? IS NULL OR col = ?) — binds the pointer value twice.
	// This is correct ONLY when Go passes nil for absent filters (not 0).
	// SpendFilters uses *int64 pointer types to enforce this contract.
	//
	// GROUP BY lists all non-aggregated selected columns explicitly to ensure correctness
	// across all SQL engines (SQLite's relaxed mode would permit omitting them, but doing
	// so is non-standard and brittle if the schema or query changes).
	const baseQuery = `
        SELECT
            k.id          AS key_id,
            k.name        AS key_name,
            k.max_budget,
            k.soft_budget,
            a.id          AS app_id,
            a.name        AS app_name,
            t.id          AS team_id,
            t.name        AS team_name,
            COALESCE(SUM(ul.total_cost), 0) AS total_spend
        FROM api_keys k
        JOIN applications a ON a.id = k.application_id
        JOIN teams t        ON t.id = a.team_id
        LEFT JOIN usage_logs ul
            ON ul.api_key_id = k.id
            AND ul.model != '_flush'
            AND ul.request_time >= ?
            AND ul.request_time < ?
        WHERE k.is_active = TRUE
          AND (? IS NULL OR t.id = ?)
          AND (? IS NULL OR a.id = ?)
          AND (? IS NULL OR k.id = ?)
        GROUP BY k.id, k.name, k.max_budget, k.soft_budget, a.id, a.name, t.id, t.name
        ORDER BY total_spend DESC
    `
	// Args: from, to, teamID, teamID, appID, appID, keyID, keyID
	args := []any{
		from, to,
		filters.TeamID, filters.TeamID,
		filters.AppID, filters.AppID,
		filters.KeyID, filters.KeyID,
	}

	rows, err := s.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("get spend summary: %w", err)
	}
	defer rows.Close()

	result := make([]storage.SpendRow, 0)
	for rows.Next() {
		var r storage.SpendRow
		if err := rows.Scan(
			&r.KeyID, &r.KeyName, &r.MaxBudget, &r.SoftBudget,
			&r.AppID, &r.AppName,
			&r.TeamID, &r.TeamName,
			&r.TotalSpend,
		); err != nil {
			return nil, fmt.Errorf("get spend summary scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
