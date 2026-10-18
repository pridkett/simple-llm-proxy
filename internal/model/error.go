package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"
)

// APIError represents an OpenAI-compatible error response.
type APIError struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error details.
type ErrorDetail struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param,omitempty"`
	Code    *string `json:"code,omitempty"`
}

// ProxyError is an error type used internally.
type ProxyError struct {
	RawBody    json.RawMessage
	RetryAfter time.Duration
	StatusCode int
	Message    string
	Type       string
	Code       string
	Err        error
}

func (e *ProxyError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *ProxyError) Unwrap() error {
	return e.Err
}

// ToAPIError converts a ProxyError to an APIError response.
func (e *ProxyError) ToAPIError() APIError {
	var code *string
	if e.Code != "" {
		code = &e.Code
	}
	return APIError{
		Error: ErrorDetail{
			Message: e.Message,
			Type:    e.Type,
			Code:    code,
		},
	}
}

// Common error constructors.

func ErrBadRequest(message string) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusBadRequest,
		Message:    message,
		Type:       "invalid_request_error",
	}
}

func ErrUnauthorized(message string) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusUnauthorized,
		Message:    message,
		Type:       "authentication_error",
		Code:       "invalid_api_key",
	}
}

func ErrModelNotFound(model string) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("model '%s' not found", model),
		Type:       "invalid_request_error",
		Code:       "model_not_found",
	}
}

func ErrNoDeploymentAvailable(model string) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusServiceUnavailable,
		Message:    fmt.Sprintf("no healthy deployment available for model '%s'", model),
		Type:       "server_error",
		Code:       "no_deployment_available",
	}
}

func ErrProviderError(provider string, err error) *ProxyError {
	result := &ProxyError{StatusCode: http.StatusBadGateway, Message: fmt.Sprintf("provider '%s' error: %v", provider, err), Type: "server_error", Code: "provider_error", Err: err}
	var upstream interface {
		HTTPStatus() int
		RetryDelay() time.Duration
		APIErrorBody() json.RawMessage
	}
	if errors.As(err, &upstream) {
		result.StatusCode = upstream.HTTPStatus()
		result.RetryAfter = upstream.RetryDelay()
		var body map[string]json.RawMessage
		if json.Unmarshal(upstream.APIErrorBody(), &body) == nil && len(body["error"]) > 0 {
			result.RawBody = upstream.APIErrorBody()
		}
		if result.StatusCode == 429 {
			result.Type = "rate_limit_error"
		} else if result.StatusCode >= 400 && result.StatusCode < 500 {
			result.Type = "invalid_request_error"
		}
	}
	return result
}

func ErrInternalServer(message string, err error) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusInternalServerError,
		Message:    message,
		Type:       "server_error",
		Err:        err,
	}
}

func ErrForbidden(message string) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusForbidden,
		Message:    message,
		Type:       "permission_error",
		Code:       "forbidden",
	}
}

func ErrNotFound(message string) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusNotFound,
		Message:    message,
		Type:       "invalid_request_error",
		Code:       "not_found",
	}
}

func ErrInternal(message string) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusInternalServerError,
		Message:    message,
		Type:       "server_error",
		Code:       "internal_error",
	}
}

// ErrServiceUnavailable returns a 503 service unavailable error.
func ErrServiceUnavailable(message string) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusServiceUnavailable,
		Message:    message,
		Type:       "server_error",
		Code:       "service_unavailable",
	}
}

// ErrRateLimited returns a 429 rate limit exceeded error.
// Per D-10: type = "rate_limit_error", message = "rate_limit_exceeded".
func ErrRateLimited(message string) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusTooManyRequests,
		Message:    message,
		Type:       "rate_limit_error",
		Code:       "rate_limit_exceeded",
	}
}

// ErrBudgetExceeded returns a 429 budget exceeded error.
// Per D-10: type = "budget_limit_error", message = "budget_exceeded".
func ErrBudgetExceeded(message string) *ProxyError {
	return &ProxyError{
		StatusCode: http.StatusTooManyRequests,
		Message:    message,
		Type:       "budget_limit_error",
		Code:       "budget_exceeded",
	}
}

// WriteError writes an error response to the http.ResponseWriter.
func WriteError(w http.ResponseWriter, err *ProxyError) {
	w.Header().Set("Content-Type", "application/json")
	if err.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(err.RetryAfter.Seconds()))))
	}
	w.WriteHeader(err.StatusCode)
	if len(err.RawBody) > 0 {
		w.Write(err.RawBody)
		return
	}
	json.NewEncoder(w).Encode(err.ToAPIError())
}

func (e *ProxyError) HTTPStatus() int           { return e.StatusCode }
func (e *ProxyError) RetryDelay() time.Duration { return e.RetryAfter }
func (e *ProxyError) APIErrorBody() json.RawMessage {
	if len(e.RawBody) > 0 {
		return e.RawBody
	}
	data, _ := json.Marshal(e.ToAPIError())
	return data
}
