package middleware

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"unicode"

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
			userID := ClientUserIDFromContext(ctx)
			if userID == "" {
				// Without the ClientUser middleware (as in isolated tests), parse the header here.
				if userID = clientUserID(r.Header.Get(ClientUserIDHeader)); userID != "" {
					ctx = context.WithValue(ctx, clientUserIDKey{}, userID)
				}
			}
			if userID != "" {
				span.SetAttributes(ClientUserAttributes(userID)...)
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

// ClientUserIDHeader names the calling application's end user (for example
// OpenWebUI's {{USER_ID}}). It is attribution supplied by an authenticated
// client, not an authenticated identity.
const ClientUserIDHeader = "X-User-ID"

type clientUserIDKey struct{}

// ClientUser records a valid X-User-ID in the request context for logging and
// tracing, independent of whether tracing is enabled.
func ClientUser() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userID := clientUserID(r.Header.Get(ClientUserIDHeader)); userID != "" {
				r = r.WithContext(context.WithValue(r.Context(), clientUserIDKey{}, userID))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientUserIDFromContext returns the X-User-ID accepted for this request.
func ClientUserIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(clientUserIDKey{}).(string)
	return id
}

// ClientUserAttributes maps a client user ID onto the generic and Langfuse keys.
func ClientUserAttributes(userID string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(proxyotel.UserID, userID),
		attribute.String(proxyotel.LangfuseUserID, userID),
	}
}

// clientUserID drops values that are empty, too long, or contain control characters.
func clientUserID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 || strings.ContainsFunc(value, unicode.IsControl) {
		return ""
	}
	return value
}
