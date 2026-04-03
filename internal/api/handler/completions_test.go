package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompletions_Returns410Gone(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/completions", nil)
	rr := httptest.NewRecorder()

	Completions()(rr, req)

	if rr.Code != http.StatusGone {
		t.Errorf("expected status %d, got %d", http.StatusGone, rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	var resp deprecatedEndpointError
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if resp.Error.Type != "deprecated_endpoint" {
		t.Errorf("expected error type %q, got %q", "deprecated_endpoint", resp.Error.Type)
	}

	if resp.Error.Code != http.StatusGone {
		t.Errorf("expected error code %d, got %d", http.StatusGone, resp.Error.Code)
	}

	expectedMsg := "POST /v1/completions is deprecated. Use POST /v1/chat/completions instead."
	if resp.Error.Message != expectedMsg {
		t.Errorf("expected error message %q, got %q", expectedMsg, resp.Error.Message)
	}
}
