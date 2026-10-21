package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestSessionLogsPaginationAndCombinedFilters(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	now := time.Now().UTC()
	session := "story:/?a=1&b=%'"
	for i := 0; i < 53; i++ {
		id := session
		if i == 52 {
			id = "unrelated"
		}
		provider := "openai"
		if i == 51 {
			provider = "anthropic"
		}
		if err := s.LogRequest(ctx, &storage.RequestLog{RequestID: fmt.Sprintf("req-%d", i), SessionID: id, Model: "m", Provider: provider, Endpoint: "/v1/chat/completions", RequestTime: now.Add(-time.Duration(i) * time.Hour), IsStreaming: i%2 == 0}); err != nil {
			t.Fatal(err)
		}
	}
	page, total, err := s.GetLogs(ctx, 50, 0, storage.LogsFilter{SessionID: session})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 50 || total != 52 {
		t.Fatalf("first page: len=%d total=%d", len(page), total)
	}
	if page[0].RequestID != "req-0" || page[0].SessionID != session {
		t.Fatalf("wrong first log: %+v", page[0])
	}
	page, total, err = s.GetLogs(ctx, 50, 50, storage.LogsFilter{SessionID: session})
	if err != nil || len(page) != 2 || total != 52 {
		t.Fatalf("second page: len=%d total=%d err=%v", len(page), total, err)
	}
	detail, err := s.GetLogByID(ctx, "req-51")
	if err != nil || detail.SessionID != session || detail.ReqBodySnippet != nil {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	from := now.Add(-24 * time.Hour)
	page, total, err = s.GetLogs(ctx, 50, 0, storage.LogsFilter{SessionID: session, Provider: "openai", Model: "m", DateFrom: &from})
	if err != nil || len(page) != 25 || total != 25 {
		t.Fatalf("combined filters: len=%d total=%d err=%v", len(page), total, err)
	}
	page, total, err = s.GetLogs(ctx, 50, 0, storage.LogsFilter{SessionID: "story:"})
	if err != nil || len(page) != 0 || total != 0 {
		t.Fatalf("session filter must be exact: len=%d total=%d err=%v", len(page), total, err)
	}
}

func TestSessionMigrationPreservesExistingLogs(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	// Recreate the schema immediately before session attribution was introduced.
	for _, q := range []string{
		`DROP TABLE request_accounting`,
		`ALTER TABLE usage_logs DROP COLUMN accounting_id`,
		`ALTER TABLE usage_logs DROP COLUMN cost_source`,
		`ALTER TABLE usage_logs DROP COLUMN cost_known`,
		`ALTER TABLE usage_logs DROP COLUMN cost_coverage`,
		`ALTER TABLE responses_jobs DROP COLUMN accounting_id`,
		`DROP INDEX idx_usage_logs_session_time`,
		`ALTER TABLE usage_logs DROP COLUMN session_id`,
		`ALTER TABLE responses_jobs DROP COLUMN session_id`,
		`DELETE FROM schema_migrations WHERE version BETWEEN 48 AND 58`,
		`INSERT INTO usage_logs (request_id,model,provider,endpoint,status_code,latency_ms,request_time) VALUES ('old','m','p','/v1/chat/completions',200,10,datetime('now'))`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	old, err := s.GetLogByID(ctx, "old")
	if err != nil || old == nil || old.SessionID != "" {
		t.Fatalf("old log=%+v err=%v", old, err)
	}
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
}
