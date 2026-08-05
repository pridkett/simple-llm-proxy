// Package responses implements the background polling worker for OpenAI
// Responses API jobs created with background: true (ADR 010 D-04).
// OpenAI does not push completion webhooks for these jobs — the only way to
// learn a job finished is to poll GET /responses/{id}, so this package owns
// that polling loop, persisting progress to storage.ResponsesJob rows and
// crediting cost/usage exactly as a synchronous request would.
package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// DefaultPollInterval is how often the worker checks pending jobs when not overridden.
const DefaultPollInterval = 2 * time.Second

// Worker polls pending background Responses API jobs to advance them to a
// terminal state, updating storage.ResponsesJob rows and crediting cost/usage.
type Worker struct {
	Router       *router.Router
	Store        storage.Storage
	CostMap      *costmap.Manager
	SpendAcc     *keystore.SpendAccumulator
	PollInterval time.Duration
}

// NewWorker constructs a Worker with DefaultPollInterval.
func NewWorker(r *router.Router, store storage.Storage, cm *costmap.Manager, sa *keystore.SpendAccumulator) *Worker {
	return &Worker{
		Router:       r,
		Store:        store,
		CostMap:      cm,
		SpendAcc:     sa,
		PollInterval: DefaultPollInterval,
	}
}

// Run blocks, polling pending jobs on PollInterval until ctx is cancelled.
// Callers should launch this in its own goroutine and cancel ctx on shutdown.
func (w *Worker) Run(ctx context.Context) {
	interval := w.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}

	// D-04: resume jobs left in flight when the process last stopped.
	w.pollOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.pollOnce(ctx)
		}
	}
}

// pollOnce advances every currently pending job by one poll.
func (w *Worker) pollOnce(ctx context.Context) {
	jobs, err := w.Store.ListPendingResponsesJobs(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("responses worker: failed to list pending jobs")
		return
	}
	for _, job := range jobs {
		w.pollJob(ctx, job)
	}
}

// pollJob polls a single job's upstream status and updates storage accordingly.
func (w *Worker) pollJob(ctx context.Context, job *storage.ResponsesJob) {
	d, err := w.Router.GetDeploymentByKey(job.ModelName, job.DeploymentKey)
	if err != nil {
		// The deployment may have been removed from config since the job was created.
		// Leave the job pending; a config reload could restore it.
		log.Warn().Err(err).Str("job_id", job.ID).Msg("responses worker: deployment not found")
		return
	}
	rp, ok := d.Provider.(provider.ResponsesProvider)
	if !ok {
		log.Warn().Str("job_id", job.ID).Str("model", job.ModelName).Msg("responses worker: deployment no longer supports Responses API")
		return
	}

	resp, err := rp.GetResponse(ctx, job.ID)
	if err != nil {
		var rlErr *provider.RateLimitError
		if errors.As(err, &rlErr) {
			// Same signal the synchronous path acts on (route.go's routePool/routeLegacy):
			// back the deployment off so the next tick's poll skips it instead of
			// hammering an upstream that just told us to slow down.
			w.Router.ReportRateLimit(d, rlErr.RetryAfter)
		}
		log.Warn().Err(err).Str("job_id", job.ID).Msg("responses worker: poll failed")
		return
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		log.Warn().Err(err).Str("job_id", job.ID).Msg("responses worker: failed to marshal response")
		return
	}
	respJSONStr := string(respJSON)

	var errorJSON *string
	if resp.Error != nil {
		if b, err := json.Marshal(resp.Error); err == nil {
			s := string(b)
			errorJSON = &s
		}
	}

	if !resp.IsTerminal() {
		if updErr := w.Store.UpdateResponsesJob(ctx, job.ID, resp.Status, &respJSONStr, errorJSON, nil); updErr != nil {
			log.Warn().Err(updErr).Str("job_id", job.ID).Msg("responses worker: failed to update in-progress job")
		}
		return
	}

	now := time.Now().UTC()
	if updErr := w.Store.UpdateResponsesJob(ctx, job.ID, resp.Status, &respJSONStr, errorJSON, &now); updErr != nil {
		// The job's row is still non-terminal in storage, so the next poll will see
		// this same upstream terminal status again. Returning here (instead of
		// falling through to logCompletion) is what prevents that retry from
		// double-crediting the spend accumulator for one job.
		log.Warn().Err(updErr).Str("job_id", job.ID).Msg("responses worker: failed to update terminal job; will retry on next poll")
		return
	}

	if resp.Status == "completed" {
		w.Router.ReportSuccess(d)
	} else {
		w.Router.ReportFailure(d)
	}

	w.logCompletion(job, d, resp)
}

// logCompletion writes a usage_logs entry and credits the spend accumulator for
// a job that just reached a terminal state, mirroring the synchronous request path.
func (w *Worker) logCompletion(job *storage.ResponsesJob, d *provider.Deployment, resp *model.ResponsesResponse) {
	if w.Store == nil || resp.Usage == nil {
		return
	}

	var totalCost float64
	if w.CostMap != nil {
		spec := w.CostMap.GetEffectiveSpec(job.ModelName, []string{d.ActualModel})
		totalCost = float64(resp.Usage.PromptTokens)*spec.Spec.InputCostPerToken +
			float64(resp.Usage.CompletionTokens)*spec.Spec.OutputCostPerToken +
			float64(resp.Usage.CacheReadTokens)*spec.Spec.CacheReadInputTokenCost +
			float64(resp.Usage.CacheWriteTokens)*spec.Spec.CacheCreationInputTokenCost
	}

	status := 200
	if resp.Status != "completed" {
		status = 502
	}

	logCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logEntry := &storage.RequestLog{
		RequestID:        fmt.Sprintf("responses-%s", job.ID),
		APIKeyID:         job.APIKeyID,
		Model:            job.ModelName,
		Provider:         d.ProviderName,
		Endpoint:         "/v1/responses",
		InputTokens:      resp.Usage.PromptTokens,
		OutputTokens:     resp.Usage.CompletionTokens,
		TotalCost:        totalCost,
		StatusCode:       status,
		LatencyMS:        time.Since(job.CreatedAt).Milliseconds(),
		RequestTime:      job.CreatedAt,
		IsStreaming:      false,
		DeploymentKey:    d.DeploymentKey(),
		PoolName:         job.PoolName,
		CacheReadTokens:  resp.Usage.CacheReadTokens,
		CacheWriteTokens: resp.Usage.CacheWriteTokens,
	}

	if err := w.Store.LogRequest(logCtx, logEntry); err != nil {
		log.Warn().Err(err).Str("job_id", job.ID).Msg("responses worker: failed to log completed job")
	}

	if job.APIKeyID != nil && w.SpendAcc != nil && totalCost > 0 {
		w.SpendAcc.Credit(*job.APIKeyID, totalCost)
	}

	// Mirror chat.go's logRequest: a background job's cost counts against its
	// pool's daily budget cap exactly like a synchronous request's would.
	if job.PoolName != "" && totalCost > 0 {
		w.Router.BudgetManager().Credit(job.PoolName, totalCost)
	}
}
