// Package testutil holds cross-backend behavioral contracts.
package testutil

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

func UsageContract(t *testing.T, ctx context.Context, s storage.Storage) {
	t.Helper()
	reports := s.(storage.UsageStorage)
	accounts := s.(storage.AccountingStorage)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	team1, err := s.CreateTeam(ctx, "usage-one")
	check(err)
	team2, err := s.CreateTeam(ctx, "usage-two")
	check(err)
	app1, err := s.CreateApplication(ctx, team1.ID, "shared-name")
	check(err)
	app2, err := s.CreateApplication(ctx, team2.ID, "shared-name")
	check(err)
	key1, err := s.CreateAPIKey(ctx, app1.ID, "revoked", "usage-1", "usage-hash-1", nil, nil, nil, nil, nil)
	check(err)
	key2, err := s.CreateAPIKey(ctx, app2.ID, "active", "usage-2", "usage-hash-2", nil, nil, nil, nil, nil)
	check(err)
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	ptr := func(v int64) *int64 { return &v }
	money := func(v string) *string { return &v }
	known, unknown := true, false
	index := 0
	insert := func(log storage.RequestLog, rec *accounting.Record) {
		t.Helper()
		index++
		log.RequestID = fmt.Sprintf("usage-contract-%d", index)
		log.Endpoint = "/v1/chat/completions"
		if log.Provider == "" {
			log.Provider = "openai"
		}
		if log.StatusCode == 0 {
			log.StatusCode = 200
		}
		if log.RequestTime.IsZero() {
			log.RequestTime = from.Add(time.Duration(index) * time.Hour)
		}
		if rec != nil {
			rec.ID = fmt.Sprintf("usage-account-%d", index)
			rec.RequestID = log.RequestID
			rec.State = "final"
			rec.APIKeyID = log.APIKeyID
			check(accounts.PutAccounting(ctx, rec))
			log.AccountingID = rec.ID
		}
		check(s.LogRequest(ctx, &log))
	}
	insert(storage.RequestLog{Model: "local", APIKeyID: &key1.ID, RequestTime: from, InputTokens: 100, OutputTokens: 20, UsageKnown: true, CostKnown: &known, CostSource: "estimate", CostCoverage: "complete"}, &accounting.Record{Usage: accounting.Usage{InputTokens: ptr(100), OutputTokens: ptr(20), TotalTokens: ptr(120), InputDetails: accounting.InputDetails{CacheReadTokens: ptr(70), CacheWriteTokens: ptr(10)}, OutputDetails: accounting.OutputDetails{ReasoningTokens: ptr(5)}}, Cost: accounting.Cost{Amount: money("0"), Source: "estimate"}, Attempts: []accounting.Attempt{{Status: "failed"}, {Status: "success"}}})
	insert(storage.RequestLog{Model: "opaque", TotalCost: .5, UsageKnown: false, CostKnown: &known, CostSource: "upstream", CostCoverage: "complete"}, &accounting.Record{Usage: accounting.Usage{TotalTokens: ptr(900)}, Cost: accounting.Cost{Amount: money("0.5"), Source: "upstream"}})
	insert(storage.RequestLog{Model: "unknown", APIKeyID: &key2.ID, UsageKnown: false, CostKnown: &unknown, TotalCost: 999}, &accounting.Record{})
	insert(storage.RequestLog{Model: "legacy-anthropic", Provider: "anthropic", APIKeyID: &key2.ID, UsageKnown: true, InputTokens: 10, OutputTokens: 5, CacheReadTokens: 30, CacheWriteTokens: 5, TotalCost: .05}, nil)
	insert(storage.RequestLog{Model: "partial", APIKeyID: &key2.ID, RequestTime: from.Add(12 * time.Hour), StatusCode: 500, CostKnown: &known, CostSource: "mixed", CostCoverage: "partial", TotalCost: .2}, &accounting.Record{Usage: accounting.Usage{InputTokens: ptr(50)}, Cost: accounting.Cost{Amount: money("0.2"), Source: "mixed"}})
	insert(storage.RequestLog{Model: "_flush", APIKeyID: &key2.ID, TotalCost: 100, CostKnown: &known}, nil)
	insert(storage.RequestLog{Model: "before", APIKeyID: &key2.ID, RequestTime: from.Add(-time.Nanosecond), CostKnown: &known, TotalCost: 100}, nil)
	insert(storage.RequestLog{Model: "after", APIKeyID: &key2.ID, RequestTime: to, CostKnown: &known, TotalCost: 100}, nil)
	check(s.RevokeAPIKey(ctx, key1.ID))
	q := storage.UsageQuery{From: from, To: to, GroupBy: "model", Buckets: []storage.UsageBucket{{Start: from, End: from.Add(12 * time.Hour)}, {Start: from.Add(12 * time.Hour), End: to}}}
	rows, err := reports.GetUsage(ctx, q)
	check(err)
	totals := storage.UsageMetrics{}
	models := map[string]storage.UsageAggregate{}
	for _, r := range rows {
		totals.Add(r.UsageMetrics)
		models[r.GroupID] = r
	}
	if totals.Calls != 5 || totals.FailedCalls != 1 || totals.TotalTokens != 1070 || totals.InputTokens != 195 || totals.OutputTokens != 25 || totals.CacheReadTokens != 100 || totals.CacheWriteTokens != 15 || totals.ReasoningTokens != 5 || totals.KnownTokenCalls != 3 || totals.KnownInputCalls != 3 || totals.KnownOutputCalls != 2 || totals.KnownCostCalls != 4 || totals.PartialCostCalls != 1 || totals.LegacyCalls != 1 || math.Abs(totals.TotalSpend-.75) > 1e-8 {
		t.Fatalf("unexpected usage totals: %+v", totals)
	}
	if totals.UpstreamCostCalls != 1 || totals.EstimatedCostCalls != 1 || totals.MixedCostCalls != 1 {
		t.Fatalf("cost sources: %+v", totals)
	}
	if r := models["opaque"]; r.TotalTokens != 900 || r.KnownInputCalls != 0 || r.KnownOutputCalls != 0 {
		t.Fatalf("total-only usage was fabricated: %+v", r)
	}
	if r := models["unknown"]; r.KnownCostCalls != 0 || r.TotalSpend != 0 || r.KnownTokenCalls != 0 {
		t.Fatalf("unknown counted as known: %+v", r)
	}
	if r := models["local"]; r.KnownCostCalls != 1 || r.TotalSpend != 0 || r.Calls != 1 {
		t.Fatalf("zero/retries/revocation: %+v", r)
	}
	if models["partial"].Bucket != 1 {
		t.Fatal("boundary request is in the wrong bucket")
	}
	for _, test := range []struct {
		name         string
		filters      storage.LogsFilter
		unattributed string
		calls        int64
	}{{"revoked key", storage.LogsFilter{KeyID: &key1.ID}, "", 1}, {"team", storage.LogsFilter{TeamID: &team2.ID}, "", 3}, {"app", storage.LogsFilter{AppID: &app2.ID}, "", 3}, {"model", storage.LogsFilter{Model: "partial"}, "", 1}, {"master key", storage.LogsFilter{}, "key", 1}} {
		t.Run(test.name, func(t *testing.T) {
			query := q
			query.Filters = test.filters
			query.Unattributed = test.unattributed
			rs, e := reports.GetUsage(ctx, query)
			if e != nil {
				t.Fatal(e)
			}
			var count int64
			for _, r := range rs {
				count += r.Calls
			}
			if count != test.calls {
				t.Fatalf("calls=%d want %d", count, test.calls)
			}
		})
	}
	q.GroupBy = "app"
	rows, err = reports.GetUsage(ctx, q)
	check(err)
	ids := map[string]bool{}
	for _, r := range rows {
		ids[r.GroupID] = true
	}
	if len(ids) != 3 {
		t.Fatalf("duplicate names or unattributed traffic lost: %+v", rows)
	}
	for _, group := range []string{"key", "team"} {
		q.GroupBy = group
		rs, e := reports.GetUsage(ctx, q)
		check(e)
		var calls int64
		for _, r := range rs {
			calls += r.Calls
		}
		if calls != 5 {
			t.Fatalf("%s calls=%d", group, calls)
		}
	}
	_, count, err := s.GetLogs(ctx, 50, 0, storage.LogsFilter{DateFrom: &from, DateBefore: &to})
	check(err)
	if count != 5 {
		t.Fatalf("Logs drill-down count=%d, report count=5", count)
	}
	_, count, err = s.GetLogs(ctx, 50, 0, storage.LogsFilter{DateFrom: &from, DateBefore: &to, Unattributed: "key"})
	check(err)
	if count != 1 {
		t.Fatalf("unattributed Logs count=%d", count)
	}
}
