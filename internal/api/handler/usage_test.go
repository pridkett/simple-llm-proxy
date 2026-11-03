package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

type mockUsageStore struct {
	mockSpendStorage
	rows  []storage.UsageAggregate
	err   error
	query storage.UsageQuery
}

func (m *mockUsageStore) GetUsage(_ context.Context, q storage.UsageQuery) ([]storage.UsageAggregate, error) {
	m.query = q
	return m.rows, m.err
}

func TestAdminUsage(t *testing.T) {
	t.Run("admin-only", func(t *testing.T) {
		for _, user := range []*storage.User{nil, regularUser()} {
			w := httptest.NewRecorder()
			AdminUsage(&mockUsageStore{}, nil)(w, newRequestWithUser("GET", "/admin/usage", user))
			if w.Code != 403 {
				t.Fatalf("got %d", w.Code)
			}
		}
	})
	t.Run("consistent summary and budget interval", func(t *testing.T) {
		s := &mockUsageStore{rows: []storage.UsageAggregate{{Bucket: 0, GroupID: "local", Name: "local", UsageMetrics: storage.UsageMetrics{Calls: 2, KnownCostCalls: 2, TotalTokens: 100, KnownTokenCalls: 2}}, {Bucket: 1, GroupID: "local", Name: "local", UsageMetrics: storage.UsageMetrics{Calls: 1, TotalSpend: .25, KnownCostCalls: 1}}}}
		w := httptest.NewRecorder()
		AdminUsage(s, func() *config.Config { return config.Defaults() })(w, newRequestWithUser("GET", "/admin/usage?from=2026-11-01&to=2026-11-02&timezone=America%2FNew_York&key_id=12", adminUser()))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var r usageResponse
		if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Summary.Calls != 3 || r.Summary.TotalSpend != .25 || len(r.Groups) != 1 || r.Groups[0].Calls != 3 || r.RetentionDays != 30 {
			t.Fatalf("response: %+v", r)
		}
		if !s.lastFrom.Equal(s.query.From) || !s.lastTo.Equal(s.query.To) || s.lastFilters.KeyID == nil || *s.lastFilters.KeyID != 12 {
			t.Fatal("budget range/filters diverged")
		}
		if r.FromTime.Format(time.RFC3339) != "2026-11-01T04:00:00Z" || r.ToTime.Format(time.RFC3339) != "2026-11-03T05:00:00Z" {
			t.Fatal("wrong UTC drill-down bounds")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("report must not be cached")
		}
	})
	t.Run("empty arrays", func(t *testing.T) {
		w := httptest.NewRecorder()
		AdminUsage(&mockUsageStore{}, nil)(w, newRequestWithUser("GET", "/admin/usage", adminUser()))
		var r map[string]any
		json.Unmarshal(w.Body.Bytes(), &r)
		for _, field := range []string{"groups", "series", "budget_rows", "alerts"} {
			if _, ok := r[field].([]any); !ok {
				t.Fatalf("%s is not an array", field)
			}
		}
	})
	t.Run("unattributed traffic does not show unrelated key budgets", func(t *testing.T) {
		s := &mockUsageStore{mockSpendStorage: mockSpendStorage{spendRows: []storage.SpendRow{{KeyID: 1, KeyName: "unrelated"}}}}
		w := httptest.NewRecorder()
		AdminUsage(s, nil)(w, newRequestWithUser("GET", "/admin/usage?unattributed=key", adminUser()))
		var r usageResponse
		json.Unmarshal(w.Body.Bytes(), &r)
		if w.Code != 200 || len(r.BudgetRows) != 0 || len(r.Alerts) != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
	})
	t.Run("query errors", func(t *testing.T) {
		w := httptest.NewRecorder()
		AdminUsage(&mockUsageStore{err: errors.New("db unavailable")}, nil)(w, newRequestWithUser("GET", "/admin/usage", adminUser()))
		if w.Code != 500 {
			t.Fatal(w.Code)
		}
	})
	t.Run("validation before storage", func(t *testing.T) {
		for _, query := range []string{"timezone=invalid", "timezone=Local", "interval=month", "group_by=provider", "key_id=-1", "team_id=no", "app_id=0", "from=bad", "from=2026-11-04&to=2026-11-03", "from=2025-01-01&to=2026-01-02", "since=2026-11-01T00:00:00Z", "unattributed=invalid", "from=2026-01-01&to=2026-02-02&interval=hour"} {
			t.Run(query, func(t *testing.T) {
				s := &mockUsageStore{}
				w := httptest.NewRecorder()
				AdminUsage(s, nil)(w, newRequestWithUser("GET", "/admin/usage?"+query, adminUser()))
				if w.Code != http.StatusBadRequest || len(s.query.Buckets) != 0 {
					t.Fatal(w.Code, w.Body.String())
				}
			})
		}
	})
}

func TestUsageCalendarBuckets(t *testing.T) {
	for _, tc := range []struct {
		name, query, first, last string
		count                    int
	}{
		{"spring forward", "from=2026-03-08&to=2026-03-08&timezone=America%2FNew_York&interval=hour", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z", 23},
		{"fall back", "from=2026-11-01&to=2026-11-01&timezone=America%2FNew_York&interval=hour", "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z", 25},
		{"fractional offset", "from=2026-11-01&to=2026-11-01&timezone=Asia%2FKolkata&interval=hour", "2026-10-31T18:30:00Z", "2026-11-01T18:30:00Z", 24},
		{"weeks clipped to interval", "from=2026-11-04&to=2026-11-18&interval=week", "2026-11-04T00:00:00Z", "2026-11-19T00:00:00Z", 3},
		{"exact bucket drill", "since=2026-11-01T05:30:00Z&until=2026-11-01T07:15:00Z&timezone=America%2FNew_York&interval=hour", "2026-11-01T05:30:00Z", "2026-11-01T07:15:00Z", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, _ := url.ParseQuery(tc.query)
			q, _, _, err := parseUsageQuery(v, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			bs := q.Buckets
			if len(bs) != tc.count || bs[0].Start.Format(time.RFC3339) != tc.first || bs[len(bs)-1].End.Format(time.RFC3339) != tc.last {
				t.Fatalf("buckets: %+v", bs)
			}
			seen := map[string]bool{}
			for i, b := range bs {
				if !b.Start.Before(b.End) {
					t.Fatal("empty bucket")
				}
				if seen[b.Label] {
					t.Fatal("ambiguous DST label", b.Label)
				}
				seen[b.Label] = true
				if i > 0 && !bs[i-1].End.Equal(b.Start) {
					t.Fatal("buckets overlap or have gaps")
				}
			}
		})
	}
}
