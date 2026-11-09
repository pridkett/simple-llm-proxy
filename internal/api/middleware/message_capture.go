package middleware

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/otel/content"
	"go.opentelemetry.io/otel/trace"
)

type messageCaptureKey struct{}

func MessageCaptureFromContext(ctx context.Context) *content.Capture {
	capture, _ := ctx.Value(messageCaptureKey{}).(*content.Capture)
	return capture
}

// CaptureMessages runs after key authentication and only for selected model routes.
func CaptureMessages(protocol content.Protocol, settings func() config.OTelMessageCapture) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			key := APIKeyFromContext(ctx)
			cfg := settings()
			if key == nil || key.Key == nil || cfg.MaxBytes <= 0 ||
				!slices.Contains(cfg.ApplicationIDs, key.Key.ApplicationID) || !trace.SpanFromContext(ctx).IsRecording() {
				next.ServeHTTP(w, r)
				return
			}
			capture := content.New(protocol, cfg.MaxBytes)
			ctx = context.WithValue(ctx, messageCaptureKey{}, capture)
			writer := &captureWriter{ResponseWriter: w, capture: capture, status: http.StatusOK}
			if _, ok := w.(http.Flusher); ok {
				w = &captureFlusher{writer}
			} else {
				w = writer
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type captureWriter struct {
	http.ResponseWriter
	capture *content.Capture
	status  int
	started bool
}

func (w *captureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *captureWriter) WriteHeader(status int) {
	if !w.started && status >= 200 {
		w.status = status
		w.started = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *captureWriter) Write(data []byte) (int, error) {
	w.started = true
	streaming := strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream")
	n, err := w.ResponseWriter.Write(data)
	if w.status >= 200 && w.status < 300 {
		w.capture.Write(data[:n], streaming, err != nil || n != len(data))
	}
	return n, err
}

type captureFlusher struct{ *captureWriter }

func (w *captureFlusher) Flush() { _ = w.FlushError() }

func (w *captureFlusher) FlushError() error {
	w.started = true
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err != nil && w.status >= 200 && w.status < 300 {
		streaming := strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream")
		w.capture.Write(nil, streaming, true)
	}
	return err
}
