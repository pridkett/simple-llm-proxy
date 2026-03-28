package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// insertUsageLog inserts a usage_log row directly for test setup.
func insertUsageLog(t *testing.T, s *Storage, keyID int64, model string, cost float64, requestTime time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_logs (request_id, api_key_id, model, provider, endpoint, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_cost, status_code, latency_ms, request_time)
		VALUES (?, ?, ?, 'openai', '/v1/chat/completions', 10, 10, 0, 0, ?, 200, 100, ?)
	`, "req-"+model+"-"+requestTime.String(), keyID, model, cost, requestTime)
	if err != nil {
		t.Fatalf("insert usage_log: %v", err)
	}
}

func TestGetSpendSummary(t *testing.T) {
	// Reference time for date range tests
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	from := now.AddDate(0, 0, -7)
	to := now

	t.Run("returns empty slice when no usage logs exist", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		// Create a team, app, and key but no usage_log rows
		team, err := s.CreateTeam(ctx, "team-a")
		if err != nil {
			t.Fatalf("create team: %v", err)
		}
		app, err := s.CreateApplication(ctx, team.ID, "app-a")
		if err != nil {
			t.Fatalf("create app: %v", err)
		}
		_, err = s.CreateAPIKey(ctx, app.ID, "key-a", "aaaaaaaa", "hashaaaa", nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("create key: %v", err)
		}

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row (zero spend), got %d", len(rows))
		}
		if rows[0].TotalSpend != 0.0 {
			t.Errorf("expected TotalSpend=0.0 for no-usage key, got %v", rows[0].TotalSpend)
		}
	})

	t.Run("excludes flush rows from aggregation", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "team-b")
		app, _ := s.CreateApplication(ctx, team.ID, "app-b")
		key, err := s.CreateAPIKey(ctx, app.ID, "key-b", "bbbbbbbb", "hashbbbb", nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("create key: %v", err)
		}

		withinRange := now.AddDate(0, 0, -1) // T-1d, within [from, to)
		// Insert one real request row (should be counted)
		insertUsageLog(t, s, key.ID, "gpt-4", 0.01, withinRange)
		// Insert one flush row (should NOT be counted)
		insertUsageLog(t, s, key.ID, "_flush", 99.99, withinRange)

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		// TotalSpend must be 0.01, not 100.00 — flush row excluded
		if rows[0].TotalSpend != 0.01 {
			t.Errorf("expected TotalSpend=0.01, got %v (flush row may have been included)", rows[0].TotalSpend)
		}
	})

	t.Run("filters by team_id", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team1, _ := s.CreateTeam(ctx, "team-c1")
		team2, _ := s.CreateTeam(ctx, "team-c2")
		app1, _ := s.CreateApplication(ctx, team1.ID, "app-c1")
		app2, _ := s.CreateApplication(ctx, team2.ID, "app-c2")
		key1, _ := s.CreateAPIKey(ctx, app1.ID, "key-c1", "cccccc01", "hashcc01", nil, nil, nil, nil, nil)
		key2, _ := s.CreateAPIKey(ctx, app2.ID, "key-c2", "cccccc02", "hashcc02", nil, nil, nil, nil, nil)

		withinRange := now.AddDate(0, 0, -1)
		insertUsageLog(t, s, key1.ID, "gpt-4", 0.10, withinRange)
		insertUsageLog(t, s, key2.ID, "gpt-4", 0.20, withinRange)

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{TeamID: &team1.ID})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row for team1 filter, got %d", len(rows))
		}
		if rows[0].TeamID != team1.ID {
			t.Errorf("expected TeamID=%d, got %d", team1.ID, rows[0].TeamID)
		}
		if rows[0].TotalSpend != 0.10 {
			t.Errorf("expected TotalSpend=0.10, got %v", rows[0].TotalSpend)
		}
	})

	t.Run("filters by app_id", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "team-d")
		app1, _ := s.CreateApplication(ctx, team.ID, "app-d1")
		app2, _ := s.CreateApplication(ctx, team.ID, "app-d2")
		key1, _ := s.CreateAPIKey(ctx, app1.ID, "key-d1", "dddddd01", "hashdd01", nil, nil, nil, nil, nil)
		key2, _ := s.CreateAPIKey(ctx, app2.ID, "key-d2", "dddddd02", "hashdd02", nil, nil, nil, nil, nil)

		withinRange := now.AddDate(0, 0, -1)
		insertUsageLog(t, s, key1.ID, "gpt-4", 0.05, withinRange)
		insertUsageLog(t, s, key2.ID, "gpt-4", 0.15, withinRange)

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{AppID: &app1.ID})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row for app1 filter, got %d", len(rows))
		}
		if rows[0].AppID != app1.ID {
			t.Errorf("expected AppID=%d, got %d", app1.ID, rows[0].AppID)
		}
		if rows[0].TotalSpend != 0.05 {
			t.Errorf("expected TotalSpend=0.05, got %v", rows[0].TotalSpend)
		}
	})

	t.Run("filters by key_id", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "team-e")
		app, _ := s.CreateApplication(ctx, team.ID, "app-e")
		key1, _ := s.CreateAPIKey(ctx, app.ID, "key-e1", "eeeeee01", "hashee01", nil, nil, nil, nil, nil)
		key2, _ := s.CreateAPIKey(ctx, app.ID, "key-e2", "eeeeee02", "hashee02", nil, nil, nil, nil, nil)

		withinRange := now.AddDate(0, 0, -1)
		insertUsageLog(t, s, key1.ID, "gpt-4", 0.07, withinRange)
		insertUsageLog(t, s, key2.ID, "gpt-4", 0.13, withinRange)

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{KeyID: &key1.ID})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row for key1 filter, got %d", len(rows))
		}
		if rows[0].KeyID != key1.ID {
			t.Errorf("expected KeyID=%d, got %d", key1.ID, rows[0].KeyID)
		}
	})

	t.Run("date range filter excludes out-of-range rows", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "team-f")
		app, _ := s.CreateApplication(ctx, team.ID, "app-f")
		key, _ := s.CreateAPIKey(ctx, app.ID, "key-f", "ffffffff", "hashffff", nil, nil, nil, nil, nil)

		// T-10d: before `from` (T-7d) — should be excluded
		insertUsageLog(t, s, key.ID, "gpt-4", 1.00, now.AddDate(0, 0, -10))
		// T-1d: within [from, to) — should be included
		insertUsageLog(t, s, key.ID, "gpt-4", 0.50, now.AddDate(0, 0, -1))
		// T+1d: at or after `to` — should be excluded
		insertUsageLog(t, s, key.ID, "gpt-4", 2.00, now.AddDate(0, 0, 1))

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		if rows[0].TotalSpend != 0.50 {
			t.Errorf("expected TotalSpend=0.50 (only T-1d row), got %v", rows[0].TotalSpend)
		}
	})

	t.Run("exact soft-budget hit is included in alerts", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "team-g")
		app, _ := s.CreateApplication(ctx, team.ID, "app-g")
		softBudget := 5.00
		key, _ := s.CreateAPIKey(ctx, app.ID, "key-g", "gggggggg", "hashgggg", nil, nil, nil, &softBudget, nil)

		withinRange := now.AddDate(0, 0, -1)
		insertUsageLog(t, s, key.ID, "gpt-4", 5.00, withinRange) // exactly at soft budget

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		if rows[0].TotalSpend != 5.00 {
			t.Errorf("expected TotalSpend=5.00 (exact soft-budget hit), got %v", rows[0].TotalSpend)
		}
		if rows[0].SoftBudget == nil {
			t.Fatal("expected non-nil SoftBudget")
		}
		if *rows[0].SoftBudget != 5.00 {
			t.Errorf("expected SoftBudget=5.00, got %v", *rows[0].SoftBudget)
		}
	})

	t.Run("exact hard-budget hit is included in alerts", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "team-h")
		app, _ := s.CreateApplication(ctx, team.ID, "app-h")
		maxBudget := 10.00
		key, _ := s.CreateAPIKey(ctx, app.ID, "key-h", "hhhhhhhh", "hashhhhhh", nil, nil, &maxBudget, nil, nil)

		withinRange := now.AddDate(0, 0, -1)
		insertUsageLog(t, s, key.ID, "gpt-4", 10.00, withinRange) // exactly at max budget

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		if rows[0].TotalSpend != 10.00 {
			t.Errorf("expected TotalSpend=10.00 (exact hard-budget hit), got %v", rows[0].TotalSpend)
		}
		if rows[0].MaxBudget == nil {
			t.Fatal("expected non-nil MaxBudget")
		}
		if *rows[0].MaxBudget != 10.00 {
			t.Errorf("expected MaxBudget=10.00, got %v", *rows[0].MaxBudget)
		}
	})

	t.Run("nil budgets produce no alerts", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "team-i")
		app, _ := s.CreateApplication(ctx, team.ID, "app-i")
		// Key with NULL max_budget and NULL soft_budget
		key, _ := s.CreateAPIKey(ctx, app.ID, "key-i", "iiiiiiii", "hashiiii", nil, nil, nil, nil, nil)

		withinRange := now.AddDate(0, 0, -1)
		insertUsageLog(t, s, key.ID, "gpt-4", 3.00, withinRange)

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		if rows[0].MaxBudget != nil {
			t.Errorf("expected nil MaxBudget, got %v", *rows[0].MaxBudget)
		}
		if rows[0].SoftBudget != nil {
			t.Errorf("expected nil SoftBudget, got %v", *rows[0].SoftBudget)
		}
	})

	t.Run("zero-spend rows are included with total_spend=0", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "team-j")
		app, _ := s.CreateApplication(ctx, team.ID, "app-j")
		// Key with no usage_log rows in the date range
		key, err := s.CreateAPIKey(ctx, app.ID, "key-j", "jjjjjjjj", "hashjjjj", nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("create key: %v", err)
		}

		// Insert a usage row OUTSIDE the date range — should not count
		insertUsageLog(t, s, key.ID, "gpt-4", 5.00, now.AddDate(0, 0, -30))

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row (zero-spend key), got %d", len(rows))
		}
		if rows[0].TotalSpend != 0.0 {
			t.Errorf("expected TotalSpend=0.0 for out-of-range usage, got %v", rows[0].TotalSpend)
		}
	})

	t.Run("flush-only rows produce zero spend not excluded entirely", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "team-k")
		app, _ := s.CreateApplication(ctx, team.ID, "app-k")
		key, err := s.CreateAPIKey(ctx, app.ID, "key-k", "kkkkkkkk", "hashkkkk", nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("create key: %v", err)
		}

		withinRange := now.AddDate(0, 0, -1)
		// Insert ONLY flush rows — these are excluded from aggregation
		insertUsageLog(t, s, key.ID, "_flush", 99.99, withinRange)

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		// Key must still appear (it's active), but with TotalSpend=0 since flush rows are excluded
		if len(rows) != 1 {
			t.Fatalf("expected 1 row (flush-only key still present via LEFT JOIN), got %d", len(rows))
		}
		if rows[0].TotalSpend != 0.0 {
			t.Errorf("expected TotalSpend=0.0 for flush-only key, got %v (flush rows must be excluded)", rows[0].TotalSpend)
		}
	})

	t.Run("join names populated correctly", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "acme-team")
		app, _ := s.CreateApplication(ctx, team.ID, "acme-app")
		key, _ := s.CreateAPIKey(ctx, app.ID, "acme-key", "acmeacme", "hashacme", nil, nil, nil, nil, nil)

		withinRange := now.AddDate(0, 0, -1)
		insertUsageLog(t, s, key.ID, "gpt-4", 1.23, withinRange)

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		r := rows[0]
		if r.KeyName != "acme-key" {
			t.Errorf("expected KeyName=acme-key, got %q", r.KeyName)
		}
		if r.AppName != "acme-app" {
			t.Errorf("expected AppName=acme-app, got %q", r.AppName)
		}
		if r.TeamName != "acme-team" {
			t.Errorf("expected TeamName=acme-team, got %q", r.TeamName)
		}
	})

	t.Run("inactive keys are excluded", func(t *testing.T) {
		s := newTestStorage(t)
		ctx := context.Background()

		team, _ := s.CreateTeam(ctx, "team-l")
		app, _ := s.CreateApplication(ctx, team.ID, "app-l")
		key, _ := s.CreateAPIKey(ctx, app.ID, "key-l", "llllllll", "hashllll", nil, nil, nil, nil, nil)

		withinRange := now.AddDate(0, 0, -1)
		insertUsageLog(t, s, key.ID, "gpt-4", 5.00, withinRange)

		// Revoke the key (sets is_active=FALSE)
		if err := s.RevokeAPIKey(ctx, key.ID); err != nil {
			t.Fatalf("revoke key: %v", err)
		}

		rows, err := s.GetSpendSummary(ctx, from, to, storage.SpendFilters{})
		if err != nil {
			t.Fatalf("GetSpendSummary: %v", err)
		}
		// Revoked key must not appear
		if len(rows) != 0 {
			t.Errorf("expected 0 rows for inactive key, got %d", len(rows))
		}
	})
}
