package handler

import (
	"encoding/json"
	"net/http"
)

// deprecatedEndpointError is the JSON structure returned for deprecated endpoints.
type deprecatedEndpointError struct {
	Error deprecatedDetail `json:"error"`
}

type deprecatedDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    int    `json:"code"`
}

// Completions handles POST /v1/completions requests.
// This endpoint is deprecated — it returns 410 Gone directing clients
// to use POST /v1/chat/completions instead.
func Completions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		json.NewEncoder(w).Encode(deprecatedEndpointError{
			Error: deprecatedDetail{
				Message: "POST /v1/completions is deprecated. Use POST /v1/chat/completions instead.",
				Type:    "deprecated_endpoint",
				Code:    http.StatusGone,
			},
		})
	}
}
