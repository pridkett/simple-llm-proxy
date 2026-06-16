package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// trackingCloser is a test helper that records whether Close() was called.
// Used to verify bodyWithClose correctly delegates Close() to the original body.
type trackingCloser struct {
	io.Reader
	closed bool
}

func (tc *trackingCloser) Close() error {
	tc.closed = true
	return nil
}

// TestBodyCapture_LimitZero verifies that when limitFn returns 0, no value is
// stored in the context (ReqBodySnippetFromContext returns "") and the body is
// passed through unmodified so the handler can read it normally.
func TestBodyCapture_LimitZero(t *testing.T) {
	const bodyContent = "hello world"

	var gotSnippet string
	var gotBody string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSnippet = ReqBodySnippetFromContext(r.Context())
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(bodyContent))
	rr := httptest.NewRecorder()

	BodyCapture(func() int { return 0 })(handler).ServeHTTP(rr, req)

	if gotSnippet != "" {
		t.Errorf("expected empty snippet when limit=0, got %q", gotSnippet)
	}
	if gotBody != bodyContent {
		t.Errorf("expected body %q, got %q", bodyContent, gotBody)
	}
}

// TestBodyCapture_BodyUnderLimit verifies that a body shorter than the limit is
// captured in full as the snippet.
func TestBodyCapture_BodyUnderLimit(t *testing.T) {
	const bodyContent = "hello"

	var gotSnippet string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSnippet = ReqBodySnippetFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(bodyContent))
	rr := httptest.NewRecorder()

	BodyCapture(func() int { return 500 })(handler).ServeHTTP(rr, req)

	if gotSnippet != bodyContent {
		t.Errorf("expected snippet %q, got %q", bodyContent, gotSnippet)
	}
}

// TestBodyCapture_BodyExactlyAtLimit verifies that a body whose length equals
// the limit is captured in full (boundary condition: no truncation needed).
func TestBodyCapture_BodyExactlyAtLimit(t *testing.T) {
	bodyContent := strings.Repeat("a", 100)

	var gotSnippet string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSnippet = ReqBodySnippetFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(bodyContent))
	rr := httptest.NewRecorder()

	BodyCapture(func() int { return 100 })(handler).ServeHTTP(rr, req)

	if gotSnippet != bodyContent {
		t.Errorf("expected snippet of length %d, got length %d", len(bodyContent), len(gotSnippet))
	}
}

// TestBodyCapture_BodyOverLimit verifies that when the body exceeds the limit,
// the snippet is truncated to exactly limit bytes (UTF-8-safe), but the handler
// still sees the complete original body via body reconstruction.
func TestBodyCapture_BodyOverLimit(t *testing.T) {
	bodyContent := strings.Repeat("a", 600)

	var gotSnippet string
	var gotBodyLen int

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSnippet = ReqBodySnippetFromContext(r.Context())
		b, _ := io.ReadAll(r.Body)
		gotBodyLen = len(b)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(bodyContent))
	rr := httptest.NewRecorder()

	BodyCapture(func() int { return 500 })(handler).ServeHTTP(rr, req)

	if len(gotSnippet) != 500 {
		t.Errorf("expected snippet of exactly 500 bytes, got %d bytes", len(gotSnippet))
	}
	if gotBodyLen != 600 {
		t.Errorf("expected handler to read 600 bytes from reconstructed body, got %d", gotBodyLen)
	}
}

// TestBodyCapture_UTF8EmojiBoundary verifies BODY-02: the UTF-8 truncation does
// not split a multi-byte sequence at the limit boundary.
//
// Body: "abc" + "🎉" (3 ASCII bytes + 4-byte emoji = 7 bytes total)
// Limit: 4 (reads 5 bytes via LimitReader: "abc\xF0\x9F")
// Expected snippet: "abc" (emoji excluded, not split at byte 4)
// Expected reconstructed body: all 7 bytes
func TestBodyCapture_UTF8EmojiBoundary(t *testing.T) {
	// 🎉 = U+1F389 = 0xF0 0x9F 0x8E 0x89 (4 bytes)
	bodyContent := "abc\xF0\x9F\x8E\x89"

	var gotSnippet string
	var gotBody []byte

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSnippet = ReqBodySnippetFromContext(r.Context())
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(bodyContent))
	rr := httptest.NewRecorder()

	// limit=4: LimitReader reads limit+1=5 bytes = "abc\xF0\x9F"
	// utf8Truncate("abc\xF0\x9F", 4): i=4, b[4]=0x9F is continuation (10xxxxxx) → i=3,
	// b[3]=0xF0 is rune start (11110xxx) → returns b[:3]="abc"
	BodyCapture(func() int { return 4 })(handler).ServeHTTP(rr, req)

	if gotSnippet != "abc" {
		t.Errorf("expected snippet %q (emoji excluded, not split), got %q", "abc", gotSnippet)
	}
	if string(gotBody) != bodyContent {
		t.Errorf("expected reconstructed body %q, got %q", bodyContent, string(gotBody))
	}
}

// TestBodyCapture_NilBody verifies that when r.Body is nil (e.g. for some GET
// requests), the middleware does not panic and the snippet is "" (no value stored).
func TestBodyCapture_NilBody(t *testing.T) {
	var gotSnippet string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSnippet = ReqBodySnippetFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Body = nil // explicitly nil
	rr := httptest.NewRecorder()

	BodyCapture(func() int { return 500 })(handler).ServeHTTP(rr, req)

	if gotSnippet != "" {
		t.Errorf("expected empty snippet for nil body, got %q", gotSnippet)
	}
}

// TestBodyCapture_CloseDelegate verifies the HIGH fix: the reconstructed r.Body
// delegates Close() to the original body, not to a no-op closer.
// This prevents resource leaks on keep-alive HTTP/1.1 connections.
func TestBodyCapture_CloseDelegate(t *testing.T) {
	tc := &trackingCloser{Reader: strings.NewReader("hello")}

	var wrappedBody io.ReadCloser

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrappedBody = r.Body
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Body = tc // set trackingCloser as the body
	rr := httptest.NewRecorder()

	BodyCapture(func() int { return 500 })(handler).ServeHTTP(rr, req)

	if wrappedBody == nil {
		t.Fatal("expected wrapped body to be set in handler")
	}

	// Close the wrapped body — must delegate to original trackingCloser.Close()
	if err := wrappedBody.Close(); err != nil {
		t.Fatalf("Close() returned unexpected error: %v", err)
	}

	if !tc.closed {
		t.Error("expected original body's Close() to be called, but trackingCloser.closed is false")
	}
}

// TestBodyCapture_JsonDecoder verifies that after BodyCapture middleware runs,
// json.NewDecoder(r.Body).Decode() succeeds and parses the original JSON correctly.
// This is the primary integration concern: the handler's json.NewDecoder must see
// the complete body despite the middleware having already read it.
func TestBodyCapture_JsonDecoder(t *testing.T) {
	type payload struct {
		Model string `json:"model"`
	}

	const bodyContent = `{"model":"gpt-4"}`

	var gotPayload payload

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("json.NewDecoder(r.Body).Decode() failed: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(bodyContent))
	rr := httptest.NewRecorder()

	BodyCapture(func() int { return 500 })(handler).ServeHTTP(rr, req)

	if gotPayload.Model != "gpt-4" {
		t.Errorf("expected model %q, got %q", "gpt-4", gotPayload.Model)
	}
}

// TestReqBodySnippetFromContext_Empty verifies that calling ReqBodySnippetFromContext
// on a bare context (no middleware has run) returns "".
func TestReqBodySnippetFromContext_Empty(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	got := ReqBodySnippetFromContext(req.Context())
	if got != "" {
		t.Errorf("expected empty string from bare context, got %q", got)
	}
}

// TestUtf8Truncate covers all boundary cases for the utf8Truncate helper function.
func TestUtf8Truncate(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		maxBytes int
		want     string
	}{
		{"empty input zero limit", []byte{}, 0, ""},
		{"empty input nonzero limit", []byte{}, 10, ""},
		{"zero maxBytes nonempty input", []byte("hi"), 0, ""},
		{"under limit", []byte("hello"), 10, "hello"},
		{"exactly at limit", []byte("hello"), 5, "hello"},
		{"ascii over limit", []byte("hello world"), 5, "hello"},
		{"emoji at boundary", []byte("abc\xF0\x9F\x8E\x89"), 4, "abc"},
		{"emoji fully included", []byte("abc\xF0\x9F\x8E\x89"), 7, "abc\U0001F389"},
		{"two-byte seq at boundary", []byte("ab\xC3\xA9"), 3, "ab"},       // é = U+00E9
		{"three-byte seq at boundary", []byte("ab\xE2\x82\xAC"), 4, "ab"}, // € = U+20AC
		{"split at limit not limit+1", []byte("a\xC3\xA9b"), 2, "a"},      // limit=2 splits mid-é, walk back to "a"
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utf8Truncate(tt.input, tt.maxBytes)
			if got != tt.want {
				t.Errorf("utf8Truncate(%q, %d) = %q, want %q", tt.input, tt.maxBytes, got, tt.want)
			}
		})
	}
}
