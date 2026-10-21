package postgres

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestAccountingConcurrentFinalizationAndRetention(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	_, _, key := fixture(t, ctx, s)
	collector := accounting.New("pg-accounting-request", &key.ID)
	attemptCtx := collector.Begin(ctx, "openai", "model", "model", "deployment", "pool", "", nil)
	accounting.ObserveHeaders(attemptCtx, http.Header{"X-Litellm-Response-Cost": []string{"0.000415"}, "X-Litellm-Total-Tokens": []string{"1874"}})
	accounting.EndAttempt(attemptCtx, nil)
	pending := collector.Snapshot(false)
	check(t, s.PutAccounting(ctx, pending))
	final := collector.Snapshot(true)
	entry := storage.AccountingLog(final, "model", "/v1/chat/completions", time.Now())
	var wins atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, err := s.FinalizeAccounting(ctx, final, entry)
			if won {
				wins.Add(1)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		check(t, err)
	}
	if wins.Load() != 1 {
		t.Fatalf("finalization winners: %d", wins.Load())
	}
	rec, err := s.GetAccounting(ctx, final.ID)
	check(t, err)
	if rec.State != "final" || rec.APIKeyID == nil || *rec.APIKeyID != key.ID || *rec.Cost.Amount != "0.000415" {
		t.Fatalf("record=%+v", rec)
	}
	logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	check(t, err)
	if total != 1 || logs[0].AccountingID != final.ID || logs[0].CostKnown == nil || !*logs[0].CostKnown || logs[0].CostSource != "upstream" {
		t.Fatalf("logs=%+v total=%d", logs, total)
	}
	detail, err := s.GetLogByID(ctx, final.RequestID)
	check(t, err)
	if detail.AccountingID != final.ID {
		t.Fatalf("detail=%+v", detail)
	}
	old := accounting.New("old", nil).Snapshot(false)
	check(t, s.PutAccounting(ctx, old))
	_, err = s.DB().ExecContext(ctx, `UPDATE request_accounting SET created_at=$1 WHERE id=$2`, time.Now().Add(-48*time.Hour), old.ID)
	check(t, err)
	_, err = s.DeleteOldRequestLogs(ctx, time.Now().Add(-24*time.Hour))
	check(t, err)
	removed, err := s.GetAccounting(ctx, old.ID)
	check(t, err)
	if removed != nil {
		t.Fatal("old accounting survived retention")
	}
}
func TestBackgroundAccountingFinalizesJobAtomically(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	collector := accounting.New("pg-background", nil)
	collector.Begin(ctx, "openai", "model", "model", "deployment", "", "", nil)
	pending := collector.Snapshot(false)
	check(t, s.PutAccounting(ctx, pending))
	job := &storage.ResponsesJob{ID: "pg-job", ModelName: "model", DeploymentKey: "deployment", Status: "queued", RequestJSON: `{}`, AccountingID: pending.ID}
	check(t, s.CreateResponsesJob(ctx, job))
	stored, err := s.GetResponsesJob(ctx, job.ID)
	check(t, err)
	if stored.AccountingID != pending.ID {
		t.Fatalf("job=%+v", stored)
	}
	final := collector.Snapshot(true)
	body := `{"id":"pg-job","status":"completed"}`
	now := time.Now()
	job.ResponseJSON = &body
	job.CompletedAt = &now
	job.Status = "completed"
	won, err := s.FinalizeResponseAccounting(ctx, final, storage.AccountingLog(final, "model", "/v1/responses", now), job)
	check(t, err)
	if !won {
		t.Fatal("initial finalization did not win")
	}
	stored, err = s.GetResponsesJob(ctx, job.ID)
	check(t, err)
	if stored.Status != "completed" || stored.CompletedAt == nil {
		t.Fatalf("job=%+v", stored)
	}
	won, err = s.FinalizeResponseAccounting(ctx, final, nil, job)
	check(t, err)
	if won {
		t.Fatal("duplicate finalization won")
	}
}
