package sqlite

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func TestFlushRowsExcludedFromRequestLogs(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	team, err := s.CreateTeam(ctx, "flush-only-team")
	if err != nil {
		t.Fatal(err)
	}
	app, err := s.CreateApplication(ctx, team.ID, "flush-only-app")
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.CreateAPIKey(ctx, app.ID, "flush-only-key", "prefix", "hash", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, entry := range []*storage.RequestLog{
		{RequestID: "real-new", Model: "gpt-4", Provider: "openai", PoolName: "real-pool", RequestTime: now},
		{RequestID: "real-old", Model: "gpt-4", Provider: "openai", RequestTime: now.Add(-time.Minute)},
		{RequestID: "legacy-flush", APIKeyID: &key.ID, Model: "_flush", Provider: "_flush", PoolName: "flush-pool", RequestTime: now.Add(time.Minute)},
	} {
		if err := s.LogRequest(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	// Exercise the actual snapshot writer too: its nullable columns must never
	// reach the request-log scanner.
	if err := s.FlushKeySpend(ctx, key.ID, 10); err != nil {
		t.Fatal(err)
	}

	for offset, want := range []string{"real-new", "real-old"} {
		logs, total, err := s.GetLogs(ctx, 1, offset, storage.LogsFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 || len(logs) != 1 || logs[0].RequestID != want {
			t.Fatalf("page %d: total=%d logs=%+v, want %s of 2 real requests", offset, total, logs, want)
		}
	}
	for _, filter := range []storage.LogsFilter{
		{Model: "_flush"}, {Provider: "_flush"}, {PoolName: "flush-pool"},
		{TeamID: &team.ID}, {AppID: &app.ID}, {KeyID: &key.ID},
	} {
		logs, total, err := s.GetLogs(ctx, 10, 0, filter)
		if err != nil || total != 0 || len(logs) != 0 {
			t.Fatalf("flush-only filter %+v: logs=%+v total=%d err=%v", filter, logs, total, err)
		}
	}
	entry, err := s.GetLogByID(ctx, "legacy-flush")
	if err != nil || entry != nil {
		t.Fatalf("flush detail: got %+v, %v; want nil, nil", entry, err)
	}
	meta, err := s.GetLogsMeta(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Teams) != 0 || len(meta.Apps) != 0 || len(meta.Keys) != 0 ||
		!reflect.DeepEqual(meta.Models, []string{"gpt-4"}) ||
		!reflect.DeepEqual(meta.Providers, []string{"openai"}) ||
		!reflect.DeepEqual(meta.Pools, []string{"real-pool"}) {
		t.Fatalf("metadata includes bookkeeping values or omits real traffic: %+v", meta)
	}
}

func TestKeySpendTotalsExcludeFlushSnapshots(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	team, err := s.CreateTeam(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	app, err := s.CreateApplication(ctx, team.ID, "app")
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.CreateAPIKey(ctx, app.ID, "key", "prefix", "hash", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	insertUsageLog(t, s, key.ID, "gpt-4", 0.25, time.Now().UTC())
	for range 3 {
		if err := s.FlushKeySpend(ctx, key.ID, 0.25); err != nil {
			t.Fatal(err)
		}
	}
	insertUsageLog(t, s, key.ID, "claude", 0.5, time.Now().UTC())
	if err := s.FlushKeySpend(ctx, key.ID, 0.75); err != nil {
		t.Fatal(err)
	}
	totals, err := s.GetKeySpendTotals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(totals) != 1 || totals[key.ID] != 0.75 {
		t.Fatalf("startup totals=%v, want only 0.75 of actual request spend", totals)
	}
}
