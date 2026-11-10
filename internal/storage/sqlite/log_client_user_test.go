package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestClientUserLogsFilterAndDetail(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, row := range []struct{ id, session, user string }{
		{"u1-a", "chat-1", "user-1"},
		{"u1-b", "chat-2", "user-1"},
		{"u2", "chat-1", "user-2"},
		{"none", "chat-1", ""},
	} {
		if err := s.LogRequest(ctx, &storage.RequestLog{RequestID: row.id, SessionID: row.session, ClientUserID: row.user, Model: "m", Provider: "p", Endpoint: "/v1/chat/completions", RequestTime: now.Add(-time.Duration(i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	page, total, err := s.GetLogs(ctx, 50, 0, storage.LogsFilter{ClientUserID: "user-1"})
	if err != nil || total != 2 || len(page) != 2 || page[0].RequestID != "u1-a" || page[0].ClientUserID != "user-1" {
		t.Fatalf("user filter: page=%+v total=%d err=%v", page, total, err)
	}
	page, total, err = s.GetLogs(ctx, 50, 0, storage.LogsFilter{ClientUserID: "user-1", SessionID: "chat-1"})
	if err != nil || total != 1 || page[0].RequestID != "u1-a" {
		t.Fatalf("user+session filter: page=%+v total=%d err=%v", page, total, err)
	}
	if _, total, err = s.GetLogs(ctx, 50, 0, storage.LogsFilter{ClientUserID: "user"}); err != nil || total != 0 {
		t.Fatalf("user filter must be exact: total=%d err=%v", total, err)
	}
	if _, total, err = s.GetLogs(ctx, 50, 0, storage.LogsFilter{}); err != nil || total != 4 {
		t.Fatalf("no filter: total=%d err=%v", total, err)
	}
	detail, err := s.GetLogByID(ctx, "u2")
	if err != nil || detail.ClientUserID != "user-2" {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
}

func TestClientUserRetainedOnResponsesJob(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	if err := s.CreateResponsesJob(ctx, &storage.ResponsesJob{ID: "resp_1", DeploymentKey: "d", ModelName: "m", Status: "queued", RequestJSON: `{}`, SessionID: "chat-1", ClientUserID: "user-1"}); err != nil {
		t.Fatal(err)
	}
	job, err := s.GetResponsesJob(ctx, "resp_1")
	if err != nil || job.ClientUserID != "user-1" || job.SessionID != "chat-1" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	pending, err := s.ListPendingResponsesJobs(ctx)
	if err != nil || len(pending) != 1 || pending[0].ClientUserID != "user-1" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
}

func TestClientUserMigrationPreservesExistingLogs(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	for _, q := range []string{
		`DROP INDEX idx_usage_logs_client_user_time`,
		`ALTER TABLE usage_logs DROP COLUMN client_user_id`,
		`ALTER TABLE responses_jobs DROP COLUMN client_user_id`,
		`DELETE FROM schema_migrations WHERE version BETWEEN 63 AND 65`,
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
	if err != nil || old == nil || old.ClientUserID != "" {
		t.Fatalf("old=%+v err=%v", old, err)
	}
}
