package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/go-chi/chi/v5"
	proxyotel "github.com/pwagstro/simple_llm_proxy/internal/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestOTelHTTPSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(t.Context())
	r := chi.NewRouter()
	r.Use(RequestID())
	r.Use(OTel(provider.Tracer("test")))
	r.Get("/items/{id}", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("bad"))
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/items/123", nil)
	req.Header.Set("X-Request-ID", "test-request-id")
	r.ServeHTTP(w, req)
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans", len(spans))
	}
	span := spans[0]
	if span.Name() != "GET /items/{id}" {
		t.Fatalf("span name = %q", span.Name())
	}
	attrs := make(map[string]any)
	for _, a := range span.Attributes() {
		attrs[string(a.Key)] = a.Value.AsInterface()
	}
	if attrs["http.route"] != "/items/{id}" || attrs["http.status_code"] != int64(502) || attrs["http.response.body.size"] != int64(3) || attrs[proxyotel.RequestID] != "test-request-id" {
		t.Fatalf("unexpected attributes: %+v", attrs)
	}
}

func TestOTelTrustedTraceparentJoinsHarnessRun(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(t.Context())
	tracer := provider.Tracer("test")
	ctx, run := tracer.Start(t.Context(), "agent.run")
	h := OTel(tracer, netip.MustParsePrefix("127.0.0.1/32"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, llm := tracer.Start(r.Context(), "chat test", trace.WithSpanKind(trace.SpanKindClient))
		defer llm.End()
		w.WriteHeader(http.StatusOK)
	}))
	for _, peer := range []string{"127.0.0.1:49152", "[::ffff:127.0.0.1]:49153"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.RemoteAddr = peer
		(propagation.TraceContext{}).Inject(ctx, propagation.HeaderCarrier(req.Header))
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	run.End()

	spans := recorder.Ended()
	if len(spans) != 5 {
		t.Fatalf("expected a root and two request span pairs, got %d spans", len(spans))
	}
	traceID := run.SpanContext().TraceID()
	for i := 0; i < 4; i += 2 {
		llm, httpSpan := spans[i], spans[i+1]
		if httpSpan.SpanContext().TraceID() != traceID || llm.SpanContext().TraceID() != traceID ||
			httpSpan.Parent().SpanID() != run.SpanContext().SpanID() || !httpSpan.Parent().IsRemote() ||
			llm.Parent().SpanID() != httpSpan.SpanContext().SpanID() {
			t.Fatalf("request %d does not attach to run: HTTP parent %v, LLM parent %v", i/2, httpSpan.Parent(), llm.Parent())
		}
	}
	if spans[1].SpanContext().SpanID() == spans[3].SpanContext().SpanID() {
		t.Fatal("requests reused the same span ID")
	}
}

func TestOTelTraceparentTrustAndValidation(t *testing.T) {
	const incoming = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	trusted := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	for _, tc := range []struct {
		name    string
		peer    string
		headers []string
		trust   []netip.Prefix
	}{
		{"default", "127.0.0.1:4000", []string{incoming}, nil},
		{"untrusted socket with spoofed forwarded IP", "192.0.2.2:4000", []string{incoming}, trusted},
		{"malformed socket", "127.0.0.1", []string{incoming}, trusted},
		{"missing traceparent", "127.0.0.1:4000", nil, trusted},
		{"invalid traceparent", "127.0.0.1:4000", []string{"broken"}, trusted},
		{"multiple traceparents", "127.0.0.1:4000", []string{incoming, incoming}, trusted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			defer provider.Shutdown(t.Context())
			h := OTel(provider.Tracer("test"), tc.trust...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", "127.0.0.1")
			for _, header := range tc.headers {
				req.Header.Add("Traceparent", header)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
			if spans := recorder.Ended(); len(spans) != 1 || spans[0].Parent().IsValid() {
				t.Fatalf("expected an independent root span: %v", spans)
			}
		})
	}
}

func TestDisabledOTelIsPassThrough(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	handler := OTel(nil)(next)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Fatal("next handler was not called")
	}
}

func TestOTelSpanClosesWhenRecoveryCatchesPanic(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(t.Context())
	r := chi.NewRouter()
	r.Use(Recovery())
	r.Use(RequestID())
	r.Use(OTel(provider.Tracer("test")))
	r.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("unexpected") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("HTTP %d", w.Code)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans=%d", len(spans))
	}
	attrs := make(map[string]any)
	for _, a := range spans[0].Attributes() {
		attrs[string(a.Key)] = a.Value.AsInterface()
	}
	if attrs["http.status_code"] != int64(500) || spans[0].Status().Code != codes.Error {
		t.Fatalf("panic not reflected in span: attrs=%v status=%v", attrs, spans[0].Status())
	}
}
