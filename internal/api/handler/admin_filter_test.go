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

// filterCaptureMock extends mockStorage to capture the LogsFilter passed to GetLogs.
// This allows TestAdminLogsFilterParsing to assert on the parsed filter values.
type filterCaptureMock struct {
	mockStorage
	capturedFilter storage.LogsFilter
	logByIDResult  *storage.RequestLog
	logByIDErr     error
	logsMetaResult *storage.LogsMeta
	logsMetaErr    error
}

func (m *filterCaptureMock) GetLogs(_ context.Context, _, _ int, f storage.LogsFilter) ([]*storage.RequestLog, int, error) {
	m.capturedFilter = f
	return nil, 0, nil
}

func (m *filterCaptureMock) GetLogByID(_ context.Context, _ string) (*storage.RequestLog, error) {
	return m.logByIDResult, m.logByIDErr
}

func (m *filterCaptureMock) GetLogsMeta(_ context.Context) (*storage.LogsMeta, error) {
	return m.logsMetaResult, m.logsMetaErr
}

// TestAdminLogsFilterParsing verifies AdminLogs parses all five new filter query params.
func TestAdminLogsFilterParsing(t *testing.T) {
	mock := &filterCaptureMock{}
	handler := AdminLogs(mock)

	req := httptest.NewRequest(http.MethodGet,
		"/admin/logs?provider=openai&pool_name=fast-pool&key_id=5&date_from=2026-04-01T00:00:00Z&date_to=2026-06-01T00:00:00Z",
		nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	f := mock.capturedFilter
	if f.Provider != "openai" {
		t.Errorf("Provider: got %q, want openai", f.Provider)
	}
	if f.PoolName != "fast-pool" {
		t.Errorf("PoolName: got %q, want fast-pool", f.PoolName)
	}
	if f.KeyID == nil || *f.KeyID != 5 {
		t.Errorf("KeyID: got %v, want &5", f.KeyID)
	}
	if f.DateFrom == nil {
		t.Error("DateFrom: got nil, want parsed time")
	}
	if f.DateTo == nil {
		t.Error("DateTo: got nil, want parsed time")
	}
}

// TestAdminLogDetail_Found verifies 200 + logDetailEntry for a known request_id.
func TestAdminLogDetail_Found(t *testing.T) {
	reqTime := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	mock := &filterCaptureMock{
		logByIDResult: &storage.RequestLog{
			RequestID:   "req-123",
			Model:       "gpt-4",
			Provider:    "openai",
			Endpoint:    "/v1/chat/completions",
			StatusCode:  200,
			LatencyMS:   150,
			RequestTime: reqTime,
			IsStreaming: false,
		},
	}
	h := AdminLogDetail(mock)

	req := httptest.NewRequest(http.MethodGet, "/admin/logs/req-123", nil)
	req = withChiParam(req, map[string]string{"requestID": "req-123"})
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["request_id"] != "req-123" {
		t.Errorf("request_id: got %v, want req-123", resp["request_id"])
	}
}

// TestAdminLogDetail_NotFound verifies 404 when GetLogByID returns nil, nil.
func TestAdminLogDetail_NotFound(t *testing.T) {
	mock := &filterCaptureMock{logByIDResult: nil}
	h := AdminLogDetail(mock)

	req := httptest.NewRequest(http.MethodGet, "/admin/logs/no-such-id", nil)
	req = withChiParam(req, map[string]string{"requestID": "no-such-id"})
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestAdminLogsMeta verifies 200 + correct JSON shape.
func TestAdminLogsMeta(t *testing.T) {
	mock := &filterCaptureMock{
		logsMetaResult: &storage.LogsMeta{
			Teams:     []storage.LogsMetaIDName{{ID: 1, Name: "Team A"}},
			Apps:      []storage.LogsMetaIDName{{ID: 5, Name: "App B"}},
			Keys:      []storage.LogsMetaIDName{{ID: 10, Name: "My Key"}},
			Providers: []string{"openai", "anthropic"},
			Models:    []string{"gpt-4"},
			Pools:     []string{"fast-pool"},
		},
	}
	h := AdminLogsMeta(mock)

	req := httptest.NewRequest(http.MethodGet, "/admin/logs/meta", nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp storage.LogsMeta
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Teams) != 1 || resp.Teams[0].ID != 1 || resp.Teams[0].Name != "Team A" {
		t.Errorf("Teams: got %+v, want [{1 Team A}]", resp.Teams)
	}
	if len(resp.Providers) != 2 {
		t.Errorf("Providers: got %d items, want 2", len(resp.Providers))
	}
}
