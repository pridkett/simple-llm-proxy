package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// GetLogByID returns the full RequestLog for the given request_id.
// Returns (nil, nil) when no log exists with that request_id (caller returns 404).
func (s *Storage) GetLogByID(ctx context.Context, requestID string) (*storage.RequestLog, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT ul.request_id, ul.model, ul.provider, ul.endpoint,
		       ul.input_tokens, ul.output_tokens, ul.total_cost,
		       ul.status_code, ul.latency_ms, ul.request_time,
		       ul.is_streaming, COALESCE(ul.deployment_key, ''),
		       ul.api_key_id,
		       COALESCE(ak.name, ''),
		       COALESCE(app.name, ''),
		       COALESCE(t.name, ''),
		       COALESCE(ul.pool_name, ''),
		       ul.ttft_ms,
		       ul.req_body_snippet,
		       COALESCE(ul.resp_body_snippet, ''),
		       ul.cache_read_tokens,
		       ul.cache_write_tokens
		FROM usage_logs ul
		LEFT JOIN api_keys ak ON ul.api_key_id = ak.id
		LEFT JOIN applications app ON ak.application_id = app.id
		LEFT JOIN teams t ON app.team_id = t.id
		WHERE ul.request_id = ?
	`, requestID)

	entry := &storage.RequestLog{}
	var ttftNull sql.NullInt64
	err := row.Scan(
		&entry.RequestID, &entry.Model, &entry.Provider, &entry.Endpoint,
		&entry.InputTokens, &entry.OutputTokens, &entry.TotalCost,
		&entry.StatusCode, &entry.LatencyMS, &entry.RequestTime,
		&entry.IsStreaming, &entry.DeploymentKey,
		&entry.APIKeyID,
		&entry.KeyName, &entry.AppName, &entry.TeamName,
		&entry.PoolName,
		&ttftNull,
		&entry.ReqBodySnippet,
		&entry.RespBodySnippet,
		&entry.CacheReadTokens,
		&entry.CacheWriteTokens,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying log by id: %w", err)
	}
	if ttftNull.Valid {
		v := ttftNull.Int64
		entry.TTFTMs = &v
	}
	return entry, nil
}

// GetLogsMeta returns distinct values for all filter dimensions for dropdown population.
// All slice fields are initialized to empty (not nil) to produce JSON [] rather than null.
func (s *Storage) GetLogsMeta(ctx context.Context) (*storage.LogsMeta, error) {
	meta := &storage.LogsMeta{
		Teams:     []storage.LogsMetaIDName{},
		Apps:      []storage.LogsMetaIDName{},
		Keys:      []storage.LogsMetaIDName{},
		Providers: []string{},
		Models:    []string{},
		Pools:     []string{},
	}

	// --- Teams (id+name via JOIN chain) ---
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT t.id, t.name
		FROM usage_logs ul
		LEFT JOIN api_keys ak ON ul.api_key_id = ak.id
		LEFT JOIN applications app ON ak.application_id = app.id
		LEFT JOIN teams t ON app.team_id = t.id
		WHERE t.id IS NOT NULL
		ORDER BY t.name
	`)
	if err != nil {
		return nil, fmt.Errorf("querying log meta teams: %w", err)
	}
	for rows.Next() {
		var row storage.LogsMetaIDName
		if err := rows.Scan(&row.ID, &row.Name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning team meta: %w", err)
		}
		meta.Teams = append(meta.Teams, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterating team meta: %w", err)
	}
	rows.Close()

	// --- Apps (id+name via JOIN) ---
	rows, err = s.db.QueryContext(ctx, `
		SELECT DISTINCT app.id, app.name
		FROM usage_logs ul
		LEFT JOIN api_keys ak ON ul.api_key_id = ak.id
		LEFT JOIN applications app ON ak.application_id = app.id
		WHERE app.id IS NOT NULL
		ORDER BY app.name
	`)
	if err != nil {
		return nil, fmt.Errorf("querying log meta apps: %w", err)
	}
	for rows.Next() {
		var row storage.LogsMetaIDName
		if err := rows.Scan(&row.ID, &row.Name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning app meta: %w", err)
		}
		meta.Apps = append(meta.Apps, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterating app meta: %w", err)
	}
	rows.Close()

	// --- Keys (id+name via JOIN) ---
	rows, err = s.db.QueryContext(ctx, `
		SELECT DISTINCT ak.id, ak.name
		FROM usage_logs ul
		LEFT JOIN api_keys ak ON ul.api_key_id = ak.id
		WHERE ak.id IS NOT NULL
		ORDER BY ak.name
	`)
	if err != nil {
		return nil, fmt.Errorf("querying log meta keys: %w", err)
	}
	for rows.Next() {
		var row storage.LogsMetaIDName
		if err := rows.Scan(&row.ID, &row.Name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning key meta: %w", err)
		}
		meta.Keys = append(meta.Keys, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterating key meta: %w", err)
	}
	rows.Close()

	// --- Providers (plain string) ---
	rows, err = s.db.QueryContext(ctx, `
		SELECT DISTINCT ul.provider
		FROM usage_logs ul
		WHERE ul.provider != ''
		ORDER BY ul.provider
	`)
	if err != nil {
		return nil, fmt.Errorf("querying log meta providers: %w", err)
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning provider meta: %w", err)
		}
		meta.Providers = append(meta.Providers, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterating provider meta: %w", err)
	}
	rows.Close()

	// --- Models (plain string) ---
	rows, err = s.db.QueryContext(ctx, `
		SELECT DISTINCT ul.model
		FROM usage_logs ul
		WHERE ul.model != ''
		ORDER BY ul.model
	`)
	if err != nil {
		return nil, fmt.Errorf("querying log meta models: %w", err)
	}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning model meta: %w", err)
		}
		meta.Models = append(meta.Models, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterating model meta: %w", err)
	}
	rows.Close()

	// --- Pools (plain string; NULL guard required — pool_name is NULL for non-pooled rows) ---
	rows, err = s.db.QueryContext(ctx, `
		SELECT DISTINCT ul.pool_name
		FROM usage_logs ul
		WHERE ul.pool_name IS NOT NULL AND ul.pool_name != ''
		ORDER BY ul.pool_name
	`)
	if err != nil {
		return nil, fmt.Errorf("querying log meta pools: %w", err)
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning pool meta: %w", err)
		}
		meta.Pools = append(meta.Pools, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterating pool meta: %w", err)
	}
	rows.Close()

	return meta, nil
}
