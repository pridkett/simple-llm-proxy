package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"unicode/utf8"
)

// ContextKeyReqBodySnippet is the context key for the captured request body snippet.
// Uses the shared contextKey type declared in session.go — NOT redeclared here.
const ContextKeyReqBodySnippet contextKey = "req_body_snippet"

// ReqBodySnippetFromContext retrieves the request body snippet from the request context.
// Returns an empty string if BodyCapture middleware did not run or limit was 0 (disabled).
// The nil/NULL distinction between disabled and empty-body is enforced at the storage
// INSERT layer (logRequestParams.ReqBodySnippet *string in chat.go), not here.
func ReqBodySnippetFromContext(ctx context.Context) string {
	s, _ := ctx.Value(ContextKeyReqBodySnippet).(string)
	return s
}

// bodyWithClose wraps an io.Reader with an explicit io.Closer.
// Used by BodyCapture to replace r.Body with a MultiReader that reads replayed bytes
// while still forwarding Close() to the original body, preventing resource leaks.
type bodyWithClose struct {
	io.Reader
	io.Closer
}

// BodyCapture returns middleware that reads up to limit bytes from the request body,
// stores a UTF-8-safe truncated snippet in the request context, then reconstructs
// the full body so downstream handlers (json.NewDecoder) see the complete stream.
//
// When limit is 0, body capture is disabled: no bytes are read, no value is stored
// in the context (ReqBodySnippetFromContext will return ""), and the request body
// is not modified.
//
// The reconstructed body's Close() method delegates to the original body's Close()
// method, preserving correct connection lifecycle under keep-alive workloads.
//
// Registration: mux.Use(middleware.BodyCapture(limitFn)) inside the /v1/* KeyAuth
// group in router.go, AFTER KeyAuth so authentication is already enforced.
// limitFn is a func() int getter so live config reloads are observed without
// rebuilding the router.
func BodyCapture(limitFn func() int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := limitFn()
			if limit > 0 && r.Body != nil {
				// Read limit+1 bytes: the +1 detects whether the body exceeds the limit
				// without requiring a second read. We still truncate to exactly limit bytes.
				raw, _ := io.ReadAll(io.LimitReader(r.Body, int64(limit)+1))
				snippet := utf8Truncate(raw, limit)
				// Reconstruct: prepend already-read bytes to the remaining (unread) stream.
				// bodyWithClose preserves the original body's Close() — not a NopCloser.
				r.Body = bodyWithClose{
					Reader: io.MultiReader(bytes.NewReader(raw), r.Body),
					Closer: r.Body,
				}
				ctx := context.WithValue(r.Context(), ContextKeyReqBodySnippet, snippet)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			// limit == 0 or body is nil: skip capture entirely; do not set context value.
			next.ServeHTTP(w, r)
		})
	}
}

// utf8Truncate returns at most maxBytes bytes of b as a valid UTF-8 string.
// If len(b) <= maxBytes, the full slice is returned as-is.
// Otherwise, it walks backward from maxBytes to find the nearest valid rune start
// boundary, ensuring multi-byte sequences are never split at the truncation point.
// If maxBytes is 0, returns "".
//
// Example: utf8Truncate([]byte("abc🎉"), 4) → "abc"
// (🎉 = 0xF0 0x9F 0x8E 0x89, 4 bytes; b[3]=0xF0 is a rune start at index 3;
// walking back from i=4: b[4]=0x9F is NOT a rune start (10xxxxxx continuation);
// i becomes 3; b[3]=0xF0 IS a rune start; loop exits → returns b[:3]="abc")
func utf8Truncate(b []byte, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(b) <= maxBytes {
		return string(b)
	}
	i := maxBytes
	for i > 0 && !utf8.RuneStart(b[i]) {
		i--
	}
	return string(b[:i])
}
