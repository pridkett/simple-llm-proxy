package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestResponsesJobCreateGet(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	keyID := int64(7)
	job := &storage.ResponsesJob{
		ID:            "resp_123",
		APIKeyID:      &keyID,
		DeploymentKey: "openai:gpt-5:",
		ModelName:     "gpt-5",
		Status:        "queued",
		RequestJSON:   `{"model":"gpt-5","input":"hi"}`,
	}
	if err := s.CreateResponsesJob(ctx, job); err != nil {
		t.Fatalf("CreateResponsesJob: %v", err)
	}

	got, err := s.GetResponsesJob(ctx, "resp_123")
	if err != nil {
		t.Fatalf("GetResponsesJob: %v", err)
	}
	if got == nil {
		t.Fatal("expected job, got nil")
	}
	if got.Status != "queued" || got.ModelName != "gpt-5" || got.DeploymentKey != "openai:gpt-5:" {
		t.Errorf("unexpected job fields: %+v", got)
	}
	if got.APIKeyID == nil || *got.APIKeyID != keyID {
		t.Errorf("expected api_key_id %d, got %v", keyID, got.APIKeyID)
	}
	if got.ResponseJSON != nil {
		t.Errorf("expected nil response_json, got %v", *got.ResponseJSON)
	}
}

func TestResponsesJobGetMissing(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	got, err := s.GetResponsesJob(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("GetResponsesJob: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing job, got %+v", got)
	}
}

func TestResponsesJobUpdate(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	job := &storage.ResponsesJob{
		ID:            "resp_456",
		DeploymentKey: "openai:gpt-5:",
		ModelName:     "gpt-5",
		Status:        "queued",
		RequestJSON:   `{}`,
	}
	if err := s.CreateResponsesJob(ctx, job); err != nil {
		t.Fatalf("CreateResponsesJob: %v", err)
	}

	respJSON := `{"id":"resp_456","status":"completed"}`
	now := time.Now().UTC()
	if err := s.UpdateResponsesJob(ctx, "resp_456", "completed", &respJSON, nil, &now); err != nil {
		t.Fatalf("UpdateResponsesJob: %v", err)
	}

	got, err := s.GetResponsesJob(ctx, "resp_456")
	if err != nil {
		t.Fatalf("GetResponsesJob: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("expected status completed, got %s", got.Status)
	}
	if got.ResponseJSON == nil || *got.ResponseJSON != respJSON {
		t.Errorf("unexpected response_json: %v", got.ResponseJSON)
	}
	if got.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}

func TestResponsesJobCRUD_DBErrors(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	job := &storage.ResponsesJob{
		ID: "resp_db_err", DeploymentKey: "openai:gpt-5:", ModelName: "gpt-5",
		Status: "queued", RequestJSON: `{}`,
	}
	if err := s.CreateResponsesJob(ctx, job); err != nil {
		t.Fatalf("CreateResponsesJob: %v", err)
	}

	// Close the underlying connection so every subsequent query fails, exercising
	// each CRUD method's error-wrapping branch.
	if err := s.db.Close(); err != nil {
		t.Fatalf("closing db: %v", err)
	}

	if err := s.CreateResponsesJob(ctx, &storage.ResponsesJob{ID: "resp_after_close", RequestJSON: `{}`}); err == nil {
		t.Error("expected CreateResponsesJob to fail after db close")
	}
	if _, err := s.GetResponsesJob(ctx, "resp_db_err"); err == nil {
		t.Error("expected GetResponsesJob to fail after db close")
	}
	if err := s.UpdateResponsesJob(ctx, "resp_db_err", "completed", nil, nil, nil); err == nil {
		t.Error("expected UpdateResponsesJob to fail after db close")
	}
	if _, err := s.ListPendingResponsesJobs(ctx); err == nil {
		t.Error("expected ListPendingResponsesJobs to fail after db close")
	}
}

func TestCreateResponsesJob_DuplicateIDFails(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	job := &storage.ResponsesJob{
		ID: "resp_dup", DeploymentKey: "openai:gpt-5:", ModelName: "gpt-5",
		Status: "queued", RequestJSON: `{}`,
	}
	if err := s.CreateResponsesJob(ctx, job); err != nil {
		t.Fatalf("first CreateResponsesJob: %v", err)
	}
	if err := s.CreateResponsesJob(ctx, job); err == nil {
		t.Error("expected duplicate id CreateResponsesJob to fail on the primary key constraint")
	}
}

func TestListPendingResponsesJobs(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	jobs := []*storage.ResponsesJob{
		{ID: "resp_pending_1", DeploymentKey: "openai:gpt-5:", ModelName: "gpt-5", Status: "queued", RequestJSON: `{}`},
		{ID: "resp_pending_2", DeploymentKey: "openai:gpt-5:", ModelName: "gpt-5", Status: "in_progress", RequestJSON: `{}`},
		{ID: "resp_done", DeploymentKey: "openai:gpt-5:", ModelName: "gpt-5", Status: "completed", RequestJSON: `{}`},
		{ID: "resp_failed", DeploymentKey: "openai:gpt-5:", ModelName: "gpt-5", Status: "failed", RequestJSON: `{}`},
	}
	for _, j := range jobs {
		if err := s.CreateResponsesJob(ctx, j); err != nil {
			t.Fatalf("CreateResponsesJob(%s): %v", j.ID, err)
		}
	}

	pending, err := s.ListPendingResponsesJobs(ctx)
	if err != nil {
		t.Fatalf("ListPendingResponsesJobs: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending jobs, got %d", len(pending))
	}
	ids := map[string]bool{}
	for _, j := range pending {
		ids[j.ID] = true
	}
	if !ids["resp_pending_1"] || !ids["resp_pending_2"] {
		t.Errorf("unexpected pending job set: %+v", ids)
	}
}
