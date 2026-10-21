package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// GetLogs returns paginated request logs ordered by most recent first.
// It LEFT JOINs through api_keys -> applications -> teams to resolve names.
// The filters parameter allows optional filtering by model, team, or application.
func (s *Storage) GetLogs(ctx context.Context, limit, offset int, filters storage.LogsFilter) ([]*storage.RequestLog, int, error) {
	// Internal spend snapshots are not requests, even when explicitly filtered.
	whereClauses := []string{"ul.model != '_flush'"}
	var whereArgs []interface{}

	if filters.RequestType == "decision" {
		whereClauses = append(whereClauses, "ul.endpoint IN ('/v1/decisions', '/v1/systemone')")
	} else if filters.RequestType == "other" {
		whereClauses = append(whereClauses, "ul.endpoint NOT IN ('/v1/decisions', '/v1/systemone')")
	}
	if filters.SessionID != "" {
		// Match the partial-index predicate explicitly so SQLite can use it
		// with a bound session ID.
		whereClauses = append(whereClauses, "ul.session_id <> ''", "ul.session_id = ?")
		whereArgs = append(whereArgs, filters.SessionID)
	}
	if filters.Model != "" {
		whereClauses = append(whereClauses, "ul.model = ?")
		whereArgs = append(whereArgs, filters.Model)
	}
	if filters.TeamID != nil {
		whereClauses = append(whereClauses, "t.id = ?")
		whereArgs = append(whereArgs, *filters.TeamID)
	}
	if filters.AppID != nil {
		whereClauses = append(whereClauses, "app.id = ?")
		whereArgs = append(whereArgs, *filters.AppID)
	}
	if filters.Provider != "" {
		whereClauses = append(whereClauses, "ul.provider = ?")
		whereArgs = append(whereArgs, filters.Provider)
	}
	if filters.PoolName != "" {
		whereClauses = append(whereClauses, "ul.pool_name = ?")
		whereArgs = append(whereArgs, filters.PoolName)
	}
	if filters.KeyID != nil {
		whereClauses = append(whereClauses, "ak.id = ?")
		whereArgs = append(whereArgs, *filters.KeyID)
	}
	if filters.DateFrom != nil {
		whereClauses = append(whereClauses, "ul.request_time >= ?")
		whereArgs = append(whereArgs, filters.DateFrom.UTC())
	}
	if filters.DateTo != nil {
		whereClauses = append(whereClauses, "ul.request_time <= ?")
		whereArgs = append(whereArgs, filters.DateTo.UTC())
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = " WHERE " + whereClauses[0]
		for _, c := range whereClauses[1:] {
			whereSQL += " AND " + c
		}
	}

	// Count total matching rows.
	countQuery := `
		SELECT COUNT(*)
		FROM usage_logs ul
		LEFT JOIN api_keys ak ON ul.api_key_id = ak.id
		LEFT JOIN applications app ON ak.application_id = app.id
		LEFT JOIN teams t ON app.team_id = t.id` + whereSQL

	var total int
	err := s.db.QueryRowContext(ctx, s.bind(countQuery), whereArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting logs: %w", err)
	}

	// Fetch the page of logs with enriched fields.
	dataQuery := `
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
		       ul.cache_write_tokens, ul.usage_known, ul.session_id, ul.accounting_id, ul.cost_source, ul.cost_known, ul.cost_coverage
		FROM usage_logs ul
		LEFT JOIN api_keys ak ON ul.api_key_id = ak.id
		LEFT JOIN applications app ON ak.application_id = app.id
		LEFT JOIN teams t ON app.team_id = t.id` + whereSQL + `
		ORDER BY ul.request_time DESC
		LIMIT ? OFFSET ?`

	dataArgs := append(whereArgs, limit, offset)
	rows, err := s.db.QueryContext(ctx, s.bind(dataQuery), dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying logs: %w", err)
	}
	defer rows.Close()

	var logs []*storage.RequestLog
	for rows.Next() {
		entry := &storage.RequestLog{}
		var ttftNull sql.NullInt64
		if err := rows.Scan(
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
			&entry.CacheReadTokens,  // plain int — NOT NULL DEFAULT 0 in DB
			&entry.CacheWriteTokens, // plain int — NOT NULL DEFAULT 0 in DB
			&entry.UsageKnown, &entry.SessionID, &entry.AccountingID, &entry.CostSource, &entry.CostKnown, &entry.CostCoverage,
		); err != nil {
			return nil, 0, fmt.Errorf("scanning log: %w", err)
		}
		if ttftNull.Valid {
			v := ttftNull.Int64
			entry.TTFTMs = &v
		}
		logs = append(logs, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating logs: %w", err)
	}

	return logs, total, nil
}

// LogRequest logs a request to the usage_logs table.
func (s *Storage) LogRequest(ctx context.Context, log *storage.RequestLog) error {
	return s.insertRequestLog(ctx, s.db, log)
}

type logExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Storage) insertRequestLog(ctx context.Context, db logExecutor, log *storage.RequestLog) error {
	_, err := db.ExecContext(ctx, s.bind(`
		INSERT INTO usage_logs (
			request_id, api_key_id, model, provider, endpoint,
			input_tokens, output_tokens, total_cost,
			status_code, latency_ms, request_time,
			is_streaming, deployment_key,
			pool_name, ttft_ms, req_body_snippet, resp_body_snippet,
			cache_read_tokens, cache_write_tokens, usage_known, session_id, accounting_id, cost_source, cost_known, cost_coverage
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`),
		log.RequestID,
		log.APIKeyID,
		log.Model,
		log.Provider,
		log.Endpoint,
		log.InputTokens,
		log.OutputTokens,
		log.TotalCost,
		log.StatusCode,
		log.LatencyMS,
		log.RequestTime.UTC().Round(0),
		log.IsStreaming,
		log.DeploymentKey,
		log.PoolName,
		log.TTFTMs,
		log.ReqBodySnippet,
		log.RespBodySnippet,
		log.CacheReadTokens,
		log.CacheWriteTokens,
		log.UsageKnown, log.SessionID, log.AccountingID, log.CostSource, log.CostKnown, log.CostCoverage,
	)
	if err != nil {
		return fmt.Errorf("inserting log: %w", err)
	}
	return nil
}
