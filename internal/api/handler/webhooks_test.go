package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// mockWebhookStore implements the subset of storage.Storage used by webhook handlers.
type mockWebhookStore struct {
	storage.Storage // embed to satisfy interface; only override needed methods

	webhooks []*storage.WebhookSubscription
	events   []*storage.NotificationEvent
	totalEvt int
	createID int64

	lastCreated *storage.WebhookSubscription
	lastUpdated *storage.WebhookSubscription
	lastDeleted int64
	createErr   error
	updateErr   error
	deleteErr   error
	listErr     error
	eventsErr   error
}

func (m *mockWebhookStore) ListWebhookSubscriptions(ctx context.Context) ([]*storage.WebhookSubscription, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.webhooks, nil
}

func (m *mockWebhookStore) CreateWebhookSubscription(ctx context.Context, sub *storage.WebhookSubscription) (*storage.WebhookSubscription, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.lastCreated = sub
	sub.ID = m.createID
	sub.CreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return sub, nil
}

func (m *mockWebhookStore) UpdateWebhookSubscription(ctx context.Context, sub *storage.WebhookSubscription) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.lastUpdated = sub
	return nil
}

func (m *mockWebhookStore) DeleteWebhookSubscription(ctx context.Context, id int64) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.lastDeleted = id
	return nil
}

func (m *mockWebhookStore) ListNotificationEvents(ctx context.Context, limit, offset int, eventType string) ([]*storage.NotificationEvent, int, error) {
	if m.eventsErr != nil {
		return nil, 0, m.eventsErr
	}
	return m.events, m.totalEvt, nil
}

func testConfig() *config.Config {
	return &config.Config{
		Webhooks: []config.WebhookConfig{
			{
				URL:     "https://example.com/hook1",
				Events:  []string{"provider_failover", "budget_exhausted"},
				Secret:  "super-secret-value",
				Enabled: true,
			},
			{
				URL:     "https://example.com/hook2",
				Events:  []string{"cooldown_enter"},
				Secret:  "another-secret",
				Enabled: false,
			},
		},
	}
}

// --- AdminListWebhooks tests ---

func TestAdminListWebhooks_MergedResponse(t *testing.T) {
	store := &mockWebhookStore{
		webhooks: []*storage.WebhookSubscription{
			{
				ID:        10,
				URL:       "https://db-hook.example.com",
				Events:    []string{"budget_exhausted"},
				Secret:    "db-secret",
				Enabled:   true,
				CreatedAt: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	getCfg := func() *config.Config { return testConfig() }

	handler := AdminListWebhooks(store, getCfg)
	req := httptest.NewRequest(http.MethodGet, "/admin/webhooks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp adminWebhooksListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// 2 YAML + 1 DB = 3 total
	if len(resp.Webhooks) != 3 {
		t.Fatalf("expected 3 webhooks, got %d", len(resp.Webhooks))
	}

	// YAML webhooks first with synthetic negative IDs
	yaml1 := resp.Webhooks[0]
	if yaml1.ID != -1 {
		t.Errorf("yaml1 ID: want -1, got %d", yaml1.ID)
	}
	if yaml1.Source != "yaml" {
		t.Errorf("yaml1 Source: want yaml, got %s", yaml1.Source)
	}
	if !yaml1.ReadOnly {
		t.Error("yaml1 ReadOnly: want true")
	}
	if yaml1.URL != "https://example.com/hook1" {
		t.Errorf("yaml1 URL: want https://example.com/hook1, got %s", yaml1.URL)
	}

	yaml2 := resp.Webhooks[1]
	if yaml2.ID != -2 {
		t.Errorf("yaml2 ID: want -2, got %d", yaml2.ID)
	}
	if yaml2.Source != "yaml" {
		t.Errorf("yaml2 Source: want yaml, got %s", yaml2.Source)
	}
	if !yaml2.ReadOnly {
		t.Error("yaml2 ReadOnly: want true")
	}

	// DB webhook last
	db1 := resp.Webhooks[2]
	if db1.ID != 10 {
		t.Errorf("db1 ID: want 10, got %d", db1.ID)
	}
	if db1.Source != "ui" {
		t.Errorf("db1 Source: want ui, got %s", db1.Source)
	}
	if db1.ReadOnly {
		t.Error("db1 ReadOnly: want false")
	}
}

func TestAdminListWebhooks_NoSecretInResponse(t *testing.T) {
	store := &mockWebhookStore{
		webhooks: []*storage.WebhookSubscription{
			{
				ID:      1,
				URL:     "https://db-hook.example.com",
				Events:  []string{"budget_exhausted"},
				Secret:  "should-not-appear",
				Enabled: true,
			},
		},
	}
	getCfg := func() *config.Config { return testConfig() }

	handler := AdminListWebhooks(store, getCfg)
	req := httptest.NewRequest(http.MethodGet, "/admin/webhooks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if bytes.Contains([]byte(body), []byte("super-secret-value")) {
		t.Error("YAML secret leaked in response")
	}
	if bytes.Contains([]byte(body), []byte("another-secret")) {
		t.Error("YAML secret leaked in response")
	}
	if bytes.Contains([]byte(body), []byte("should-not-appear")) {
		t.Error("DB secret leaked in response")
	}
	// Also check there's no "secret" key in the JSON
	if bytes.Contains([]byte(body), []byte(`"secret"`)) {
		t.Error("secret field present in JSON response")
	}
}

// --- AdminCreateWebhook tests ---

func TestAdminCreateWebhook_Success(t *testing.T) {
	store := &mockWebhookStore{createID: 42}

	handler := AdminCreateWebhook(store)
	body := `{"url":"https://new-hook.example.com","events":["budget_exhausted"],"secret":"new-secret"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/webhooks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp webhookResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != 42 {
		t.Errorf("ID: want 42, got %d", resp.ID)
	}
	if resp.Source != "ui" {
		t.Errorf("Source: want ui, got %s", resp.Source)
	}
	if resp.ReadOnly {
		t.Error("ReadOnly: want false")
	}
	if resp.URL != "https://new-hook.example.com" {
		t.Errorf("URL: want https://new-hook.example.com, got %s", resp.URL)
	}
	if !resp.Enabled {
		t.Error("Enabled should default to true")
	}
}

func TestAdminCreateWebhook_MissingURL(t *testing.T) {
	store := &mockWebhookStore{}
	handler := AdminCreateWebhook(store)
	body := `{"events":["budget_exhausted"]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/webhooks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminCreateWebhook_EmptyEvents(t *testing.T) {
	store := &mockWebhookStore{}
	handler := AdminCreateWebhook(store)
	body := `{"url":"https://example.com","events":[]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/webhooks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- AdminUpdateWebhook tests ---

func TestAdminUpdateWebhook_Success(t *testing.T) {
	store := &mockWebhookStore{}

	r := chi.NewRouter()
	r.Put("/admin/webhooks/{id}", AdminUpdateWebhook(store))

	body := `{"url":"https://updated.example.com","events":["cooldown_enter"],"enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/admin/webhooks/5", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if store.lastUpdated == nil {
		t.Fatal("store.lastUpdated should not be nil")
	}
	if store.lastUpdated.ID != 5 {
		t.Errorf("ID: want 5, got %d", store.lastUpdated.ID)
	}
	if store.lastUpdated.URL != "https://updated.example.com" {
		t.Errorf("URL: want https://updated.example.com, got %s", store.lastUpdated.URL)
	}
}

func TestAdminUpdateWebhook_YAMLProtected(t *testing.T) {
	store := &mockWebhookStore{}

	r := chi.NewRouter()
	r.Put("/admin/webhooks/{id}", AdminUpdateWebhook(store))

	body := `{"url":"https://updated.example.com","events":["cooldown_enter"]}`
	req := httptest.NewRequest(http.MethodPut, "/admin/webhooks/-1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for YAML webhook update, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- AdminDeleteWebhook tests ---

func TestAdminDeleteWebhook_Success(t *testing.T) {
	store := &mockWebhookStore{}

	r := chi.NewRouter()
	r.Delete("/admin/webhooks/{id}", AdminDeleteWebhook(store))

	req := httptest.NewRequest(http.MethodDelete, "/admin/webhooks/7", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if store.lastDeleted != 7 {
		t.Errorf("lastDeleted: want 7, got %d", store.lastDeleted)
	}
}

func TestAdminDeleteWebhook_YAMLProtected(t *testing.T) {
	store := &mockWebhookStore{}

	r := chi.NewRouter()
	r.Delete("/admin/webhooks/{id}", AdminDeleteWebhook(store))

	req := httptest.NewRequest(http.MethodDelete, "/admin/webhooks/-2", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for YAML webhook delete, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- AdminEvents tests ---

func TestAdminEvents_Default(t *testing.T) {
	store := &mockWebhookStore{
		events: []*storage.NotificationEvent{
			{
				ID:        1,
				EventType: "provider_failover",
				Payload:   `{"model":"gpt-4","from":"openai-1","to":"openai-2"}`,
				CreatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			},
		},
		totalEvt: 1,
	}

	handler := AdminEvents(store)
	req := httptest.NewRequest(http.MethodGet, "/admin/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp adminEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(resp.Events))
	}
	if resp.Total != 1 {
		t.Errorf("Total: want 1, got %d", resp.Total)
	}
	if resp.Limit != 50 {
		t.Errorf("Limit: want 50 (default), got %d", resp.Limit)
	}
	if resp.Offset != 0 {
		t.Errorf("Offset: want 0, got %d", resp.Offset)
	}
	if resp.Events[0].EventType != "provider_failover" {
		t.Errorf("EventType: want provider_failover, got %s", resp.Events[0].EventType)
	}
	// Payload should be parsed JSON, not a string
	if resp.Events[0].Payload == nil {
		t.Error("Payload should be parsed from JSON, not nil")
	}
}

func TestAdminEvents_FilterByEventType(t *testing.T) {
	store := &mockWebhookStore{
		events:   []*storage.NotificationEvent{},
		totalEvt: 0,
	}

	handler := AdminEvents(store)
	req := httptest.NewRequest(http.MethodGet, "/admin/events?event_type=provider_failover", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp adminEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Just verifying the request doesn't error — filter is passed through to storage
	if resp.Limit != 50 {
		t.Errorf("Limit: want 50, got %d", resp.Limit)
	}
}

func TestAdminEvents_Pagination(t *testing.T) {
	store := &mockWebhookStore{
		events:   []*storage.NotificationEvent{},
		totalEvt: 100,
	}

	handler := AdminEvents(store)
	req := httptest.NewRequest(http.MethodGet, "/admin/events?limit=10&offset=20", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp adminEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Limit != 10 {
		t.Errorf("Limit: want 10, got %d", resp.Limit)
	}
	if resp.Offset != 20 {
		t.Errorf("Offset: want 20, got %d", resp.Offset)
	}
	if resp.Total != 100 {
		t.Errorf("Total: want 100, got %d", resp.Total)
	}
}

func TestAdminCreateWebhook_DefaultEnabled(t *testing.T) {
	store := &mockWebhookStore{createID: 1}

	handler := AdminCreateWebhook(store)
	// No "enabled" field in body — should default to true
	body := `{"url":"https://example.com/hook","events":["budget_exhausted"]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/webhooks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	if store.lastCreated == nil {
		t.Fatal("lastCreated should not be nil")
	}
	if !store.lastCreated.Enabled {
		t.Error("Enabled should default to true when not provided")
	}
}

func TestAdminListWebhooks_StorageError(t *testing.T) {
	store := &mockWebhookStore{listErr: fmt.Errorf("db down")}
	getCfg := func() *config.Config { return testConfig() }

	handler := AdminListWebhooks(store, getCfg)
	req := httptest.NewRequest(http.MethodGet, "/admin/webhooks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}
