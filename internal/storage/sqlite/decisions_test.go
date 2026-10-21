package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestDecisionKeyMigrationAndAtomicPermissions(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	team, err := s.CreateTeam(ctx, "decisions")
	if err != nil {
		t.Fatal(err)
	}
	app, err := s.CreateApplication(ctx, team.ID, "app")
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.CreateAPIKey(ctx, app.ID, "old", "prefix", "old-hash", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Upgrade an existing key from the schema immediately before this feature.
	for _, q := range []string{`ALTER TABLE api_keys DROP COLUMN allow_decisions`, `DELETE FROM schema_migrations WHERE version=59`} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	old, err := s.GetAPIKeyByHash(ctx, "old-hash")
	if err != nil || old.AllowDecisions {
		t.Fatalf("legacy key unexpectedly enabled: %+v %v", old, err)
	}
	permission := true
	if err := s.UpdateAPIKey(ctx, key.ID, "enabled", nil, nil, nil, nil, []string{"jev"}, &permission); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAPIKey(ctx, key.ID, "renamed", nil, nil, nil, nil, []string{"luna"}); err != nil {
		t.Fatal(err)
	}
	keys, err := s.ListAPIKeys(ctx, app.ID)
	if err != nil || len(keys) != 1 || !keys[0].AllowDecisions {
		t.Fatalf("permission was not persisted: %+v %v", keys, err)
	}
	permission = false
	// A duplicate allowlist makes the transaction fail. Permission and name must
	// roll back with the allowlist, not silently disable or broaden the key.
	if err := s.UpdateAPIKey(ctx, key.ID, "bad", nil, nil, nil, nil, []string{"dup", "dup"}, &permission); err == nil {
		t.Fatal("expected allowlist conflict")
	}
	got, err := s.GetAPIKeyByID(ctx, key.ID)
	if err != nil || !got.AllowDecisions || got.Name != "renamed" {
		t.Fatalf("partial update: %+v %v", got, err)
	}
	models, err := s.GetKeyAllowedModels(ctx, key.ID)
	if err != nil || len(models) != 1 || models[0] != "luna" {
		t.Fatalf("allowlist rollback: %v %v", models, err)
	}
}

func TestDecisionLogFilteringAndPagination(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, endpoint := range []string{"/v1/decisions", "/v1/systemone", "/v1/chat/completions", "/v1/embeddings"} {
		if err := s.LogRequest(ctx, &storage.RequestLog{RequestID: endpoint, SessionID: "s", Model: "alias", Provider: "mock", Endpoint: endpoint, RequestTime: now.Add(time.Duration(i) * time.Second), StatusCode: 200, UsageKnown: true}); err != nil {
			t.Fatal(err)
		}
	}
	logs, count, err := s.GetLogs(ctx, 1, 1, storage.LogsFilter{RequestType: "decision", Model: "alias", SessionID: "s", Provider: "mock"})
	if err != nil || count != 2 || len(logs) != 1 || logs[0].Endpoint != "/v1/decisions" {
		t.Fatalf("page=%+v count=%d err=%v", logs, count, err)
	}
	logs, count, err = s.GetLogs(ctx, 10, 0, storage.LogsFilter{RequestType: "other", SessionID: "s"})
	if err != nil || count != 2 || len(logs) != 2 {
		t.Fatalf("other=%+v count=%d err=%v", logs, count, err)
	}
	from := now.Add(500 * time.Millisecond)
	_, count, err = s.GetLogs(ctx, 10, 0, storage.LogsFilter{RequestType: "decision", DateFrom: &from})
	if err != nil || count != 1 {
		t.Fatalf("combined time/type filter: %d %v", count, err)
	}
}
