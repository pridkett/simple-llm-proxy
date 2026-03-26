package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// mockSessionStore is a minimal in-memory SCS store for tests.
// It implements scs.Store (Find, Commit, Delete) but always returns empty — no real session.
type mockSessionStore struct{}

func (m *mockSessionStore) Find(token string) ([]byte, bool, error) {
	return nil, false, nil
}

func (m *mockSessionStore) Commit(token string, b []byte, expiry interface{}) error {
	return nil
}

func (m *mockSessionStore) Delete(token string) error {
	return nil
}

// mockStorage implements storage.Storage for testing RequireSession.
// Only GetUser is used by RequireSession.
type mockSessionStorage struct {
	user *storage.User
	err  error
}

func (m *mockSessionStorage) Initialize(ctx context.Context) error { return nil }
func (m *mockSessionStorage) Close() error                         { return nil }
func (m *mockSessionStorage) LogRequest(ctx context.Context, log *storage.RequestLog) error {
	return nil
}
func (m *mockSessionStorage) GetLogs(ctx context.Context, limit, offset int) ([]*storage.RequestLog, int, error) {
	return nil, 0, nil
}
func (m *mockSessionStorage) UpsertCostMapKey(ctx context.Context, modelName, costMapKey string) error {
	return nil
}
func (m *mockSessionStorage) UpsertCustomCostSpec(ctx context.Context, modelName, specJSON string) error {
	return nil
}
func (m *mockSessionStorage) GetCostOverride(ctx context.Context, modelName string) (*storage.CostOverride, error) {
	return nil, nil
}
func (m *mockSessionStorage) DeleteCostOverride(ctx context.Context, modelName string) error {
	return nil
}
func (m *mockSessionStorage) ListCostOverrides(ctx context.Context) ([]*storage.CostOverride, error) {
	return nil, nil
}
func (m *mockSessionStorage) UpsertUser(ctx context.Context, u *storage.User) error { return nil }
func (m *mockSessionStorage) GetUser(ctx context.Context, id string) (*storage.User, error) {
	return m.user, m.err
}
func (m *mockSessionStorage) ListUsers(ctx context.Context) ([]*storage.User, error) { return nil, nil }
func (m *mockSessionStorage) CreateTeam(ctx context.Context, name string) (*storage.Team, error) {
	return nil, nil
}
func (m *mockSessionStorage) DeleteTeam(ctx context.Context, id int64) error { return nil }
func (m *mockSessionStorage) ListTeams(ctx context.Context) ([]*storage.Team, error) {
	return nil, nil
}
func (m *mockSessionStorage) AddTeamMember(ctx context.Context, teamID int64, userID, role string) error {
	return nil
}
func (m *mockSessionStorage) RemoveTeamMember(ctx context.Context, teamID int64, userID string) error {
	return nil
}
func (m *mockSessionStorage) UpdateTeamMemberRole(ctx context.Context, teamID int64, userID, role string) error {
	return nil
}
func (m *mockSessionStorage) ListTeamMembers(ctx context.Context, teamID int64) ([]*storage.TeamMember, error) {
	return nil, nil
}
func (m *mockSessionStorage) ListMyTeams(ctx context.Context, userID string) ([]*storage.TeamMember, error) {
	return nil, nil
}
func (m *mockSessionStorage) CreateApplication(ctx context.Context, teamID int64, name string) (*storage.Application, error) {
	return nil, nil
}
func (m *mockSessionStorage) DeleteApplication(ctx context.Context, id int64) error { return nil }
func (m *mockSessionStorage) ListApplications(ctx context.Context, teamID int64) ([]*storage.Application, error) {
	return nil, nil
}
func (m *mockSessionStorage) CleanExpiredSessions(ctx context.Context) error { return nil }

// API Key CRUD stubs — required by interface, not exercised by session tests.
func (m *mockSessionStorage) CreateAPIKey(_ context.Context, _ int64, _, _, _ string, _, _ *int, _, _ *float64, _ []string) (*storage.APIKey, error) {
	return nil, nil
}
func (m *mockSessionStorage) GetAPIKeyByHash(_ context.Context, _ string) (*storage.APIKey, error) {
	return nil, nil
}
func (m *mockSessionStorage) ListAPIKeys(_ context.Context, _ int64) ([]*storage.APIKey, error) {
	return nil, nil
}
func (m *mockSessionStorage) RevokeAPIKey(_ context.Context, _ int64) error { return nil }
func (m *mockSessionStorage) GetKeyAllowedModels(_ context.Context, _ int64) ([]string, error) {
	return nil, nil
}
func (m *mockSessionStorage) UpdateKeyAllowedModels(_ context.Context, _ int64, _ []string) error {
	return nil
}
func (m *mockSessionStorage) UpdateAPIKey(_ context.Context, _ int64, _ string, _ *int, _ *int, _ *float64, _ *float64, _ []string) error {
	return nil
}
func (m *mockSessionStorage) RecordKeySpend(_ context.Context, _ int64, _ float64) error {
	return nil
}
func (m *mockSessionStorage) GetKeySpendTotals(_ context.Context) (map[int64]float64, error) {
	return nil, nil
}
func (m *mockSessionStorage) FlushKeySpend(_ context.Context, _ int64, _ float64) error {
	return nil
}
func (m *mockSessionStorage) GetSpendSummary(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.SpendRow, error) {
	return nil, nil
}
func (m *mockSessionStorage) GetModelSpend(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.ModelSpendRow, error) { return nil, nil }
func (m *mockSessionStorage) GetDailySpend(_ context.Context, _, _ time.Time, _ storage.SpendFilters) ([]storage.DailySpendRow, error) { return nil, nil }

// newTestSessionManager creates an SCS SessionManager with a no-op store for tests.
func newTestSessionManager() *scs.SessionManager {
	sm := scs.New()
	// Use in-memory store from scs itself (no-op for our tests)
	return sm
}

// TestRequireSession verifies that a request with no session cookie results in a 401
// response with a JSON error body when Accept is application/json.
func TestRequireSession(t *testing.T) {
	sm := newTestSessionManager()
	store := &mockSessionStorage{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with RequireSession and the SCS LoadAndSave middleware.
	protected := sm.LoadAndSave(RequireSession(store, sm)(handler))

	req := httptest.NewRequest("GET", "/admin/status", nil)
	req.Header.Set("Accept", "application/json")

	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	// Body must be JSON with an error field.
	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if _, ok := body["error"]; !ok {
		t.Errorf("expected JSON body with 'error' field, got: %v", body)
	}
}

// TestRequireSessionMissing verifies the two response modes when no session exists:
//   - Accept: text/html (browser nav)  → 302 redirect to /login
//   - Accept: application/json (API)   → 401 JSON
func TestRequireSessionMissing(t *testing.T) {
	sm := newTestSessionManager()
	store := &mockSessionStorage{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	protected := sm.LoadAndSave(RequireSession(store, sm)(handler))

	tests := []struct {
		name           string
		acceptHeader   string
		wantStatus     int
		wantRedirect   bool
	}{
		{
			name:         "browser nav gets redirect",
			acceptHeader: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			wantStatus:   http.StatusSeeOther,
			wantRedirect: true,
		},
		{
			name:         "API caller gets 401 JSON",
			acceptHeader: "application/json",
			wantStatus:   http.StatusUnauthorized,
			wantRedirect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/admin/status", nil)
			req.Header.Set("Accept", tt.acceptHeader)

			rr := httptest.NewRecorder()
			protected.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}

			if tt.wantRedirect {
				location := rr.Header().Get("Location")
				if location != "/login" {
					t.Errorf("expected redirect to /login, got: %q", location)
				}
			} else {
				var body map[string]interface{}
				if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
					t.Fatalf("response body is not valid JSON: %v", err)
				}
				if _, ok := body["error"]; !ok {
					t.Errorf("expected JSON body with 'error' field, got: %v", body)
				}
			}
		})
	}
}
