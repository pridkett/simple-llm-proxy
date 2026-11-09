package middleware

import (
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5"
	proxyotel "github.com/pwagstro/simple_llm_proxy/internal/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// OTel records one HTTP server span around the request. A nil tracer leaves
// the middleware completely out of the request path when tracing is disabled.
// Incoming trace context is accepted from explicitly trusted socket peers.
func OTel(tracer trace.Tracer, trustedPeers ...netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if tracer == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if len(r.Header.Values("traceparent")) == 1 && trustedTracePeer(r.RemoteAddr, trustedPeers) {
				ctx = (propagation.TraceContext{}).Extract(ctx, propagation.HeaderCarrier(r.Header))
			}
			ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path, trace.WithSpanKind(trace.SpanKindServer))
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

func trustedTracePeer(remoteAddr string, prefixes []netip.Prefix) bool {
	if len(prefixes) == 0 {
		return false
	}
	peer, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return false
	}
	addr := peer.Addr().WithZone("")
	for _, prefix := range prefixes {
		if prefix.Contains(addr) || prefix.Contains(addr.Unmap()) {
			return true
		}
	}
	return false
}
