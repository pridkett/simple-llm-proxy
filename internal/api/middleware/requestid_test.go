package middleware

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// uuidRegex matches a standard UUID v4 string.
var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestRequestID_GeneratesUUID(t *testing.T) {
	var gotID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	RequestID()(handler).ServeHTTP(rr, req)

	if gotID == "" {
		t.Fatal("expected request ID in context, got empty string")
	}
	if !uuidRegex.MatchString(gotID) {
		t.Errorf("expected UUID v4 format, got %q", gotID)
	}

	// Response header must match the context value.
	respHeader := rr.Header().Get("X-Request-ID")
	if respHeader != gotID {
		t.Errorf("response header X-Request-ID = %q, want %q", respHeader, gotID)
	}
}

func TestRequestID_PassThroughExisting(t *testing.T) {
	const existingID = "client-supplied-id-123"

	var gotID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", existingID)
	rr := httptest.NewRecorder()

	RequestID()(handler).ServeHTTP(rr, req)

	if gotID != existingID {
		t.Errorf("expected pass-through ID %q, got %q", existingID, gotID)
	}

	respHeader := rr.Header().Get("X-Request-ID")
	if respHeader != existingID {
		t.Errorf("response header X-Request-ID = %q, want %q", respHeader, existingID)
	}
}

func TestRequestID_SetsResponseHeader(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	RequestID()(handler).ServeHTTP(rr, req)

	respHeader := rr.Header().Get("X-Request-ID")
	if respHeader == "" {
		t.Fatal("expected X-Request-ID response header, got empty")
	}
	if !uuidRegex.MatchString(respHeader) {
		t.Errorf("expected UUID v4 format in response header, got %q", respHeader)
	}
}

func TestRequestID_UniquePerRequest(t *testing.T) {
	ids := make(map[string]bool)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := RequestIDFromContext(r.Context())
		if ids[id] {
			t.Errorf("duplicate request ID: %s", id)
		}
		ids[id] = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequestID()(handler)
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		mw.ServeHTTP(rr, req)
	}

	if len(ids) != 100 {
		t.Errorf("expected 100 unique IDs, got %d", len(ids))
	}
}

func TestRequestIDFromContext_Empty(t *testing.T) {
	// When no middleware has run, RequestIDFromContext should return "".
	id := RequestIDFromContext(httptest.NewRequest("GET", "/", nil).Context())
	if id != "" {
		t.Errorf("expected empty string, got %q", id)
	}
}
