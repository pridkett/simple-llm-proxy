package otel

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

func TestDisabledProvider(t *testing.T) {
	p, err := NewProvider(config.OTelSettings{})
	if err != nil || p.Enabled() || p.sdk != nil {
		t.Fatalf("disabled provider: %+v, %v", p, err)
	}
	_, span := p.Tracer().Start(context.Background(), "ignored")
	if span.IsRecording() {
		t.Fatal("disabled tracer should not record")
	}
	span.End()
	if err := p.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPExportAndShutdown(t *testing.T) {
	requests := make(chan *collector.ExportTraceServiceRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Basic secret" {
			t.Errorf("auth header missing")
		}
		data, _ := io.ReadAll(r.Body)
		var req collector.ExportTraceServiceRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			t.Errorf("decode OTLP: %v", err)
		}
		requests <- &req
	}))
	defer srv.Close()
	p, err := NewProvider(config.OTelSettings{Enabled: true, SamplingRatio: 1, ServiceName: "test-proxy", Exporter: config.OTelExporter{
		Protocol: "http", Endpoint: srv.URL, Insecure: true, Headers: map[string]string{"Authorization": "Basic secret"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, span := p.Tracer().Start(context.Background(), "test-span")
	span.End()
	if err := p.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := p.Shutdown(); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
	select {
	case req := <-requests:
		if len(req.ResourceSpans) != 1 || len(req.ResourceSpans[0].ScopeSpans) != 1 || len(req.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
			t.Fatalf("unexpected OTLP spans: %+v", req.ResourceSpans)
		}
		foundService := false
		for _, attr := range req.ResourceSpans[0].Resource.Attributes {
			if attr.Key == "service.name" && attr.Value.GetStringValue() == "test-proxy" {
				foundService = true
			}
		}
		if !foundService {
			t.Fatal("service.name resource attribute missing")
		}
		if req.ResourceSpans[0].ScopeSpans[0].Spans[0].Name != "test-span" {
			t.Fatal("span was not flushed")
		}
	default:
		t.Fatal("shutdown returned without exporting the span")
	}
}

func TestZeroSamplingRatioSuppressesSpans(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer srv.Close()
	p, err := NewProvider(config.OTelSettings{Enabled: true, SamplingRatio: 0, Exporter: config.OTelExporter{
		Endpoint: srv.URL, Insecure: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, span := p.Tracer().Start(context.Background(), "sampled-out")
	if span.IsRecording() {
		t.Fatal("zero ratio recorded a span")
	}
	span.End()
	if err := p.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatalf("unexpected export requests: %d", requests.Load())
	}
}

type grpcCollector struct {
	collector.UnimplementedTraceServiceServer
	requests chan *collector.ExportTraceServiceRequest
}

func (c *grpcCollector) Export(_ context.Context, req *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
	c.requests <- req
	return &collector.ExportTraceServiceResponse{}, nil
}

func TestGRPCExport(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	collectorImpl := &grpcCollector{requests: make(chan *collector.ExportTraceServiceRequest, 1)}
	collector.RegisterTraceServiceServer(server, collectorImpl)
	go server.Serve(lis)
	defer server.Stop()
	p, err := NewProvider(config.OTelSettings{Enabled: true, SamplingRatio: 1, Exporter: config.OTelExporter{
		Protocol: "grpc", Endpoint: "http://" + lis.Addr().String(), Insecure: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, span := p.Tracer().Start(context.Background(), "grpc-span")
	span.End()
	if err := p.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-collectorImpl.requests:
		if req.ResourceSpans[0].ScopeSpans[0].Spans[0].Name != "grpc-span" {
			t.Fatal("missing gRPC span")
		}
	default:
		t.Fatal("gRPC exporter did not flush")
	}
}

func TestProviderRejectsInvalidConfiguration(t *testing.T) {
	for _, settings := range []config.OTelSettings{
		{Enabled: true, SamplingRatio: -1},
		{Enabled: true, SamplingRatio: 2},
		{Enabled: true, SamplingRatio: 1, Exporter: config.OTelExporter{Endpoint: "not-a-url"}},
		{Enabled: true, SamplingRatio: 1, Exporter: config.OTelExporter{Endpoint: "http://localhost:4318"}},
		{Enabled: true, SamplingRatio: 1, Exporter: config.OTelExporter{Protocol: "udp", Endpoint: "http://localhost:4318"}},
	} {
		if p, err := NewProvider(settings); err == nil {
			t.Fatalf("accepted invalid settings %+v: %+v", settings, p)
		}
	}
}
