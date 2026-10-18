package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// ---------------------------------------------------------------------------
// Phase 13 Plan 02 GREEN — SQLite cache token round-trip test
//
// Verifies that CacheReadTokens and CacheWriteTokens survive a
// LogRequest/GetLogs round-trip through SQLite. The usage_logs table has
// cache_read_tokens and cache_write_tokens columns since migration 15
// (NOT NULL DEFAULT 0), so no DDL changes are needed here.
// ---------------------------------------------------------------------------

// TestLogRequestCacheTokens (INSTR-04): verifies that CacheReadTokens and
// CacheWriteTokens survive a LogRequest/GetLogs round-trip through SQLite.
func TestLogRequestCacheTokens(t *testing.T) {
	// newTestStorage is defined in identity_test.go (same package).
	s := newTestStorage(t)
	ctx := context.Background()

	log := &storage.RequestLog{
		RequestID:        "cache-token-test-001",
		Model:            "claude-3-5-sonnet-20241022",
		Provider:         "anthropic",
		Endpoint:         "/v1/chat/completions",
		InputTokens:      10,
		OutputTokens:     20,
		TotalCost:        0.001,
		StatusCode:       200,
		LatencyMS:        150,
		RequestTime:      time.Now(),
		IsStreaming:      false,
		DeploymentKey:    "anthropic:claude-3-5-sonnet:",
		CacheReadTokens:  100,
		CacheWriteTokens: 25,
	}
	if err := s.LogRequest(ctx, log); err != nil {
		t.Fatalf("LogRequest failed: %v", err)
	}
	logs, _, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) == 0 {
		t.Fatal("no logs returned")
	}
	if logs[0].CacheReadTokens != 100 {
		t.Errorf("CacheReadTokens: got %d, want 100", logs[0].CacheReadTokens)
	}
	if logs[0].CacheWriteTokens != 25 {
		t.Errorf("CacheWriteTokens: got %d, want 25", logs[0].CacheWriteTokens)
	}
}

func TestUsageAvailabilityRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	for _, known := range []bool{false, true} {
		id := "unknown"
		if known {
			id = "known"
		}
		err := s.LogRequest(ctx, &storage.RequestLog{RequestID: id, Model: "test", Provider: "test", Endpoint: "/v1/chat/completions", RequestTime: time.Now(), UsageKnown: known, StatusCode: 200})
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.GetLogByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.UsageKnown != known {
			t.Fatal("usage availability lost")
		}
	}
	logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	if err != nil || total != 2 {
		t.Fatal(total, err)
	}
	states := map[string]bool{}
	for _, row := range logs {
		states[row.RequestID] = row.UsageKnown
	}
	if states["unknown"] || !states["known"] {
		t.Fatal(states)
	}
}
