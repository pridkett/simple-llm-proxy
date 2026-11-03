package sqlstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// GetUsage aggregates one filtered population for charts, summaries and drill-down.
// Calendar intervals are constructed in Go, so SQLite and PostgreSQL share exact
// DST/offset semantics. Only aggregate measurements, never bodies, leave SQL.
func (s *Storage) GetUsage(ctx context.Context, q storage.UsageQuery) ([]storage.UsageAggregate, error) {
	if len(q.Buckets) == 0 || len(q.Buckets) > 744 {
		return nil, fmt.Errorf("usage requires 1–744 buckets")
	}
	groupID, groupName := "ul.model", "ul.model"
	switch q.GroupBy {
	case "model":
	case "team":
		groupID, groupName = "CAST(t.id AS TEXT)", "t.name"
	case "app":
		groupID, groupName = "CAST(a.id AS TEXT)", "a.name"
	case "key":
		groupID, groupName = "CAST(k.id AS TEXT)", "k.name"
	default:
		return nil, fmt.Errorf("invalid usage grouping")
	}
	args := make([]any, 0, len(q.Buckets)*3+10)
	values := make([]string, len(q.Buckets))
	for i, b := range q.Buckets {
		if !b.Start.Before(b.End) {
			return nil, fmt.Errorf("invalid usage bucket")
		}
		if s.dialect == PostgreSQL {
			values[i] = "(CAST(? AS INTEGER), CAST(? AS TIMESTAMPTZ), CAST(? AS TIMESTAMPTZ))"
		} else {
			values[i] = "(?, ?, ?)"
		}
		args = append(args, i, b.Start.UTC(), b.End.UTC())
	}
	// SQL fragments below are entirely allowlisted; caller-controlled values are bound.
	jsonInt := func(path string) string {
		if s.dialect == PostgreSQL {
			return "CAST(ra.record_json::jsonb #>> '{" + strings.ReplaceAll(path, ".", ",") + "}' AS BIGINT)"
		}
		return "CAST(json_extract(ra.record_json, '$." + path + "') AS BIGINT)"
	}
	legacyInput := "ul.input_tokens + CASE WHEN ul.provider = 'anthropic' THEN ul.cache_read_tokens + ul.cache_write_tokens ELSE 0 END"
	measurement := func(path, legacy string) string {
		return "CASE WHEN COALESCE(ul.accounting_id, '') != '' THEN " + jsonInt(path) + " WHEN ul.usage_known = TRUE THEN " + legacy + " END"
	}
	input := measurement("usage.input_tokens", legacyInput)
	output := measurement("usage.output_tokens", "ul.output_tokens")
	total := measurement("usage.total_tokens", legacyInput+" + ul.output_tokens")
	read := measurement("usage.input_details.cache_read_tokens", "CASE WHEN ul.provider = 'anthropic' THEN ul.cache_read_tokens END")
	write := measurement("usage.input_details.cache_write_tokens", "CASE WHEN ul.provider = 'anthropic' THEN ul.cache_write_tokens END")
	reasoning := measurement("usage.output_details.reasoning_tokens", "NULL")
	cost := "CASE WHEN COALESCE(ul.cost_known, ul.usage_known) = TRUE THEN ul.total_cost END"
	where := []string{"ul.model != '_flush'", "ul.request_time >= ?", "ul.request_time < ?"}
	args = append(args, q.From.UTC(), q.To.UTC())
	for _, f := range []struct {
		col   string
		value *int64
	}{{"t.id", q.Filters.TeamID}, {"a.id", q.Filters.AppID}, {"ul.api_key_id", q.Filters.KeyID}} {
		if f.value != nil {
			where = append(where, f.col+" = ?")
			args = append(args, *f.value)
		}
	}
	if q.Filters.Model != "" {
		where = append(where, "ul.model = ?")
		args = append(args, q.Filters.Model)
	}
	switch q.Unattributed {
	case "":
	case "team":
		where = append(where, "t.id IS NULL")
	case "app":
		where = append(where, "a.id IS NULL")
	case "key":
		where = append(where, "k.id IS NULL")
	default:
		return nil, fmt.Errorf("invalid attribution filter")
	}
	query := `WITH buckets(bucket, start_time, end_time) AS (VALUES ` + strings.Join(values, ",") + `), measured AS (
 SELECT b.bucket, COALESCE(` + groupID + `, '') AS group_id, COALESCE(` + groupName + `, 'Unattributed') AS name,
 ul.status_code, ul.cost_source, ul.cost_coverage, ul.accounting_id,
 ` + input + ` AS input_count, ` + output + ` AS output_count, ` + total + ` AS token_count,
 ` + read + ` AS read_count, ` + write + ` AS write_count, ` + reasoning + ` AS reasoning_count, ` + cost + ` AS cost
 FROM usage_logs ul
 JOIN buckets b ON ul.request_time >= b.start_time AND ul.request_time < b.end_time
 LEFT JOIN api_keys k ON k.id = ul.api_key_id
 LEFT JOIN applications a ON a.id = k.application_id
 LEFT JOIN teams t ON t.id = a.team_id
 LEFT JOIN request_accounting ra ON ra.id = ul.accounting_id
 WHERE ` + strings.Join(where, " AND ") + `)
 SELECT bucket, group_id, name, COUNT(*),
 SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END),
 COALESCE(SUM(input_count),0), COALESCE(SUM(output_count),0), COALESCE(SUM(token_count),0),
 COALESCE(SUM(read_count),0), COALESCE(SUM(write_count),0), COALESCE(SUM(reasoning_count),0),
 COUNT(input_count), COUNT(output_count), COUNT(token_count), COUNT(read_count), COUNT(write_count), COUNT(reasoning_count),
 COUNT(cost), SUM(CASE WHEN cost IS NOT NULL AND cost_coverage = 'partial' THEN 1 ELSE 0 END),
 COALESCE(SUM(cost),0),
 SUM(CASE WHEN cost IS NOT NULL AND cost_source = 'upstream' THEN 1 ELSE 0 END),
 SUM(CASE WHEN cost IS NOT NULL AND cost_source IN ('estimate', 'estimated') THEN 1 ELSE 0 END),
 SUM(CASE WHEN cost IS NOT NULL AND cost_source = 'mixed' THEN 1 ELSE 0 END),
 SUM(CASE WHEN accounting_id = '' THEN 1 ELSE 0 END)
 FROM measured GROUP BY bucket, group_id, name ORDER BY bucket, group_id`
	rows, err := s.db.QueryContext(ctx, s.bind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("query usage: %w", err)
	}
	defer rows.Close()
	result := make([]storage.UsageAggregate, 0)
	for rows.Next() {
		var r storage.UsageAggregate
		if err := rows.Scan(&r.Bucket, &r.GroupID, &r.Name, &r.Calls, &r.FailedCalls, &r.InputTokens, &r.OutputTokens, &r.TotalTokens, &r.CacheReadTokens, &r.CacheWriteTokens, &r.ReasoningTokens, &r.KnownInputCalls, &r.KnownOutputCalls, &r.KnownTokenCalls, &r.KnownCacheReadCalls, &r.KnownCacheWriteCalls, &r.KnownReasoningCalls, &r.KnownCostCalls, &r.PartialCostCalls, &r.TotalSpend, &r.UpstreamCostCalls, &r.EstimatedCostCalls, &r.MixedCostCalls, &r.LegacyCalls); err != nil {
			return nil, fmt.Errorf("scan usage: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
