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
