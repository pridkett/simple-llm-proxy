package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// --- TestApplications ---

func TestApplications(t *testing.T) {
	t.Run("TestAdminListApplications", func(t *testing.T) {
		store := &mockIdentityStorage{
			applications: []*storage.Application{
				{ID: 1, TeamID: 1, Name: "App One", CreatedAt: time.Now()},
				{ID: 2, TeamID: 1, Name: "App Two", CreatedAt: time.Now()},
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/admin/applications?team_id=1", nil)
		req = withChiParamAndUser(req, adminUser(), nil)
		w := httptest.NewRecorder()
		AdminApplications(store)(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var apps []*storage.Application
		if err := json.Unmarshal(w.Body.Bytes(), &apps); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(apps) != 2 {
			t.Errorf("expected 2 applications, got %d", len(apps))
		}
		if store.lastListAppsTeamID != 1 {
			t.Errorf("expected ListApplications teamID=1, got %d", store.lastListAppsTeamID)
		}
	})

	t.Run("TestAdminCreateApplication admin creates app", func(t *testing.T) {
		store := &mockIdentityStorage{
			createApplicationResult: &storage.Application{ID: 5, TeamID: 1, Name: "MyApp", CreatedAt: time.Now()},
		}
		body := bytes.NewBufferString(`{"team_id":1,"name":"MyApp"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/applications", body)
		req = withChiParamAndUser(req, adminUser(), nil)
		w := httptest.NewRecorder()
		AdminCreateApplication(store)(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var app storage.Application
		if err := json.Unmarshal(w.Body.Bytes(), &app); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if app.ID != 5 {
			t.Errorf("expected id=5, got %d", app.ID)
		}
		if store.lastCreateAppTeamID != 1 {
			t.Errorf("expected CreateApplication teamID=1, got %d", store.lastCreateAppTeamID)
		}
		if store.lastCreateAppName != "MyApp" {
			t.Errorf("expected CreateApplication name=MyApp, got %q", store.lastCreateAppName)
		}
	})

	t.Run("TestAdminCreateApplication non-admin gets 403", func(t *testing.T) {
		store := &mockIdentityStorage{}
		body := bytes.NewBufferString(`{"team_id":1,"name":"MyApp"}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/applications", body)
		req = withChiParamAndUser(req, regularUser(), nil)
		w := httptest.NewRecorder()
		AdminCreateApplication(store)(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})

	t.Run("TestAdminDeleteApplication admin deletes app", func(t *testing.T) {
		store := &mockIdentityStorage{}
		req := httptest.NewRequest(http.MethodDelete, "/admin/applications/1", nil)
		req = withChiParamAndUser(req, adminUser(), map[string]string{"id": "1"})
		w := httptest.NewRecorder()
		AdminDeleteApplication(store)(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
		}
		if store.lastDeleteAppID != 1 {
			t.Errorf("expected DeleteApplication id=1, got %d", store.lastDeleteAppID)
		}
	})

	t.Run("TestAdminDeleteApplication non-admin gets 403", func(t *testing.T) {
		store := &mockIdentityStorage{}
		req := httptest.NewRequest(http.MethodDelete, "/admin/applications/1", nil)
		req = withChiParamAndUser(req, regularUser(), map[string]string{"id": "1"})
		w := httptest.NewRecorder()
		AdminDeleteApplication(store)(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})
}
