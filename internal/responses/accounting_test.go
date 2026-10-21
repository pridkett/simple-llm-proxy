package responses

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestBackgroundAccountingSurvivesResumeAndCreditsOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-litellm-response-cost", "0.25")
		fmt.Fprint(w, `{"id":"r","status":"completed","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`)
	}))
	defer server.Close()
	route, store, cm, sa := newWorkerTestFixture(t, server.URL)
	ctx := context.Background()
	team, err := store.CreateTeam(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	app, err := store.CreateApplication(ctx, team.ID, "app")
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.CreateAPIKey(ctx, app.ID, "key", "prefix", "hash", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := accounting.New("original-request-id", &key.ID)
	dk := mustDeploymentKey(t, route, "test-model")
	c.Begin(ctx, "workertest", "test-model", "gpt-5", dk, "", "", cm)
	rec := c.Snapshot(false)
	if err := store.PutAccounting(ctx, rec); err != nil {
		t.Fatal(err)
	}
	job := &storage.ResponsesJob{ID: "r", ModelName: "test-model", DeploymentKey: dk, APIKeyID: &key.ID, AccountingID: rec.ID, Status: "queued", RequestJSON: `{}`}
	if err := store.CreateResponsesJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(route, store, cm, sa)
	for i := 0; i < 2; i++ {
		worker.pollJob(ctx, job)
	}
	saved, err := store.GetAccounting(ctx, rec.ID)
	if err != nil || saved.State != "final" || saved.Cost.Amount == nil || *saved.Cost.Amount != "0.25" || saved.RequestID != "original-request-id" {
		t.Fatalf("record=%+v err=%v", saved, err)
	}
	if value := sa.CurrentSpend(key.ID); value != 0.25 {
		t.Fatalf("charged %v", value)
	}
	savedJob, err := store.GetResponsesJob(ctx, "r")
	if err != nil || savedJob.Status != "completed" {
		t.Fatalf("job=%+v err=%v", savedJob, err)
	}
	_, total, err := store.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	if err != nil || total != 1 {
		t.Fatalf("logs=%d err=%v", total, err)
	}
}
