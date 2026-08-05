package responses

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/provider/openai"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"
)

func init() {
	provider.Register("workertest", openai.New)
}

func TestWorker_PollJob_AdvancesToTerminal(t *testing.T) {
	statuses := []string{"in_progress", "completed"}
	call := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := statuses[call]
		if call < len(statuses)-1 {
			call++
		}
		resp := map[string]any{"id": "resp_1", "status": status}
		if status == "completed" {
			resp["usage"] = map[string]any{"prompt_tokens": 3, "completion_tokens": 4, "total_tokens": 7}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	r, store, cm, sa := newWorkerTestFixture(t, ts.URL)
	ctx := context.Background()

	dk := mustDeploymentKey(t, r, "test-model")
	job := &storage.ResponsesJob{
		ID: "resp_1", DeploymentKey: dk, ModelName: "test-model",
		Status: "queued", RequestJSON: `{}`,
	}
	if err := store.CreateResponsesJob(ctx, job); err != nil {
		t.Fatalf("CreateResponsesJob: %v", err)
	}

	w := NewWorker(r, store, cm, sa)

	got, _ := store.GetResponsesJob(ctx, "resp_1")
	w.pollJob(ctx, got)
	got, _ = store.GetResponsesJob(ctx, "resp_1")
	if got.Status != "in_progress" {
		t.Fatalf("expected in_progress after first poll, got %s", got.Status)
	}
	if got.CompletedAt != nil {
		t.Fatalf("expected completed_at nil while in_progress")
	}

	w.pollJob(ctx, got)
	got, _ = store.GetResponsesJob(ctx, "resp_1")
	if got.Status != "completed" {
		t.Fatalf("expected completed after second poll, got %s", got.Status)
	}
	if got.CompletedAt == nil {
		t.Fatalf("expected completed_at to be set")
	}
}

func TestWorker_PollJob_UnknownDeployment(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called when deployment cannot be resolved")
	}))
	defer ts.Close()

	r, store, cm, sa := newWorkerTestFixture(t, ts.URL)
	ctx := context.Background()

	job := &storage.ResponsesJob{
		ID: "resp_2", DeploymentKey: "workertest:missing-model:", ModelName: "test-model",
		Status: "queued", RequestJSON: `{}`,
	}
	if err := store.CreateResponsesJob(ctx, job); err != nil {
		t.Fatalf("CreateResponsesJob: %v", err)
	}

	w := NewWorker(r, store, cm, sa)
	w.pollJob(ctx, job)

	got, _ := store.GetResponsesJob(ctx, "resp_2")
	if got.Status != "queued" {
		t.Errorf("expected job left unchanged on deployment-not-found, got %s", got.Status)
	}
}

func TestWorker_Run_ResumesPendingAndStopsOnCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "resp_3", "status": "completed", "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
	}))
	defer ts.Close()

	r, store, cm, sa := newWorkerTestFixture(t, ts.URL)
	ctx := context.Background()

	dk := mustDeploymentKey(t, r, "test-model")
	job := &storage.ResponsesJob{
		ID: "resp_3", DeploymentKey: dk, ModelName: "test-model",
		Status: "queued", RequestJSON: `{}`,
	}
	if err := store.CreateResponsesJob(ctx, job); err != nil {
		t.Fatalf("CreateResponsesJob: %v", err)
	}

	w := NewWorker(r, store, cm, sa)
	w.PollInterval = time.Millisecond

	runCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w.Run(runCtx)

	got, _ := store.GetResponsesJob(context.Background(), "resp_3")
	if got.Status != "completed" {
		t.Errorf("expected job resumed and completed, got %s", got.Status)
	}
}

// flakyUpdateStore wraps a real storage.Storage and fails the Nth call to
// UpdateResponsesJob, to exercise the worker's handling of a failed terminal write.
type flakyUpdateStore struct {
	storage.Storage
	failOnCall int
	calls      int
}

func (s *flakyUpdateStore) UpdateResponsesJob(ctx context.Context, id, status string, responseJSON, errorJSON *string, completedAt *time.Time) error {
	s.calls++
	if s.calls == s.failOnCall {
		return context.DeadlineExceeded
	}
	return s.Storage.UpdateResponsesJob(ctx, id, status, responseJSON, errorJSON, completedAt)
}

func TestWorker_PollJob_TerminalUpdateFailureDoesNotDoubleCredit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_flaky", "status": "completed",
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20},
		})
	}))
	defer ts.Close()

	r, realStore, cm, sa := newWorkerTestFixture(t, ts.URL)
	ctx := context.Background()
	cm.SetCustomSpec("test-model", costmap.ModelSpec{InputCostPerToken: 0.01, OutputCostPerToken: 0.02})

	dk := mustDeploymentKey(t, r, "test-model")
	apiKeyID := int64(42)
	job := &storage.ResponsesJob{
		ID: "resp_flaky", APIKeyID: &apiKeyID, DeploymentKey: dk, ModelName: "test-model",
		Status: "queued", RequestJSON: `{}`,
	}
	if err := realStore.CreateResponsesJob(ctx, job); err != nil {
		t.Fatalf("CreateResponsesJob: %v", err)
	}

	flaky := &flakyUpdateStore{Storage: realStore, failOnCall: 1}
	w := NewWorker(r, flaky, cm, sa)
	w.pollJob(ctx, job)

	// The failed UpdateResponsesJob must short-circuit before logCompletion —
	// the job stays non-terminal in storage and nothing gets credited yet.
	got, _ := realStore.GetResponsesJob(ctx, "resp_flaky")
	if got.Status != "queued" {
		t.Fatalf("expected job to remain queued after a failed terminal update, got %s", got.Status)
	}
	if spend := sa.CurrentSpend(apiKeyID); spend != 0 {
		t.Fatalf("expected no spend credited when the terminal update failed, got %v", spend)
	}

	// The next poll succeeds (flaky store only fails once) and must credit exactly once.
	w.pollJob(ctx, got)
	got, _ = realStore.GetResponsesJob(ctx, "resp_flaky")
	if got.Status != "completed" {
		t.Fatalf("expected completed after the retried poll, got %s", got.Status)
	}
	if spend := sa.CurrentSpend(apiKeyID); spend <= 0 {
		t.Fatalf("expected spend to be credited exactly once after the successful retry, got %v", spend)
	}
}

func TestWorker_PollJob_RateLimitBacksOffDeployment(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	r, store, cm, sa := newWorkerTestFixture(t, ts.URL)
	ctx := context.Background()

	dk := mustDeploymentKey(t, r, "test-model")
	job := &storage.ResponsesJob{
		ID: "resp_rl", DeploymentKey: dk, ModelName: "test-model",
		Status: "queued", RequestJSON: `{}`,
	}
	if err := store.CreateResponsesJob(ctx, job); err != nil {
		t.Fatalf("CreateResponsesJob: %v", err)
	}

	w := NewWorker(r, store, cm, sa)
	w.pollJob(ctx, job)

	// A 429 from GetResponse must apply the same backoff the synchronous path
	// applies on rate limit, so the deployment is skipped until it expires.
	if _, err := r.GetDeploymentWithRetry("test-model", map[*provider.Deployment]bool{}); err == nil {
		t.Fatal("expected the deployment to be in backoff after a rate-limited poll, but GetDeploymentWithRetry succeeded")
	}

	// The job itself must be left untouched (still queued) — a poll error is not
	// a job outcome.
	got, _ := store.GetResponsesJob(ctx, "resp_rl")
	if got.Status != "queued" {
		t.Errorf("expected job left unchanged on rate-limited poll, got %s", got.Status)
	}
}

func TestWorker_LogCompletion_CreditsPoolBudget(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_pool", "status": "completed",
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 100, "total_tokens": 200},
		})
	}))
	defer ts.Close()

	r, store, cm, sa := newWorkerTestFixture(t, ts.URL)
	ctx := context.Background()
	cm.SetCustomSpec("test-model", costmap.ModelSpec{InputCostPerToken: 0.01, OutputCostPerToken: 0.02})

	dk := mustDeploymentKey(t, r, "test-model")
	job := &storage.ResponsesJob{
		ID: "resp_pool", DeploymentKey: dk, ModelName: "test-model", PoolName: "my-pool",
		Status: "queued", RequestJSON: `{}`,
	}
	if err := store.CreateResponsesJob(ctx, job); err != nil {
		t.Fatalf("CreateResponsesJob: %v", err)
	}
	if got, _ := store.GetResponsesJob(ctx, "resp_pool"); got.PoolName != "my-pool" {
		t.Fatalf("expected PoolName to round-trip through storage, got %q", got.PoolName)
	}

	w := NewWorker(r, store, cm, sa)
	w.pollJob(ctx, job)

	// The synchronous request path credits both the per-key spend accumulator and
	// the pool's daily budget cap (chat.go's logRequest); a background job's
	// completion must do the same or its pool would never see its cost applied.
	if spend := r.BudgetManager().CurrentSpend("my-pool"); spend <= 0 {
		t.Errorf("expected pool budget to be credited for a completed background job, got spend=%v", spend)
	}
}

func mustDeploymentKey(t *testing.T, r *router.Router, modelName string) string {
	t.Helper()
	d, err := r.GetDeployment(modelName)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	return d.DeploymentKey()
}

func newWorkerTestFixture(t *testing.T, apiBase string) (*router.Router, *sqlite.Storage, *costmap.Manager, *keystore.SpendAccumulator) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := &config.Config{
		ModelList: []config.ModelConfig{
			{
				ModelName: "test-model",
				LiteLLMParams: config.LiteLLMParams{
					Model:   "workertest/gpt-5",
					APIBase: apiBase,
				},
			},
		},
		RouterSettings: config.RouterSettings{RoutingStrategy: "simple-shuffle", CooldownTime: 30 * time.Second, AllowedFails: 3},
	}
	r, err := router.New(cfg, nil)
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return r, store, costmap.New(), keystore.NewSpendAccumulator()
}
