package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestWithSQLiteTimeFormat(t *testing.T) {
	for in, want := range map[string]string{
		"/data/proxy.db":                      "/data/proxy.db?_time_format=sqlite",
		":memory:":                            ":memory:?_time_format=sqlite",
		"file:proxy.db?mode=rwc":              "file:proxy.db?mode=rwc&_time_format=sqlite",
		"file:proxy.db?_time_format=sqlite&x": "file:proxy.db?_time_format=sqlite&x",
	} {
		if got := withSQLiteTimeFormat(in); got != want {
			t.Errorf("withSQLiteTimeFormat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRequestTimeIsReadableBySQLiteDateFunctions(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 10, 0, 33, 11, 949138787, time.UTC)
	if err := s.LogRequest(ctx, &storage.RequestLog{RequestID: "new", Model: "m", Provider: "p", Endpoint: "/v1/chat/completions", RequestTime: at}); err != nil {
		t.Fatal(err)
	}
	var raw string
	var day *string
	if err := s.db.QueryRowContext(ctx, `SELECT CAST(request_time AS TEXT), date(request_time) FROM usage_logs WHERE request_id = 'new'`).Scan(&raw, &day); err != nil {
		t.Fatal(err)
	}
	if raw != "2026-11-10 00:33:11.949138787+00:00" || day == nil || *day != "2026-11-10" {
		t.Fatalf("stored %q, date() = %v", raw, day)
	}
	got, err := s.GetLogByID(ctx, "new")
	if err != nil || !got.RequestTime.Equal(at) {
		t.Fatalf("round trip: %v err=%v", got, err)
	}
}

func TestRequestTimeMigrationNormalizesGoStringTimes(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	for _, q := range []string{
		`DELETE FROM schema_migrations WHERE version = 67`,
		`INSERT INTO usage_logs (request_id,model,provider,endpoint,status_code,latency_ms,request_time) VALUES ('old','m','p','/v1/chat/completions',200,10,'2026-10-18 22:33:06.550426722 +0000 UTC')`,
		`INSERT INTO usage_logs (request_id,model,provider,endpoint,status_code,latency_ms,request_time) VALUES ('whole','m','p','/v1/chat/completions',200,10,'2026-10-18 22:33:06 +0000 UTC')`,
		`INSERT INTO usage_logs (request_id,model,provider,endpoint,status_code,latency_ms,request_time) VALUES ('flush','_flush','_flush','_flush',0,0,'2026-10-18 22:33:06')`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"old":   "2026-10-18 22:33:06.550426722+00:00",
		"whole": "2026-10-18 22:33:06+00:00",
		"flush": "2026-10-18 22:33:06",
	} {
		var raw string
		var day *string
		if err := s.db.QueryRowContext(ctx, `SELECT CAST(request_time AS TEXT), date(request_time) FROM usage_logs WHERE request_id = ?`, id).Scan(&raw, &day); err != nil {
			t.Fatal(err)
		}
		if raw != want || day == nil || *day != "2026-10-18" {
			t.Fatalf("%s: stored %q, date() = %v", id, raw, day)
		}
	}
	from := time.Date(2026, 10, 18, 22, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 18, 23, 0, 0, 0, time.UTC)
	logs, total, err := s.GetLogs(ctx, 10, 0, storage.LogsFilter{DateFrom: &from, DateTo: &to})
	if err != nil || total != 2 || len(logs) != 2 {
		t.Fatalf("date filter across migrated rows: total=%d err=%v", total, err)
	}
	old, err := s.GetLogByID(ctx, "old")
	if err != nil || !old.RequestTime.Equal(time.Date(2026, 10, 18, 22, 33, 6, 550426722, time.UTC)) {
		t.Fatalf("migrated row reads back as %v err=%v", old, err)
	}
}
