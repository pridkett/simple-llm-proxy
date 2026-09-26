package otel

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Provider owns the trace exporter and batch processor for one server lifetime.
type Provider struct {
	enabled bool
	tracer  trace.Tracer
	sdk     *sdktrace.TracerProvider
	once    sync.Once
	err     error
}

func NewProvider(settings config.OTelSettings) (*Provider, error) {
	if !settings.Enabled {
		return &Provider{tracer: trace.NewNoopTracerProvider().Tracer("simple-llm-proxy")}, nil
	}
	if settings.SamplingRatio < 0 || settings.SamplingRatio > 1 || math.IsNaN(settings.SamplingRatio) {
		return nil, fmt.Errorf("otel_settings.sampling_ratio must be between 0 and 1")
	}
	if settings.Exporter.Endpoint == "" {
		return nil, fmt.Errorf("otel_settings.exporter.endpoint is required when tracing is enabled")
	}
	u, err := url.Parse(settings.Exporter.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("otel_settings.exporter.endpoint must be an http(s) URL with a host")
	}
	if u.Scheme == "http" && !settings.Exporter.Insecure {
		return nil, fmt.Errorf("otel_settings.exporter.insecure must be true for an http endpoint")
	}
	if settings.ServiceName == "" {
		settings.ServiceName = "simple-llm-proxy"
	}
	protocol := settings.Exporter.Protocol
	if protocol == "" {
		protocol = "http"
	}
	var exporter sdktrace.SpanExporter
	switch protocol {
	case "http":
		if u.Path == "" || u.Path == "/" {
			u.Path = "/v1/traces"
		}
		options := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(u.String()), otlptracehttp.WithHeaders(settings.Exporter.Headers)}
		if settings.Exporter.Insecure {
			options = append(options, otlptracehttp.WithInsecure())
		}
		exporter, err = otlptracehttp.New(context.Background(), options...)
	case "grpc":
		if u.Path != "" && u.Path != "/" {
			return nil, fmt.Errorf("gRPC OTLP endpoint must not contain a path")
		}
		options := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(u.Host), otlptracegrpc.WithHeaders(settings.Exporter.Headers)}
		if settings.Exporter.Insecure || strings.EqualFold(u.Scheme, "http") {
			options = append(options, otlptracegrpc.WithInsecure())
		}
		exporter, err = otlptracegrpc.New(context.Background(), options...)
	default:
		return nil, fmt.Errorf("unsupported OTLP protocol %q", protocol)
	}
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}
	res := resource.NewWithAttributes("", attribute.String("service.name", settings.ServiceName))
	sdk := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exporter)),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(settings.SamplingRatio))),
		sdktrace.WithResource(res),
	)
	return &Provider{enabled: true, tracer: sdk.Tracer("simple-llm-proxy"), sdk: sdk}, nil
}

func (p *Provider) Enabled() bool { return p != nil && p.enabled }
func (p *Provider) Tracer() trace.Tracer {
	if p == nil {
		return trace.NewNoopTracerProvider().Tracer("simple-llm-proxy")
	}
	return p.tracer
}

// Shutdown flushes spans after the HTTP server has stopped accepting requests.
func (p *Provider) Shutdown() error {
	if p == nil || p.sdk == nil {
		return nil
	}
	p.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p.err = p.sdk.Shutdown(ctx)
	})
	return p.err
}
