package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
)

// apiPrefixes are path prefixes owned by the JSON API. The SPA fallback must
// never serve HTML for these — an unmatched API path gets a JSON 404 instead.
var apiPrefixes = []string{"/v1/", "/admin/", "/auth/", "/health", "/openapi.json"}

// SPA serves the built frontend (Vue dist directory) with an index.html
// fallback for client-side routes. It is mounted as the router's NotFound
// handler, so it only ever sees requests no API route matched.
func SPA(dir string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(dir))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			model.WriteError(w, model.ErrNotFound("not found"))
			return
		}

		reqPath := r.URL.Path
		for _, p := range apiPrefixes {
			if strings.HasPrefix(reqPath, p) || reqPath == strings.TrimSuffix(p, "/") {
				model.WriteError(w, model.ErrNotFound("not found"))
				return
			}
		}

		// Serve the file if it exists; otherwise fall back to index.html so
		// the SPA router handles the path client-side.
		clean := filepath.Join(dir, filepath.Clean("/"+reqPath))
		if info, err := os.Stat(clean); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	}
}
