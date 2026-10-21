package postgres

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// Each test gets a private schema. Never truncate or drop the supplied database.
// TEST_POSTGRES_URL must name a dedicated test database with CREATE permission.
func testDatabase(t *testing.T) (context.Context, string) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_URL to run real PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid TEST_POSTGRES_URL")
	}
	db := stdlib.OpenDB(*cfg)
	schema := fmt.Sprintf("proxy_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+quoted); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
		db.Close()
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	q.Set("timezone", "Pacific/Honolulu") // expose accidental server-timezone grouping
	q.Set("datestyle", "ISO, MDY")
	u.RawQuery = q.Encode()
	return ctx, u.String()
}

func newTestStorage(t *testing.T) (context.Context, *Storage, string) {
	t.Helper()
	ctx, dsn := testDatabase(t)
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, s, dsn
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, ctx context.Context, s *Storage) (*storage.Team, *storage.Application, *storage.APIKey) {
	t.Helper()
	check(t, s.UpsertUser(ctx, &storage.User{ID: "subject", Email: "user@example.test", Name: "User", IsAdmin: true}))
	team, err := s.CreateTeam(ctx, "Team")
	check(t, err)
	check(t, s.AddTeamMember(ctx, team.ID, "subject", "admin"))
	app, err := s.CreateApplication(ctx, team.ID, "App")
	check(t, err)
	key, err := s.CreateAPIKey(ctx, app.ID, "Key", "prefix", "hash", nil, nil, nil, nil, []string{"model"})
	check(t, err)
	return team, app, key
}

func TestMigrationConcurrencyAndReopen(t *testing.T) {
	ctx, dsn := testDatabase(t)
	const n = 4
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := New(ctx, dsn)
			if err != nil {
				errs <- err
				return
			}
			defer s.Close()
			errs <- s.Initialize(ctx)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		check(t, err)
	}
	s, err := New(ctx, dsn)
	check(t, err)
	check(t, s.Initialize(ctx))
	_, err = s.CreateTeam(ctx, "Persisted")
	check(t, err)
	check(t, s.Close())
	s, err = New(ctx, dsn)
	check(t, err)
	defer s.Close()
	check(t, s.Initialize(ctx))
	teams, err := s.ListTeams(ctx)
	check(t, err)
	if len(teams) != 1 || teams[0].Name != "Persisted" {
		t.Fatalf("reopen lost data: %+v", teams)
	}
	var count int
	check(t, s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count))
	if count != len(migrations) {
		t.Fatalf("migration count %d", count)
	}
}

func TestMigrationRollback(t *testing.T) {
	ctx, dsn := testDatabase(t)
	s, err := New(ctx, dsn)
	check(t, err)
	defer s.Close()
	// Inject a failing migration without changing the package migration sequence.
	// Baseline failure after creating users must roll back tables and metadata.
	_, err = s.DB().ExecContext(ctx, "CREATE TABLE teams(id BIGINT)")
	check(t, err)
	if err := s.Initialize(ctx); err == nil {
		t.Fatal("expected baseline conflict")
	}
	var users, metadata *string
	check(t, s.DB().QueryRowContext(ctx, "SELECT to_regclass('users')::text, to_regclass('schema_migrations')::text").Scan(&users, &metadata))
	if users != nil || metadata != nil {
		t.Fatal("failed migration left partial schema or metadata")
	}
	_, err = s.DB().ExecContext(ctx, "DROP TABLE teams")
	check(t, err)
	check(t, s.Initialize(ctx))
}

func TestIdentityKeysAndTransactions(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	team, app, key := fixture(t, ctx, s)
	u, err := s.GetUser(ctx, "subject")
	check(t, err)
	created := u.CreatedAt
	check(t, s.UpsertUser(ctx, &storage.User{ID: "subject", Email: "updated@example.test", Name: "Updated"}))
	u, err = s.GetUser(ctx, "subject")
	check(t, err)
	if u.Email != "updated@example.test" || !u.CreatedAt.Equal(created) || u.IsAdmin {
		t.Fatalf("user upsert: %+v", u)
	}
	users, err := s.ListUsers(ctx)
	check(t, err)
	if len(users) != 1 {
		t.Fatal(users)
	}
	missing, err := s.GetUser(ctx, "missing")
	check(t, err)
	if missing != nil {
		t.Fatal(missing)
	}
	check(t, s.UpdateTeamMemberRole(ctx, team.ID, "subject", "viewer"))
	members, err := s.ListTeamMembers(ctx, team.ID)
	check(t, err)
	if len(members) != 1 || members[0].Role != "viewer" {
		t.Fatal(members)
	}
	mine, err := s.ListMyTeams(ctx, "subject")
	check(t, err)
	if len(mine) != 1 {
		t.Fatal(mine)
	}
	if err := s.UpdateTeamMemberRole(ctx, team.ID, "subject", "invalid"); err == nil {
		t.Fatal("invalid role accepted")
	}
	apps, err := s.ListApplications(ctx, team.ID)
	check(t, err)
	if len(apps) != 1 {
		t.Fatal(apps)
	}
	keys, err := s.ListAPIKeys(ctx, app.ID)
	check(t, err)
	if len(keys) != 1 {
		t.Fatal(keys)
	}
	accessible, err := s.ListUserAccessibleKeys(ctx, "subject")
	check(t, err)
	if len(accessible) != 1 {
		t.Fatal(accessible)
	}
	byHash, err := s.GetAPIKeyByHash(ctx, "hash")
	check(t, err)
	if byHash == nil || byHash.ID != key.ID || byHash.TeamName != "Team" {
		t.Fatal(byHash)
	}
	missingKey, err := s.GetAPIKeyByHash(ctx, "missing")
	check(t, err)
	if missingKey != nil {
		t.Fatal(missingKey)
	}
	// Duplicate allowlist fails after key insertion; transaction must remove the key.
	_, err = s.CreateAPIKey(ctx, app.ID, "Bad", "p", "bad-hash", nil, nil, nil, nil, []string{"x", "x"})
	if err == nil {
		t.Fatal("duplicate allowlist accepted")
	}
	missingKey, err = s.GetAPIKeyByHash(ctx, "bad-hash")
	check(t, err)
	if missingKey != nil {
		t.Fatal("key insertion was not rolled back")
	}
	rpm, budget := 12, 3.5
	check(t, s.UpdateAPIKey(ctx, key.ID, "Updated", &rpm, nil, &budget, nil, []string{"other"}))
	if err := s.UpdateAPIKey(ctx, key.ID, "Rollback", nil, nil, nil, nil, []string{"x", "x"}); err == nil {
		t.Fatal("expected failed update")
	}
	key, err = s.GetAPIKeyByID(ctx, key.ID)
	check(t, err)
	if key.Name != "Updated" || key.MaxRPM == nil || *key.MaxRPM != rpm {
		t.Fatal("key update not atomic")
	}
	models, err := s.GetKeyAllowedModels(ctx, key.ID)
	check(t, err)
	if len(models) != 1 || models[0] != "other" {
		t.Fatal(models)
	}
	check(t, s.UpdateKeyAllowedModels(ctx, key.ID, nil))
	models, err = s.GetKeyAllowedModels(ctx, key.ID)
	check(t, err)
	if len(models) != 0 {
		t.Fatal(models)
	}
	check(t, s.RevokeAPIKey(ctx, key.ID))
	key, err = s.GetAPIKeyByID(ctx, key.ID)
	check(t, err)
	if key.IsActive {
		t.Fatal("key still active")
	}
	check(t, s.RemoveTeamMember(ctx, team.ID, "subject"))
	check(t, s.AddTeamMember(ctx, team.ID, "subject", "member"))
	check(t, s.DeleteApplication(ctx, app.ID))
	check(t, s.DeleteTeam(ctx, team.ID))
	mine, err = s.ListMyTeams(ctx, "subject")
	check(t, err)
	if len(mine) != 0 {
		t.Fatal("membership cascade failed")
	}
}

func TestSessionsAndRoutingState(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	sessions := s.SessionStore()
	binary := []byte{0, 1, 255, 128}
	check(t, sessions.CommitCtx(ctx, "token", binary, time.Now().Add(time.Hour)))
	data, found, err := sessions.FindCtx(ctx, "token")
	check(t, err)
	if !found || !bytes.Equal(data, binary) {
		t.Fatalf("binary session: %v %v", data, found)
	}
	check(t, sessions.Commit("token", []byte("updated"), time.Now().Add(time.Hour)))
	data, found, err = sessions.Find("token")
	check(t, err)
	if !found || string(data) != "updated" {
		t.Fatal("session upsert failed")
	}
	check(t, sessions.CommitCtx(ctx, "expired", binary, time.Now().Add(-time.Hour)))
	_, found, err = sessions.FindCtx(ctx, "expired")
	check(t, err)
	if found {
		t.Fatal("expired session found")
	}
	check(t, s.CleanExpiredSessions(ctx))
	check(t, sessions.Delete("token"))
	_, found, err = sessions.Find("token")
	check(t, err)
	if found {
		t.Fatal("deleted session found")
	}
	check(t, s.UpsertStickySession(ctx, "session", "pool", "deployment"))
	d, err := s.GetStickySession(ctx, "session", "pool")
	check(t, err)
	if d != "deployment" {
		t.Fatal(d)
	}
	check(t, s.UpsertStickySession(ctx, "session", "pool", "updated"))
	check(t, s.BulkUpsertStickySessions(ctx, []storage.StickySession{{SessionKey: "session", PoolName: "pool", DeploymentKey: "bulk", LastUsedAt: time.Now()}, {SessionKey: "old", PoolName: "pool", DeploymentKey: "expired", LastUsedAt: time.Now().Add(-2 * time.Hour)}}))
	d, err = s.GetStickySession(ctx, "session", "pool")
	check(t, err)
	if d != "bulk" {
		t.Fatal(d)
	}
	d, err = s.GetStickySession(ctx, "old", "pool")
	check(t, err)
	if d != "" {
		t.Fatal("expired sticky session returned")
	}
	n, err := s.DeleteExpiredStickySessions(ctx, time.Now().Add(-time.Hour))
	check(t, err)
	if n != 1 {
		t.Fatal(n)
	}
	check(t, s.UpsertPoolBudgetState(ctx, "pool", 1.0, "2026-10-21"))
	check(t, s.UpsertPoolBudgetState(ctx, "pool", 2.5, "2026-10-22"))
	// Date output must remain ISO even when server DateStyle is configured otherwise.
	s.DB().SetMaxOpenConns(1)
	_, err = s.DB().ExecContext(ctx, "SET datestyle = 'SQL, DMY'")
	check(t, err)
	state, err := s.GetPoolBudgetState(ctx)
	check(t, err)
	if len(state) != 1 || state[0].SpendToday != 2.5 || state[0].ResetDate != "2026-10-22" {
		t.Fatalf("budget upsert: %+v", state)
	}
}

func TestRequestLogsSpendAndRetention(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	team, app, key := fixture(t, ctx, s)
	now := time.Date(2026, 10, 21, 0, 30, 0, 0, time.UTC)
	snippet, ttft := "{\"message\":\"?\"}", int64(17)
	for i := 0; i < 3; i++ {
		check(t, s.LogRequest(ctx, &storage.RequestLog{RequestID: fmt.Sprintf("request-%d", i), APIKeyID: &key.ID, Model: "model", Provider: "provider", Endpoint: "/v1/chat/completions", InputTokens: 10, OutputTokens: 20, TotalCost: 0.5, StatusCode: 200, LatencyMS: 42, RequestTime: now.Add(time.Duration(i) * time.Hour), IsStreaming: true, DeploymentKey: "deployment", PoolName: "pool", TTFTMs: &ttft, ReqBodySnippet: &snippet, RespBodySnippet: "response", CacheReadTokens: 3, CacheWriteTokens: 4, UsageKnown: true, SessionID: "client-session"}))
	}
	check(t, s.FlushKeySpend(ctx, key.ID, 500))
	check(t, s.RecordKeySpend(ctx, key.ID, 0.5)) // existing compatibility no-op
	from, to := now.Add(-time.Hour), now.Add(4*time.Hour)
	filter := storage.LogsFilter{TeamID: &team.ID, AppID: &app.ID, KeyID: &key.ID, Model: "model", Provider: "provider", PoolName: "pool", DateFrom: &from, DateTo: &to, SessionID: "client-session"}
	logs, total, err := s.GetLogs(ctx, 2, 1, filter)
	check(t, err)
	if total != 3 || len(logs) != 2 || logs[0].RequestID != "request-1" {
		t.Fatalf("filtered page: %d %+v", total, logs)
	}
	got, err := s.GetLogByID(ctx, "request-0")
	check(t, err)
	if got == nil || got.ReqBodySnippet == nil || *got.ReqBodySnippet != snippet || got.TTFTMs == nil || *got.TTFTMs != ttft || !got.UsageKnown || !got.IsStreaming || got.CacheReadTokens != 3 || !got.RequestTime.Equal(now) || got.TeamName != "Team" {
		t.Fatalf("round trip: %+v", got)
	}
	missing, err := s.GetLogByID(ctx, "missing")
	check(t, err)
	if missing != nil {
		t.Fatal(missing)
	}
	meta, err := s.GetLogsMeta(ctx)
	check(t, err)
	if len(meta.Models) != 1 || len(meta.Teams) != 1 || len(meta.Keys) != 1 || len(meta.Pools) != 1 {
		t.Fatal(meta)
	}
	totals, err := s.GetKeySpendTotals(ctx)
	check(t, err)
	if totals[key.ID] != 1.5 {
		t.Fatal(totals)
	}
	for _, sf := range []storage.SpendFilters{{}, {TeamID: &team.ID, AppID: &app.ID, KeyID: &key.ID}} {
		summary, err := s.GetSpendSummary(ctx, from, to, sf)
		check(t, err)
		if len(summary) != 1 || summary[0].TotalSpend != 1.5 {
			t.Fatal(summary)
		}
		models, err := s.GetModelSpend(ctx, from, to, sf)
		check(t, err)
		if len(models) != 1 || models[0].TotalSpend != 1.5 || models[0].InputTokens != 30 || models[0].CacheWriteTokens != 12 {
			t.Fatal(models)
		}
		daily, err := s.GetDailySpend(ctx, from, to, sf)
		check(t, err)
		if len(daily) != 1 || daily[0].Day != "2026-10-21" || daily[0].RequestCount != 3 {
			t.Fatalf("UTC grouping: %+v", daily)
		}
	}
	// More than one retention batch, plus NULL body and unavailable usage.
	_, err = s.DB().ExecContext(ctx, `INSERT INTO usage_logs(request_id,model,provider,endpoint,status_code,latency_ms,request_time) SELECT 'old-'||n,'old','p','e',200,1,$1::timestamptz FROM generate_series(1,1001) n`, now.Add(-48*time.Hour))
	check(t, err)
	old, err := s.GetLogByID(ctx, "old-1")
	check(t, err)
	if old.ReqBodySnippet != nil || old.UsageKnown {
		t.Fatal(old)
	}
	n, err := s.DeleteOldRequestLogs(ctx, now.Add(-24*time.Hour))
	check(t, err)
	if n != 1001 {
		t.Fatal(n)
	}
	_, total, err = s.GetLogs(ctx, 10, 0, storage.LogsFilter{})
	check(t, err)
	if total != 3 {
		t.Fatal(total)
	}
	// Concurrent writers use a bounded pool and leave every row visible.
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.LogRequest(ctx, &storage.RequestLog{RequestID: fmt.Sprintf("concurrent-%d", i), Model: "concurrent", Provider: "p", Endpoint: "e", StatusCode: 200, RequestTime: now})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		check(t, err)
	}
	_, total, err = s.GetLogs(ctx, 50, 0, storage.LogsFilter{Model: "concurrent"})
	check(t, err)
	if total != 20 {
		t.Fatal(total)
	}
}

func TestCostOverridesWebhooksAndResponses(t *testing.T) {
	ctx, s, _ := newTestStorage(t)
	check(t, s.UpsertCostMapKey(ctx, "model", "cost-key"))
	check(t, s.UpsertCustomCostSpec(ctx, "model", `{"input_cost_per_token":0.1}`))
	ov, err := s.GetCostOverride(ctx, "model")
	check(t, err)
	if ov.CostMapKey != nil || ov.CustomSpec == nil {
		t.Fatal(ov)
	}
	check(t, s.UpsertCostMapKey(ctx, "model", "replacement"))
	ovs, err := s.ListCostOverrides(ctx)
	check(t, err)
	if len(ovs) != 1 || ovs[0].CustomSpec != nil {
		t.Fatal(ovs)
	}
	check(t, s.DeleteCostOverride(ctx, "model"))
	ov, err = s.GetCostOverride(ctx, "model")
	check(t, err)
	if ov != nil {
		t.Fatal(ov)
	}
	sub, err := s.CreateWebhookSubscription(ctx, &storage.WebhookSubscription{URL: "https://example.test/hook", Events: []string{"event"}, Secret: "secret", Enabled: true})
	check(t, err)
	sub.URL = "https://example.test/updated"
	check(t, s.UpdateWebhookSubscription(ctx, sub))
	subs, err := s.ListWebhookSubscriptions(ctx)
	check(t, err)
	if len(subs) != 1 || subs[0].URL != sub.URL {
		t.Fatal(subs)
	}
	enabled, err := s.GetEnabledWebhooksByEvent(ctx, "event")
	check(t, err)
	if len(enabled) != 1 {
		t.Fatal(enabled)
	}
	event, err := s.InsertNotificationEvent(ctx, "event", `{"value":"?"}`)
	check(t, err)
	delivery, err := s.InsertWebhookDelivery(ctx, &sub.ID, event)
	check(t, err)
	_, err = s.InsertWebhookDelivery(ctx, nil, event)
	check(t, err)
	check(t, s.UpdateWebhookDeliveryStatus(ctx, delivery, "delivered", 200, 2))
	var status string
	var attempts int
	var attempted time.Time
	check(t, s.DB().QueryRowContext(ctx, "SELECT status,attempt_count,last_attempt_at FROM webhook_deliveries WHERE id=$1", delivery).Scan(&status, &attempts, &attempted))
	if status != "delivered" || attempts != 2 || attempted.IsZero() {
		t.Fatal(status, attempts)
	}
	for _, filter := range []string{"", "event"} {
		events, total, err := s.ListNotificationEvents(ctx, 10, 0, filter)
		check(t, err)
		if len(events) != 1 || total != 1 {
			t.Fatal(events, total)
		}
	}
	check(t, s.DeleteWebhookSubscription(ctx, sub.ID))
	n, err := s.DeleteOldNotificationEvents(ctx, time.Now().Add(time.Hour))
	check(t, err)
	if n != 1 {
		t.Fatal(n)
	}
	var count int
	check(t, s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM webhook_deliveries").Scan(&count))
	if count != 0 {
		t.Fatal("delivery cascade failed")
	}
	job := &storage.ResponsesJob{ID: "resp-1", DeploymentKey: "dep", ModelName: "model", PoolName: "pool", Status: "queued", RequestJSON: `{"input":"?"}`, SessionID: "client-session"}
	check(t, s.CreateResponsesJob(ctx, job))
	if err := s.CreateResponsesJob(ctx, job); err == nil {
		t.Fatal("duplicate job accepted")
	}
	pending, err := s.ListPendingResponsesJobs(ctx)
	check(t, err)
	if len(pending) != 1 || pending[0].SessionID != job.SessionID {
		t.Fatal(pending)
	}
	response, completed := `{"output":[]}`, time.Now().UTC().Truncate(time.Microsecond)
	check(t, s.UpdateResponsesJob(ctx, job.ID, "completed", &response, nil, &completed))
	got, err := s.GetResponsesJob(ctx, job.ID)
	check(t, err)
	if got.Status != "completed" || got.ResponseJSON == nil || *got.ResponseJSON != response || got.CompletedAt == nil || !got.CompletedAt.Equal(completed) {
		t.Fatalf("job round trip: %+v", got)
	}
	pending, err = s.ListPendingResponsesJobs(ctx)
	check(t, err)
	if len(pending) != 0 {
		t.Fatal(pending)
	}
	got, err = s.GetResponsesJob(ctx, "missing")
	check(t, err)
	if got != nil {
		t.Fatal(got)
	}
}

func TestCanceledConnectionDoesNotExposeCredentials(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(ctx, "postgres://private-user:secret-password@127.0.0.1:1/db?sslmode=disable")
	if err == nil || strings.Contains(err.Error(), "private-user") || strings.Contains(err.Error(), "secret-password") {
		t.Fatalf("unsafe error: %v", err)
	}
}
