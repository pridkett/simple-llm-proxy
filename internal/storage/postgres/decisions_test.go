package postgres

import (
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestDecisionPermissionUpgradeAndTransactions(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	_, app, key := fixture(t, ctx, s)
	// Replay only the decision migration against a populated prior schema.
	_, err := s.DB().ExecContext(ctx, `ALTER TABLE api_keys DROP COLUMN allow_decisions; DROP INDEX idx_usage_logs_endpoint_time; DELETE FROM schema_migrations WHERE version=3`)
	check(t, err)
	check(t, s.Initialize(ctx))
	old, err := s.GetAPIKeyByHash(ctx, key.KeyHash)
	check(t, err)
	if old.AllowDecisions {
		t.Fatal("legacy PostgreSQL key gained decision access")
	}
	permission := true
	created, err := s.CreateAPIKey(ctx, app.ID, "Decision key", "prefix2", "hash2", nil, nil, nil, nil, []string{"jev"}, &permission)
	check(t, err)
	if !created.AllowDecisions {
		t.Fatal("create permission lost")
	}
	check(t, s.UpdateAPIKey(ctx, key.ID, "enabled", nil, nil, nil, nil, []string{"jev"}, &permission))
	check(t, s.UpdateAPIKey(ctx, key.ID, "renamed", nil, nil, nil, nil, []string{"luna"}))
	got, err := s.GetAPIKeyByID(ctx, key.ID)
	check(t, err)
	if !got.AllowDecisions || got.Name != "renamed" {
		t.Fatalf("omitted permission reset: %+v", got)
	}
	permission = false
	if err := s.UpdateAPIKey(ctx, key.ID, "bad", nil, nil, nil, nil, []string{"dup", "dup"}, &permission); err == nil {
		t.Fatal("expected allowlist conflict")
	}
	got, err = s.GetAPIKeyByID(ctx, key.ID)
	check(t, err)
	if !got.AllowDecisions || got.Name != "renamed" {
		t.Fatalf("partial PostgreSQL update: %+v", got)
	}
	check(t, s.UpdateAPIKey(ctx, key.ID, "disabled", nil, nil, nil, nil, nil, &permission))
	keys, err := s.ListAPIKeys(ctx, app.ID)
	check(t, err)
	for _, k := range keys {
		if k.ID == key.ID && k.AllowDecisions {
			t.Fatal("permission disable lost")
		}
	}
}

func TestDecisionLogsFiltersAndSpend(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	team, app, key := fixture(t, ctx, s)
	now := time.Now().UTC()
	for i, endpoint := range []string{"/v1/decisions", "/v1/systemone", "/v1/chat/completions"} {
		check(t, s.LogRequest(ctx, &storage.RequestLog{RequestID: endpoint, SessionID: "decision-session", APIKeyID: &key.ID, Model: "model", Provider: "mock", Endpoint: endpoint, RequestTime: now.Add(time.Duration(i) * time.Second), InputTokens: 50, CacheReadTokens: 30, CacheWriteTokens: 20, TotalCost: 0.1, StatusCode: 200, UsageKnown: true}))
	}
	logs, total, err := s.GetLogs(ctx, 1, 1, storage.LogsFilter{RequestType: "decision", TeamID: &team.ID, AppID: &app.ID, KeyID: &key.ID, Model: "model", Provider: "mock", SessionID: "decision-session"})
	check(t, err)
	if total != 2 || len(logs) != 1 || logs[0].Endpoint != "/v1/decisions" {
		t.Fatalf("decision page=%+v count=%d", logs, total)
	}
	logs, total, err = s.GetLogs(ctx, 10, 0, storage.LogsFilter{RequestType: "other"})
	check(t, err)
	if total != 1 || len(logs) != 1 || logs[0].Endpoint != "/v1/chat/completions" {
		t.Fatalf("other page=%+v count=%d", logs, total)
	}
	spend, err := s.GetModelSpend(ctx, now.Add(-time.Minute), now.Add(time.Minute), storage.SpendFilters{KeyID: &key.ID})
	check(t, err)
	if len(spend) != 1 || spend[0].RequestCount != 3 || spend[0].InputTokens != 150 || spend[0].CacheReadTokens != 90 || spend[0].CacheWriteTokens != 60 {
		t.Fatalf("decision spend aggregation: %+v", spend)
	}
}
