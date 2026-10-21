package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestAccountingFinalizeIsAtomicAndIdempotent(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	c := accounting.New("req", nil)
	c.Begin(ctx, "openai", "m", "m", "d", "", "", nil)
	if err := s.PutAccounting(ctx, c.Snapshot(false)); err != nil {
		t.Fatal(err)
	}
	rec := c.Snapshot(true)
	entry := storage.AccountingLog(rec, "m", "/v1/chat/completions", time.Now())
	// Force the log insertion to fail: the accounting update must roll back too.
	entry.Model = ""
	entry.RequestTime = time.Now()
	entry.Provider = "p"
	entry.Endpoint = "e"
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER fail_accounting_log BEFORE INSERT ON usage_logs BEGIN SELECT RAISE(ABORT,'test rollback'); END`); err != nil {
		t.Fatal(err)
	}
	if won, err := s.FinalizeAccounting(ctx, rec, entry); err == nil || won {
		t.Fatalf("won=%v err=%v", won, err)
	}
	got, err := s.GetAccounting(ctx, rec.ID)
	if err != nil || got.State != "pending" {
		t.Fatalf("record=%+v err=%v", got, err)
	}
	s.db.ExecContext(ctx, `DROP TRIGGER fail_accounting_log`)
	for i := 0; i < 2; i++ {
		won, err := s.FinalizeAccounting(ctx, rec, entry)
		if err != nil || won != (i == 0) {
			t.Fatalf("attempt=%d won=%v err=%v", i, won, err)
		}
	}
	_, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	if err != nil || total != 1 {
		t.Fatalf("total=%d err=%v", total, err)
	}
}
