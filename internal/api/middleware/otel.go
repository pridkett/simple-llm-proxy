package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	proxyotel "github.com/pwagstro/simple_llm_proxy/internal/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OTel records one HTTP server span around the request. A nil tracer leaves
// the middleware completely out of the request path when tracing is disabled.
func OTel(tracer trace.Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if tracer == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path, trace.WithSpanKind(trace.SpanKindServer))
			if id := RequestIDFromContext(ctx); id != "" {
				span.SetAttributes(attribute.String(proxyotel.RequestID, id))
			}
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				panicValue := recover()
				if panicValue != nil {
					rw.status = http.StatusInternalServerError
				}
				pattern := r.URL.Path
				if routeCtx := chi.RouteContext(ctx); routeCtx != nil && routeCtx.RoutePattern() != "" {
					pattern = routeCtx.RoutePattern()
				}
				span.SetName(r.Method + " " + pattern)
				span.SetAttributes(
					attribute.String("http.route", pattern),
					attribute.Int("http.status_code", rw.status),
					attribute.Int("http.response.body.size", rw.size),
				)
				if rw.status >= 500 {
					span.SetStatus(codes.Error, http.StatusText(rw.status))
				}
				if panicValue != nil {
					span.SetAttributes(attribute.String(proxyotel.ErrorType, "panic"))
				}
				span.End()
				if panicValue != nil {
					panic(panicValue)
				}
			}()
			next.ServeHTTP(rw, r.WithContext(ctx))
		})
	}
}
