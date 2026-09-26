package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	proxyotel "github.com/pwagstro/simple_llm_proxy/internal/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
