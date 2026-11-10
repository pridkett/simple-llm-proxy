package api_test

import (
	"bytes"
	"net/http"
	"testing"

	proxyotel "github.com/pwagstro/simple_llm_proxy/internal/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Only model calls reach the trace backend; housekeeping routes stay out.
func TestOnlyModelRoutesAreTraced(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer provider.Shutdown(t.Context())
	f := newDecisionFixtureWithTracer(t, provider.Tracer("test"))

	for _, path := range []string{"/health", "/v1/models", "/v1/models/luna"} {
		if status, _, body := f.request(t, "GET", path, "", "master-only", false); status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, status, body)
		}
	}
	if spans := recorder.Ended(); len(spans) != 0 {
		names := make([]string, 0, len(spans))
		for _, span := range spans {
			names = append(names, span.Name())
		}
		t.Fatalf("housekeeping routes produced spans: %v", names)
	}

	req, err := http.NewRequest("POST", f.url+"/v1/decisions", bytes.NewBufferString(decisionBody("luna")))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer master-only")
	req.Header.Set("X-User-ID", "owui-user-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("decision status %d", resp.StatusCode)
	}

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want HTTP and model spans", len(spans))
	}
	for _, span := range spans {
		attrs := map[string]any{}
		for _, a := range span.Attributes() {
			attrs[string(a.Key)] = a.Value.AsInterface()
		}
		if attrs[proxyotel.UserID] != "owui-user-1" || attrs[proxyotel.LangfuseUserID] != "owui-user-1" {
			t.Fatalf("span %q missing user attributes: %+v", span.Name(), attrs)
		}
	}
}
