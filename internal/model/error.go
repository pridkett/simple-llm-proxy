package model

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	return &ProxyError{
		StatusCode: http.StatusBadGateway,
		Message:    fmt.Sprintf("provider '%s' error: %v", provider, err),
		Type:       "server_error",
		Code:       "provider_error",
		Err:        err,
	}
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
	w.WriteHeader(err.StatusCode)
	json.NewEncoder(w).Encode(err.ToAPIError())
}
