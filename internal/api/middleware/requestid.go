package middleware

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
)

// ContextKeyRequestID is the context key for the request correlation ID.
const ContextKeyRequestID contextKey = "request_id"

// RequestIDFromContext extracts the request correlation ID from the request context.
// Returns an empty string if no request ID is present.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ContextKeyRequestID).(string)
	return id
}

// RequestID returns middleware that assigns a unique correlation ID to each request.
// If the incoming request already carries an X-Request-ID header, that value is
// reused (pass-through). Otherwise a new UUID v4 is generated using crypto/rand.
// The ID is stored in the request context and set as the X-Request-ID response header.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = newUUID()
			}

			// Set the response header so callers can correlate responses.
			w.Header().Set("X-Request-ID", id)

			ctx := context.WithValue(r.Context(), ContextKeyRequestID, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// newUUID generates a UUID v4 string using crypto/rand.
// Format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx where y is one of {8,9,a,b}.
func newUUID() string {
	var uuid [16]byte
	_, _ = rand.Read(uuid[:])

	// Set version 4 (bits 12-15 of time_hi_and_version)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	// Set variant (bits 6-7 of clock_seq_hi_and_reserved)
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}
