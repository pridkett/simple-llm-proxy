package middleware

import (
	"bufio"
	"net"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

func (rw *responseWriter) Flush() {
	_ = rw.FlushError()
}

func (rw *responseWriter) FlushError() error {
	return http.NewResponseController(rw.ResponseWriter).Flush()
}

// Hijack lets the Realtime upgrade pass through logging and tracing wrappers.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buf, err := http.NewResponseController(rw.ResponseWriter).Hijack()
	if err == nil {
		rw.status = http.StatusSwitchingProtocols
	}
	return conn, buf, err
}

// Logging returns middleware that logs requests with structured fields.
// HTTP 5xx responses are logged at error level, 4xx at warn, others at info.
func Logging() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rw := &responseWriter{
				ResponseWriter: w,
				status:         http.StatusOK,
			}

			next.ServeHTTP(rw, r)

			duration := time.Since(start)

			var ev *zerolog.Event
			switch {
			case rw.status >= 500:
				ev = log.Error()
			case rw.status >= 400:
				ev = log.Warn()
			default:
				ev = log.Info()
			}

			// Include correlation ID if present in context.
			if reqID := RequestIDFromContext(r.Context()); reqID != "" {
				ev = ev.Str("request_id", reqID)
			}

			ev.Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", rw.status).
				Str("duration", duration.Truncate(time.Microsecond).String()).
				Int("bytes", rw.size).
				Msg("request")
		})
	}
}
