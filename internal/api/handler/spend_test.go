package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// mockSpendStorage extends mockStorage with controllable GetSpendSummary behavior.
type mockSpendStorage struct {
	mockStorage
	spendRows   []storage.SpendRow
	spendErr    error
	lastFilters storage.SpendFilters
	lastFrom    time.Time
	lastTo      time.Time
}

func (m *mockSpendStorage) GetSpendSummary(_ context.Context, from, to time.Time, filters storage.SpendFilters) ([]storage.SpendRow, error) {
	m.lastFrom = from
	m.lastTo = to
	m.lastFilters = filters
	return m.spendRows, m.spendErr
}
func (m *mockSpendStorage) GetModelSpend(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.ModelSpendRow, error) { return nil, nil }
func (m *mockSpendStorage) GetDailySpend(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.DailySpendRow, error) { return nil, nil }

func TestAdminSpend(t *testing.T) {
	t.Run("returns 200 with aggregated spend rows for default 7d range", func(t *testing.T) {
		store := &mockSpendStorage{spendRows: []storage.SpendRow{}}
		req := newRequestWithUser(http.MethodGet, "/admin/spend", adminUser())
		w := httptest.NewRecorder()
		AdminSpend(store)(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp spendResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Rows == nil {
			t.Error("expected non-nil rows slice")
		}
		if resp.Alerts == nil {
			t.Error("expected non-nil alerts slice")
		}
	})

	t.Run("returns pre-computed alerts for keys over soft budget", func(t *testing.T) {
		softBudget := 8.0
		maxBudget := 10.0
		store := &mockSpendStorage{spendRows: []storage.SpendRow{
			{KeyID: 1, KeyName: "k1", AppName: "app1", TeamName: "team1",
				TotalSpend: 9.0, SoftBudget: &softBudget, MaxBudget: &maxBudget},
		}}
		req := newRequestWithUser(http.MethodGet, "/admin/spend", adminUser())
		w := httptest.NewRecorder()
		AdminSpend(store)(w, req)
		var resp spendResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if len(resp.Alerts) != 1 {
			t.Fatalf("expected 1 alert, got %d", len(resp.Alerts))
		}
		if resp.Alerts[0].AlertType != "soft" {
			t.Errorf("expected alert_type=soft, got %q", resp.Alerts[0].AlertType)
		}
	})

	t.Run("hard budget takes precedence over soft when both exceeded", func(t *testing.T) {
		softBudget := 5.0
		maxBudget := 8.0
		store := &mockSpendStorage{spendRows: []storage.SpendRow{
			{KeyID: 1, KeyName: "k1", AppName: "app1", TeamName: "team1",
				TotalSpend: 9.0, SoftBudget: &softBudget, MaxBudget: &maxBudget},
		}}
		req := newRequestWithUser(http.MethodGet, "/admin/spend", adminUser())
		w := httptest.NewRecorder()
		AdminSpend(store)(w, req)
		var resp spendResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if len(resp.Alerts) != 1 {
			t.Fatalf("expected 1 alert, got %d", len(resp.Alerts))
		}
		if resp.Alerts[0].AlertType != "hard" {
			t.Errorf("expected alert_type=hard, got %q", resp.Alerts[0].AlertType)
		}
	})

	t.Run("returns 400 for malformed date params", func(t *testing.T) {
		store := &mockSpendStorage{}
		req := newRequestWithUser(http.MethodGet, "/admin/spend?from=not-a-date", adminUser())
		w := httptest.NewRecorder()
		AdminSpend(store)(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("filters by team_id query param", func(t *testing.T) {
		store := &mockSpendStorage{spendRows: []storage.SpendRow{}}
		teamID := int64(42)
		req := newRequestWithUser(http.MethodGet, "/admin/spend?team_id=42", adminUser())
		w := httptest.NewRecorder()
		AdminSpend(store)(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if store.lastFilters.TeamID == nil {
			t.Fatal("expected TeamID filter to be set")
		}
		if *store.lastFilters.TeamID != teamID {
			t.Errorf("expected TeamID=%d, got %d", teamID, *store.lastFilters.TeamID)
		}
	})

	// Auth rejection tests: AdminSpend enforces admin-only access per-handler (same pattern as
	// AdminUsers, AdminTeams, etc.) by calling middleware.UserFromContext. A nil user or a
	// non-admin user receives 403. The RequireSession middleware (applied at route group level)
	// handles 401 for truly unauthenticated requests, but in unit tests the handler itself
	// returns 403 for both nil-user and non-admin cases since no session middleware is running.
	t.Run("non-admin session returns 403", func(t *testing.T) {
		store := &mockSpendStorage{}
		req := newRequestWithUser(http.MethodGet, "/admin/spend", regularUser())
		w := httptest.NewRecorder()
		AdminSpend(store)(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})

	t.Run("unauthenticated request returns 403 at handler level (401 handled by RequireSession middleware)", func(t *testing.T) {
		// Without session middleware, a request with no user in context gets 403 from AdminSpend.
		// The RequireSession middleware (not tested here) would intercept before the handler,
		// returning 401 for truly unauthenticated requests. This test verifies the handler-level guard.
		store := &mockSpendStorage{}
		req := newRequestWithUser(http.MethodGet, "/admin/spend", nil) // nil user = no session
		w := httptest.NewRecorder()
		AdminSpend(store)(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})

	// Date boundary semantics
	t.Run("to date is inclusive — row at 23:59 on to date is included", func(t *testing.T) {
		// When user sends to=2026-03-26, the SQL bound should be < 2026-03-27.
		// We verify that the `to` time passed to GetSpendSummary is the day AFTER the user date.
		store := &mockSpendStorage{spendRows: []storage.SpendRow{}}
		req := newRequestWithUser(http.MethodGet, "/admin/spend?from=2026-03-20&to=2026-03-26", adminUser())
		w := httptest.NewRecorder()
		AdminSpend(store)(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		// The SQL `to` bound must be 2026-03-27 (exclusive) so rows on 2026-03-26 are included
		expectedToSQL := time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC)
		if !store.lastTo.Equal(expectedToSQL) {
			t.Errorf("expected SQL to=%v (exclusive), got %v", expectedToSQL, store.lastTo)
		}
		// The response `to` field must reflect the user-facing inclusive date
		var resp spendResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.To != "2026-03-26" {
			t.Errorf("expected response to=2026-03-26 (inclusive), got %q", resp.To)
		}
	})

	t.Run("team_id=0 is treated as no filter (nil)", func(t *testing.T) {
		store := &mockSpendStorage{spendRows: []storage.SpendRow{}}
		req := newRequestWithUser(http.MethodGet, "/admin/spend?team_id=0", adminUser())
		w := httptest.NewRecorder()
		AdminSpend(store)(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if store.lastFilters.TeamID != nil {
			t.Errorf("expected nil TeamID for team_id=0, got %v", store.lastFilters.TeamID)
		}
	})
}
