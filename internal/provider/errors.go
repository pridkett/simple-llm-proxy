package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// HTTPError retains the upstream status and body through routing and failover.
type HTTPError struct {
	Provider   string
	Status     int
	Body       []byte
	RetryAfter time.Duration
	Cause      error
}

func (e *HTTPError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(e.Body, &body) == nil && body.Error.Message != "" {
		return fmt.Sprintf("%s error: %s", e.Provider, body.Error.Message)
	}
	return fmt.Sprintf("%s error (status %d): %s", e.Provider, e.Status, e.Body)
}
func (e *HTTPError) Unwrap() error                      { return e.Cause }
func (e *HTTPError) HTTPStatus() int                    { return e.Status }
func (e *HTTPError) RetryDelay() time.Duration          { return e.RetryAfter }
func (e *HTTPError) APIErrorBody() json.RawMessage      { return e.Body }
func (e *RateLimitError) HTTPStatus() int               { return http.StatusTooManyRequests }
func (e *RateLimitError) RetryDelay() time.Duration     { return e.RetryAfter }
func (e *RateLimitError) APIErrorBody() json.RawMessage { return e.ResponseBody }
func NewHTTPError(name string, status int, headers http.Header, body []byte, causes ...error) error {
	retry := ParseRetryAfter(headers.Get("Retry-After"))
	if status == http.StatusTooManyRequests {
		return &RateLimitError{Provider: name, RetryAfter: retry, ResponseBody: body}
	}
	e := &HTTPError{Provider: name, Status: status, Body: body, RetryAfter: retry}
	if len(causes) > 0 {
		e.Cause = causes[0]
	}
	return e
}

// Invalid input cannot recover by retrying or cooling down a healthy provider.
func IsInvalidRequest(err error) bool {
	var e interface{ HTTPStatus() int }
	return errors.As(err, &e) && (e.HTTPStatus() == 400 || e.HTTPStatus() == 422)
}
