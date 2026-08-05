package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newSPADir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>spa-index</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log('app')"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSPAServesIndexAtRoot(t *testing.T) {
	h := SPA(newSPADir(t))
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "spa-index") {
		t.Errorf("expected index.html content, got %q", rec.Body.String())
	}
}

func TestSPAServesStaticAsset(t *testing.T) {
	h := SPA(newSPADir(t))
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "console.log") {
		t.Errorf("expected asset content, got %q", rec.Body.String())
	}
}

func TestSPAFallsBackToIndexForUnknownPath(t *testing.T) {
	h := SPA(newSPADir(t))
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/some/client/route", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 fallback, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "spa-index") {
		t.Errorf("expected index.html fallback, got %q", rec.Body.String())
	}
}

func TestSPAReturnsJSON404ForAPIPrefixes(t *testing.T) {
	h := SPA(newSPADir(t))
	for _, path := range []string{"/v1/nonexistent", "/admin/nonexistent", "/auth/nonexistent", "/health", "/openapi.json"} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s: expected JSON content type, got %q", path, ct)
		}
	}
}

func TestSPARejectsNonGETMethods(t *testing.T) {
	h := SPA(newSPADir(t))
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for POST, got %d", rec.Code)
	}
}

func TestSPABlocksPathTraversal(t *testing.T) {
	dir := newSPADir(t)
	// Place a sentinel file OUTSIDE the SPA dir
	parent := filepath.Dir(dir)
	if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := SPA(dir)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = "/../secret.txt"
	h(rec, req)

	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("path traversal leaked file outside SPA dir")
	}
}
