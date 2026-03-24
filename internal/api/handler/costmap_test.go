package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
)

const handlerTestJSON = `{"gpt-4":{"max_tokens":8192,"input_cost_per_token":0.00003,"output_cost_per_token":0.00006,"litellm_provider":"openai","mode":"chat"}}`

func newCostMapTestServer(statusCode int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		w.Write([]byte(body)) //nolint:errcheck
	}))
}

func loadManager(t *testing.T, srv *httptest.Server) *costmap.Manager {
	t.Helper()
	m := costmap.New()
	if err := m.SetURL(srv.URL); err != nil {
		t.Fatalf("SetURL: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := m.Load(req.Context()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return m
}

func TestAdminCostMapStatus_NotLoaded(t *testing.T) {
	m := costmap.New()
	handler := AdminCostMapStatus(m)

	req := httptest.NewRequest(http.MethodGet, "/admin/costmap", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp costmap.Status
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.Loaded {
		t.Error("expected Loaded=false")
	}
	if resp.ModelCount != 0 {
		t.Errorf("expected ModelCount=0, got %d", resp.ModelCount)
	}
}

func TestAdminCostMapStatus_Loaded(t *testing.T) {
	srv := newCostMapTestServer(http.StatusOK, handlerTestJSON)
	defer srv.Close()
	m := loadManager(t, srv)

	handler := AdminCostMapStatus(m)
	req := httptest.NewRequest(http.MethodGet, "/admin/costmap", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp costmap.Status
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if !resp.Loaded {
		t.Error("expected Loaded=true")
	}
	if resp.ModelCount != 1 {
		t.Errorf("expected ModelCount=1, got %d", resp.ModelCount)
	}
}

func TestAdminCostMapReload_Success(t *testing.T) {
	srv := newCostMapTestServer(http.StatusOK, handlerTestJSON)
	defer srv.Close()

	m := costmap.New()
	if err := m.SetURL(srv.URL); err != nil {
		t.Fatalf("SetURL: %v", err)
	}

	handler := AdminCostMapReload(m)
	req := httptest.NewRequest(http.MethodPost, "/admin/costmap/reload", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp costmapReloadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status=ok, got %q", resp.Status)
	}
	if resp.ModelCount != 1 {
		t.Errorf("expected ModelCount=1, got %d", resp.ModelCount)
	}
}

func TestAdminCostMapReload_NetworkError(t *testing.T) {
	m := costmap.New()
	if err := m.SetURL("http://127.0.0.1:1"); err != nil {
		t.Fatalf("SetURL: %v", err)
	}

	handler := AdminCostMapReload(m)
	req := httptest.NewRequest(http.MethodPost, "/admin/costmap/reload", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestAdminCostMapSetURL_Valid(t *testing.T) {
	m := costmap.New()
	handler := AdminCostMapSetURL(m)

	body := `{"url":"https://example.com/models.json"}`
	req := httptest.NewRequest(http.MethodPut, "/admin/costmap/url", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp costmapURLResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.URL != "https://example.com/models.json" {
		t.Errorf("unexpected URL in response: %q", resp.URL)
	}
	if m.GetURL() != "https://example.com/models.json" {
		t.Error("Manager URL was not updated")
	}
}

func TestAdminCostMapSetURL_MalformedBody(t *testing.T) {
	m := costmap.New()
	handler := AdminCostMapSetURL(m)

	req := httptest.NewRequest(http.MethodPut, "/admin/costmap/url", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAdminCostMapSetURL_EmptyURL(t *testing.T) {
	m := costmap.New()
	handler := AdminCostMapSetURL(m)

	req := httptest.NewRequest(http.MethodPut, "/admin/costmap/url", bytes.NewBufferString(`{"url":""}`))
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAdminCostMapSetURL_InvalidScheme(t *testing.T) {
	m := costmap.New()
	handler := AdminCostMapSetURL(m)

	req := httptest.NewRequest(http.MethodPut, "/admin/costmap/url", bytes.NewBufferString(`{"url":"ftp://example.com/models.json"}`))
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
