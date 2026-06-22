package handler

import (
	"net/http"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

// AdminLogDetail returns the full RequestLog for a single request_id.
// URL param: requestID
// TODO(phase16-03): implement full handler with GetLogByID call.
func AdminLogDetail(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"not yet implemented","type":"server_error"}}`, http.StatusNotImplemented)
	}
}

// AdminLogsMeta returns distinct dimension values for all filter dropdowns.
// TODO(phase16-03): implement full handler with GetLogsMeta call.
func AdminLogsMeta(store storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"not yet implemented","type":"server_error"}}`, http.StatusNotImplemented)
	}
}
