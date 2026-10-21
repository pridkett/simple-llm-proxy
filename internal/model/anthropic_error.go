package model

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
)

// AnthropicError is shared by HTTP responses and named SSE error events.
type AnthropicError struct {
	Type      string      `json:"type"`
	Error     ErrorDetail `json:"error"`
	RequestID string      `json:"request_id,omitempty"`
}

func ToAnthropicError(err *ProxyError, requestID string) AnthropicError {
	typeName := "api_error"
	switch err.StatusCode {
	case 400, 422:
		typeName = "invalid_request_error"
	case 401:
		typeName = "authentication_error"
	case 403:
		typeName = "permission_error"
	case 404:
		typeName = "not_found_error"
	case 413:
		typeName = "request_too_large"
	case 429:
		typeName = "rate_limit_error"
	case 503, 529:
		typeName = "overloaded_error"
	case 504:
		typeName = "timeout_error"
	default:
		if err.StatusCode >= 400 && err.StatusCode < 500 {
			typeName = "invalid_request_error"
		}
	}
	return AnthropicError{Type: "error", Error: ErrorDetail{Type: typeName, Message: err.Message}, RequestID: requestID}
}

func WriteAnthropicError(w http.ResponseWriter, err *ProxyError) {
	w.Header().Set("Content-Type", "application/json")
	if err.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(err.RetryAfter.Seconds()))))
	}
	w.WriteHeader(err.StatusCode)
	json.NewEncoder(w).Encode(ToAnthropicError(err, w.Header().Get("request-id")))
}

// WriteRequestError keeps middleware errors in the endpoint's wire format.
func WriteRequestError(w http.ResponseWriter, req *http.Request, err *ProxyError) {
	if req.URL.Path == "/v1/messages" {
		WriteAnthropicError(w, err)
		return
	}
	WriteError(w, err)
}
