package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// UpsertCostMapKey sets a cost map key override for the given proxy model name,
// clearing any custom spec that was previously stored.
func (s *Storage) UpsertCostMapKey(ctx context.Context, modelName, costMapKey string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cost_overrides (model_name, cost_map_key, custom_spec, updated_at)
		VALUES (?, ?, NULL, datetime('now'))
		ON CONFLICT(model_name) DO UPDATE SET
			cost_map_key = excluded.cost_map_key,
			custom_spec  = NULL,
			updated_at   = excluded.updated_at
	`, modelName, costMapKey)
	if err != nil {
		return fmt.Errorf("upserting cost map key: %w", err)
	}
	return nil
}

// UpsertCustomCostSpec stores a custom cost spec (JSON-encoded ModelSpec) for the given
// proxy model name, clearing any cost map key override that was previously stored.
func (s *Storage) UpsertCustomCostSpec(ctx context.Context, modelName, specJSON string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cost_overrides (model_name, cost_map_key, custom_spec, updated_at)
		VALUES (?, NULL, ?, datetime('now'))
		ON CONFLICT(model_name) DO UPDATE SET
			cost_map_key = NULL,
			custom_spec  = excluded.custom_spec,
			updated_at   = excluded.updated_at
	`, modelName, specJSON)
	if err != nil {
		return fmt.Errorf("upserting custom cost spec: %w", err)
	}
	return nil
}

// GetCostOverride returns the cost override for the given proxy model name.
// Returns (nil, nil) if no override exists.
func (s *Storage) GetCostOverride(ctx context.Context, modelName string) (*storage.CostOverride, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT model_name, cost_map_key, custom_spec, updated_at
		FROM cost_overrides
		WHERE model_name = ?
	`, modelName)

	var ov storage.CostOverride
	var costMapKey, customSpec sql.NullString
	if err := row.Scan(&ov.ModelName, &costMapKey, &customSpec, &ov.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scanning cost override: %w", err)
	}
	if costMapKey.Valid {
		ov.CostMapKey = &costMapKey.String
	}
	if customSpec.Valid {
		ov.CustomSpec = &customSpec.String
	}
	return &ov, nil
}

// ListCostOverrides returns all stored cost overrides ordered by model name.
func (s *Storage) ListCostOverrides(ctx context.Context) ([]*storage.CostOverride, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT model_name, cost_map_key, custom_spec, updated_at
		FROM cost_overrides
		ORDER BY model_name
	`)
	if err != nil {
		return nil, fmt.Errorf("querying cost overrides: %w", err)
	}
	defer rows.Close()

	var overrides []*storage.CostOverride
	for rows.Next() {
		var ov storage.CostOverride
		var costMapKey, customSpec sql.NullString
		if err := rows.Scan(&ov.ModelName, &costMapKey, &customSpec, &ov.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning cost override row: %w", err)
		}
		if costMapKey.Valid {
			ov.CostMapKey = &costMapKey.String
		}
		if customSpec.Valid {
			ov.CustomSpec = &customSpec.String
		}
		overrides = append(overrides, &ov)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating cost overrides: %w", err)
	}
	return overrides, nil
}
