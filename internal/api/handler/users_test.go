package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// mockIdentityStorage is a mock that extends mockStorage with controllable identity behavior.
type mockIdentityStorage struct {
	mockStorage

	// Users
	users    []*storage.User
	listUsersErr error

	// Teams
	teams         []*storage.Team
	listTeamsErr  error
	createTeamResult *storage.Team
	createTeamErr error
	deleteTeamErr error

	// Team members
	teamMembers        []*storage.TeamMember
	listTeamMembersErr error
	addTeamMemberErr   error
	removeTeamMemberErr error
	updateTeamMemberRoleErr error

	myTeams       []*storage.TeamMember
	listMyTeamsErr error

	// Applications
	applications      []*storage.Application
	listApplicationsErr error
	createApplicationResult *storage.Application
	createApplicationErr error
	deleteApplicationErr error

	// Capture args
	lastCreateTeamName    string
	lastDeleteTeamID      int64
	lastAddMemberTeamID   int64
	lastAddMemberUserID   string
	lastAddMemberRole     string
	lastRemoveMemberTeamID int64
	lastRemoveMemberUserID string
	lastUpdateRoleTeamID  int64
	lastUpdateRoleUserID  string
	lastUpdateRole        string
	lastListMembersTeamID int64
	lastListMyTeamsUserID string
	lastListAppsTeamID    int64
	lastCreateAppTeamID   int64
	lastCreateAppName     string
	lastDeleteAppID       int64
}

func (m *mockIdentityStorage) ListUsers(_ context.Context) ([]*storage.User, error) {
	return m.users, m.listUsersErr
}

func (m *mockIdentityStorage) CreateTeam(_ context.Context, name string) (*storage.Team, error) {
	m.lastCreateTeamName = name
	return m.createTeamResult, m.createTeamErr
}

func (m *mockIdentityStorage) DeleteTeam(_ context.Context, id int64) error {
	m.lastDeleteTeamID = id
	return m.deleteTeamErr
}

func (m *mockIdentityStorage) ListTeams(_ context.Context) ([]*storage.Team, error) {
	return m.teams, m.listTeamsErr
}

func (m *mockIdentityStorage) AddTeamMember(_ context.Context, teamID int64, userID string, role string) error {
	m.lastAddMemberTeamID = teamID
	m.lastAddMemberUserID = userID
	m.lastAddMemberRole = role
	return m.addTeamMemberErr
}

func (m *mockIdentityStorage) RemoveTeamMember(_ context.Context, teamID int64, userID string) error {
	m.lastRemoveMemberTeamID = teamID
	m.lastRemoveMemberUserID = userID
	return m.removeTeamMemberErr
}

func (m *mockIdentityStorage) UpdateTeamMemberRole(_ context.Context, teamID int64, userID string, role string) error {
	m.lastUpdateRoleTeamID = teamID
	m.lastUpdateRoleUserID = userID
	m.lastUpdateRole = role
	return m.updateTeamMemberRoleErr
}

func (m *mockIdentityStorage) ListTeamMembers(_ context.Context, teamID int64) ([]*storage.TeamMember, error) {
	m.lastListMembersTeamID = teamID
	return m.teamMembers, m.listTeamMembersErr
}

func (m *mockIdentityStorage) ListMyTeams(_ context.Context, userID string) ([]*storage.TeamMember, error) {
	m.lastListMyTeamsUserID = userID
	return m.myTeams, m.listMyTeamsErr
}

func (m *mockIdentityStorage) CreateApplication(_ context.Context, teamID int64, name string) (*storage.Application, error) {
	m.lastCreateAppTeamID = teamID
	m.lastCreateAppName = name
	return m.createApplicationResult, m.createApplicationErr
}

func (m *mockIdentityStorage) DeleteApplication(_ context.Context, id int64) error {
	m.lastDeleteAppID = id
	return m.deleteApplicationErr
}

func (m *mockIdentityStorage) ListApplications(_ context.Context, teamID int64) ([]*storage.Application, error) {
	m.lastListAppsTeamID = teamID
	return m.applications, m.listApplicationsErr
}

// newRequestWithUser builds an *http.Request with the given user injected into context.
func newRequestWithUser(method, path string, user *storage.User) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if user != nil {
		ctx := context.WithValue(req.Context(), middleware.ContextKeyUser, user)
		req = req.WithContext(ctx)
	}
	return req
}

func adminUser() *storage.User {
	return &storage.User{ID: "sub-admin", Email: "admin@example.com", Name: "Admin", IsAdmin: true, CreatedAt: time.Now(), LastSeen: time.Now()}
}

func regularUser() *storage.User {
	return &storage.User{ID: "sub-user", Email: "user@example.com", Name: "User", IsAdmin: false, CreatedAt: time.Now(), LastSeen: time.Now()}
}

// --- TestAdminUsers ---

func TestAdminUsers(t *testing.T) {
	t.Run("admin gets user list", func(t *testing.T) {
		store := &mockIdentityStorage{
			users: []*storage.User{
				{ID: "sub1", Email: "a@example.com", Name: "Alice", IsAdmin: true},
				{ID: "sub2", Email: "b@example.com", Name: "Bob", IsAdmin: false},
			},
		}
		req := newRequestWithUser(http.MethodGet, "/admin/users", adminUser())
		w := httptest.NewRecorder()
		AdminUsers(store)(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var users []*storage.User
		if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(users) != 2 {
			t.Errorf("expected 2 users, got %d", len(users))
		}
	})

	t.Run("non-admin gets 403", func(t *testing.T) {
		store := &mockIdentityStorage{}
		req := newRequestWithUser(http.MethodGet, "/admin/users", regularUser())
		w := httptest.NewRecorder()
		AdminUsers(store)(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})

	t.Run("no user in context gets 403", func(t *testing.T) {
		store := &mockIdentityStorage{}
		req := newRequestWithUser(http.MethodGet, "/admin/users", nil)
		w := httptest.NewRecorder()
		AdminUsers(store)(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})
}
