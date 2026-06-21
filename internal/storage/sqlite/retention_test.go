package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// insertUsageLogNullKey inserts a usage_log row with a NULL api_key_id for retention tests.
// This avoids FK constraint failures while still exercising the request_time-based deletion.
func insertUsageLogNullKey(t *testing.T, s *Storage, model string, cost float64, requestTime time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_logs (request_id, api_key_id, model, provider, endpoint, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_cost, status_code, latency_ms, request_time)
		VALUES (?, NULL, ?, 'openai', '/v1/chat/completions', 10, 10, 0, 0, ?, 200, 100, ?)
	`, "req-"+model+"-"+requestTime.String(), model, cost, requestTime)
	if err != nil {
		t.Fatalf("insert usage_log: %v", err)
	}
}

func TestDeleteOldRequestLogs(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	now := time.Now().UTC()
	old := now.AddDate(0, 0, -31)    // 31 days ago — beyond 30-day default retention
	recent := now.AddDate(0, 0, -1)  // 1 day ago — within retention window

	// Insert one old row and one recent row
	insertUsageLogNullKey(t, s, "gpt-4-old", 0.01, old)
	insertUsageLogNullKey(t, s, "gpt-4-recent", 0.01, recent)

	// Delete rows older than 30 days ago
	cutoff := now.AddDate(0, 0, -30)
	n, err := s.DeleteOldRequestLogs(ctx, cutoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row deleted, got %d", n)
	}

	// Verify the recent row still exists
	logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	if err != nil {
		t.Fatalf("GetLogs error: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 row remaining, got %d", total)
	}
	if logs[0].Model != "gpt-4-recent" {
		t.Fatalf("expected recent row to remain, got model=%q", logs[0].Model)
	}
}

func TestDeleteOldRequestLogsNoneOld(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	now := time.Now().UTC()
	recent := now.AddDate(0, 0, -1) // 1 day ago

	// Insert a recent row
	insertUsageLogNullKey(t, s, "gpt-4", 0.01, recent)

	// Cutoff far in the past — should delete nothing
	cutoff := now.AddDate(0, 0, -90)
	n, err := s.DeleteOldRequestLogs(ctx, cutoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rows deleted, got %d", n)
	}

	// Verify row still exists
	_, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	if err != nil {
		t.Fatalf("GetLogs error: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected row to remain, got total=%d", total)
	}
}

func TestDeleteOldRequestLogsLargeBatch(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	now := time.Now().UTC()
	old := now.AddDate(0, 0, -31) // 31 days ago

	// Insert 2,500 old rows — must be deleted across 3 batches (1000+1000+500)
	for i := 0; i < 2500; i++ {
		// Vary the request_id to avoid unique constraint violations
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO usage_logs (request_id, api_key_id, model, provider, endpoint, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_cost, status_code, latency_ms, request_time)
			VALUES (?, NULL, 'gpt-4', 'openai', '/v1/chat/completions', 10, 10, 0, 0, 0.01, 200, 100, ?)
		`, fmt.Sprintf("req-large-%d", i), old)
		if err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	// Delete all old rows
	cutoff := now.AddDate(0, 0, -30)
	n, err := s.DeleteOldRequestLogs(ctx, cutoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2500 {
		t.Fatalf("expected 2500 rows deleted, got %d", n)
	}

	// Verify table is empty
	_, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	if err != nil {
		t.Fatalf("GetLogs error: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 rows remaining, got %d", total)
	}
}

func TestDeleteOldRequestLogsKeepsRecent(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	now := time.Now().UTC()
	cutoff := now.AddDate(0, 0, -30)

	// Insert a row at exactly the cutoff instant — should NOT be deleted (cutoff is strictly <)
	insertUsageLogNullKey(t, s, "at-cutoff", 0.01, cutoff)

	n, err := s.DeleteOldRequestLogs(ctx, cutoff)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rows deleted (row at cutoff should be kept), got %d", n)
	}

	// Verify the at-cutoff row still exists
	_, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	if err != nil {
		t.Fatalf("GetLogs error: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 row remaining, got %d", total)
	}
}
