package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// ResumeAccounting uses one attempt across polling calls; polling does not incur a second model charge.
func ResumeAccounting(ctx context.Context, store storage.Storage, job *storage.ResponsesJob) (context.Context, *accounting.Collector, error) {
	db, ok := store.(storage.AccountingStorage)
	if !ok || job.AccountingID == "" {
		return ctx, nil, nil
	}
	rec, err := db.GetAccounting(ctx, job.AccountingID)
	if err != nil {
		return ctx, nil, err
	}
	if rec == nil || len(rec.Attempts) == 0 {
		return ctx, nil, fmt.Errorf("background accounting record unavailable")
	}
	c := accounting.Restore(rec)
	return c.CurrentContext(ctx), c, nil
}

// FinishAccounting atomically commits terminal job state, usage log, and accounting evidence.
// Concurrent completion/cancellation and repeated polls credit budgets at most once.
func FinishAccounting(ctx context.Context, store storage.Storage, job *storage.ResponsesJob, resp *model.ResponsesResponse, c *accounting.Collector, sa *keystore.SpendAccumulator, r *router.Router) (bool, error) {
	db, ok := store.(storage.AccountingStorage)
	if !ok || c == nil {
		return false, nil
	}
	accounting.ObserveUsage(ctx, resp.Usage)
	var statusErr error
	if resp.Status != "completed" {
		statusErr = fmt.Errorf("response %s", resp.Status)
	}
	accounting.EndAttempt(ctx, statusErr)
	rec := c.Snapshot(true)
	entry := storage.AccountingLog(rec, job.ModelName, "/v1/responses", job.CreatedAt)
	if entry != nil {
		entry.SessionID = job.SessionID
		if statusErr != nil {
			entry.StatusCode = 502
		}
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return true, err
	}
	body := string(b)
	now := time.Now().UTC()
	updated := *job
	updated.Status = resp.Status
	updated.ResponseJSON = &body
	updated.CompletedAt = &now
	if resp.Error != nil {
		b, _ := json.Marshal(resp.Error)
		message := string(b)
		updated.ErrorJSON = &message
	}
	persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	won, err := db.FinalizeResponseAccounting(persistCtx, rec, entry, &updated)
	if err != nil {
		return true, err
	}
	if won {
		if rec.APIKeyID != nil && sa != nil {
			if value := accounting.Float(rec.Cost.Amount); value > 0 {
				sa.Credit(*rec.APIKeyID, value)
			}
		}
		if budget := r.BudgetManager(); budget != nil {
			for _, a := range rec.Attempts {
				if value := accounting.Float(a.Cost.Amount); value > 0 && a.Pool != "" {
					budget.Credit(a.Pool, value)
				}
			}
		}
	}
	return true, nil
}
