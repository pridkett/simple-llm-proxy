package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// withChiParam sets chi URL params on a request.
func withChiParam(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// withChiParamAndUser sets chi URL params and injects user into context.
func withChiParamAndUser(req *http.Request, user *storage.User, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if user != nil {
		ctx = context.WithValue(ctx, middleware.ContextKeyUser, user)
	}
	return req.WithContext(ctx)
}

// --- TestTeams ---

func TestTeams(t *testing.T) {
	t.Run("TestAdminListTeams", func(t *testing.T) {
		store := &mockIdentityStorage{
			teams: []*storage.Team{
				{ID: 1, Name: "Engineering", CreatedAt: time.Now()},
				{ID: 2, Name: "Design", CreatedAt: time.Now()},
			},
		}
		req := newRequestWithUser(http.MethodGet, "/admin/teams", adminUser())
		w := httptest.NewRecorder()
		AdminTeams(store)(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var teams []*storage.Team
		if err := json.Unmarshal(w.Body.Bytes(), &teams); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(teams) != 2 {
			t.Errorf("expected 2 teams, got %d", len(teams))
		}
	})

	t.Run("TestAdminCreateTeam admin creates team", func(t *testing.T) {
		store := &mockIdentityStorage{
			createTeamResult: &storage.Team{ID: 1, Name: "Engineering", CreatedAt: time.Now()},
		}
		body := bytes.NewBufferString(`{"name":"Engineering"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/teams", body)
		req = withChiParamAndUser(req, adminUser(), nil)
		w := httptest.NewRecorder()
		AdminCreateTeam(store)(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var team storage.Team
		if err := json.Unmarshal(w.Body.Bytes(), &team); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if team.ID != 1 {
			t.Errorf("expected id=1, got %d", team.ID)
		}
		if store.lastCreateTeamName != "Engineering" {
			t.Errorf("expected name=Engineering, got %q", store.lastCreateTeamName)
		}
	})

	t.Run("TestAdminCreateTeam non-admin gets 403", func(t *testing.T) {
		store := &mockIdentityStorage{}
		body := bytes.NewBufferString(`{"name":"Engineering"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/teams", body)
		req = withChiParamAndUser(req, regularUser(), nil)
		w := httptest.NewRecorder()
		AdminCreateTeam(store)(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})

	t.Run("TestAdminDeleteTeam admin deletes team", func(t *testing.T) {
		store := &mockIdentityStorage{}
		req := httptest.NewRequest(http.MethodDelete, "/admin/teams/1", nil)
		req = withChiParamAndUser(req, adminUser(), map[string]string{"id": "1"})
		w := httptest.NewRecorder()
		AdminDeleteTeam(store)(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
		}
		if store.lastDeleteTeamID != 1 {
			t.Errorf("expected DeleteTeam called with id=1, got %d", store.lastDeleteTeamID)
		}
	})

	t.Run("TestAdminDeleteTeam non-admin gets 403", func(t *testing.T) {
		store := &mockIdentityStorage{}
		req := httptest.NewRequest(http.MethodDelete, "/admin/teams/1", nil)
		req = withChiParamAndUser(req, regularUser(), map[string]string{"id": "1"})
		w := httptest.NewRecorder()
		AdminDeleteTeam(store)(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})
}

// --- TestTeamMembers ---

func TestTeamMembers(t *testing.T) {
	t.Run("TestAdminListTeamMembers", func(t *testing.T) {
		store := &mockIdentityStorage{
			teamMembers: []*storage.TeamMember{
				{TeamID: 1, UserID: "sub1", Role: "admin", UserEmail: "a@example.com", UserName: "Alice"},
				{TeamID: 1, UserID: "sub2", Role: "member", UserEmail: "b@example.com", UserName: "Bob"},
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/admin/teams/1/members", nil)
		req = withChiParamAndUser(req, adminUser(), map[string]string{"id": "1"})
		w := httptest.NewRecorder()
		AdminTeamMembers(store)(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var members []*storage.TeamMember
		if err := json.Unmarshal(w.Body.Bytes(), &members); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(members) != 2 {
			t.Errorf("expected 2 members, got %d", len(members))
		}
	})

	t.Run("TestAdminAddTeamMember", func(t *testing.T) {
		store := &mockIdentityStorage{}
		body := bytes.NewBufferString(`{"user_id":"sub123","role":"member"}`)
		req := httptest.NewRequest(http.MethodPut, "/admin/teams/1/members", body)
		req = withChiParamAndUser(req, adminUser(), map[string]string{"id": "1"})
		w := httptest.NewRecorder()
		AdminAddTeamMember(store)(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
		}
		if store.lastAddMemberTeamID != 1 {
			t.Errorf("expected AddTeamMember teamID=1, got %d", store.lastAddMemberTeamID)
		}
		if store.lastAddMemberUserID != "sub123" {
			t.Errorf("expected AddTeamMember userID=sub123, got %q", store.lastAddMemberUserID)
		}
		if store.lastAddMemberRole != "member" {
			t.Errorf("expected AddTeamMember role=member, got %q", store.lastAddMemberRole)
		}
	})

	t.Run("TestAdminRemoveTeamMember", func(t *testing.T) {
		store := &mockIdentityStorage{}
		req := httptest.NewRequest(http.MethodDelete, "/admin/teams/1/members/sub123", nil)
		req = withChiParamAndUser(req, adminUser(), map[string]string{"id": "1", "user_id": "sub123"})
		w := httptest.NewRecorder()
		AdminRemoveTeamMember(store)(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
		}
		if store.lastRemoveMemberTeamID != 1 {
			t.Errorf("expected RemoveTeamMember teamID=1, got %d", store.lastRemoveMemberTeamID)
		}
		if store.lastRemoveMemberUserID != "sub123" {
			t.Errorf("expected RemoveTeamMember userID=sub123, got %q", store.lastRemoveMemberUserID)
		}
	})

	t.Run("TestAdminUpdateTeamMemberRole", func(t *testing.T) {
		store := &mockIdentityStorage{}
		body := bytes.NewBufferString(`{"role":"admin"}`)
		req := httptest.NewRequest(http.MethodPatch, "/admin/teams/1/members/sub123", body)
		req = withChiParamAndUser(req, adminUser(), map[string]string{"id": "1", "user_id": "sub123"})
		w := httptest.NewRecorder()
		AdminUpdateTeamMemberRole(store)(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
		}
		if store.lastUpdateRoleTeamID != 1 {
			t.Errorf("expected UpdateTeamMemberRole teamID=1, got %d", store.lastUpdateRoleTeamID)
		}
		if store.lastUpdateRoleUserID != "sub123" {
			t.Errorf("expected UpdateTeamMemberRole userID=sub123, got %q", store.lastUpdateRoleUserID)
		}
		if store.lastUpdateRole != "admin" {
			t.Errorf("expected UpdateTeamMemberRole role=admin, got %q", store.lastUpdateRole)
		}
	})

	t.Run("TestAdminMyTeams", func(t *testing.T) {
		store := &mockIdentityStorage{
			myTeams: []*storage.TeamMember{
				{TeamID: 1, UserID: "sub-user", Role: "member", TeamName: "Engineering"},
			},
		}
		req := newRequestWithUser(http.MethodGet, "/admin/teams/mine", regularUser())
		w := httptest.NewRecorder()
		AdminMyTeams(store)(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var myTeams []*storage.TeamMember
		if err := json.Unmarshal(w.Body.Bytes(), &myTeams); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(myTeams) != 1 {
			t.Errorf("expected 1 team, got %d", len(myTeams))
		}
		if store.lastListMyTeamsUserID != "sub-user" {
			t.Errorf("expected ListMyTeams called with sub-user, got %q", store.lastListMyTeamsUserID)
		}
	})
}

// --- TestTeamsMine ---

func TestTeamsMine(t *testing.T) {
	t.Run("non-admin user gets their own teams", func(t *testing.T) {
		store := &mockIdentityStorage{
			myTeams: []*storage.TeamMember{
				{TeamID: 1, UserID: "sub-user", Role: "viewer", TeamName: "Design"},
				{TeamID: 2, UserID: "sub-user", Role: "member", TeamName: "Marketing"},
			},
		}
		req := newRequestWithUser(http.MethodGet, "/admin/teams/mine", regularUser())
		w := httptest.NewRecorder()
		AdminMyTeams(store)(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var myTeams []*storage.TeamMember
		if err := json.Unmarshal(w.Body.Bytes(), &myTeams); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(myTeams) != 2 {
			t.Errorf("expected 2 teams, got %d", len(myTeams))
		}
	})
}
