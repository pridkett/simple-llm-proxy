package handler

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strconv"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	proxyotel "github.com/pwagstro/simple_llm_proxy/internal/otel"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type operationSpan struct{ span trace.Span }

func beginOperation(ctx context.Context, tracer trace.Tracer, operation, model string, stream bool) (context.Context, *operationSpan) {
	if tracer == nil {
		return ctx, nil
	}
	ctx, span := tracer.Start(ctx, operation+" "+model, trace.WithSpanKind(trace.SpanKindClient))
	attrs := []attribute.KeyValue{
		attribute.String(proxyotel.GenAIOperationName, operation),
		attribute.String(proxyotel.GenAIRequestModel, model),
		attribute.Bool(proxyotel.GenAIRequestStream, stream),
	}
	if id := middleware.RequestIDFromContext(ctx); id != "" {
		attrs = append(attrs, attribute.String(proxyotel.RequestID, id))
	}
	if key := middleware.APIKeyFromContext(ctx); key != nil && key.Key != nil {
		if key.TeamName != "" {
			attrs = append(attrs, attribute.String(proxyotel.Team, key.TeamName))
		}
		if key.AppName != "" {
			attrs = append(attrs, attribute.String(proxyotel.Application, key.AppName))
		}
		if key.Key.Name != "" {
			attrs = append(attrs, attribute.String(proxyotel.APIKeyName, key.Key.Name))
		}
	}
	if user := middleware.UserFromContext(ctx); user != nil && user.Email != "" {
		attrs = append(attrs, attribute.String(proxyotel.UserEmail, user.Email))
	}
	span.SetAttributes(attrs...)
	return ctx, &operationSpan{span: span}
}

func (o *operationSpan) end() {
	if o != nil {
		o.span.End()
	}
}

func (o *operationSpan) failedAttempt(d *provider.Deployment, err error) {
	if o == nil || err == nil {
		return
	}
	attrs := []attribute.KeyValue{attribute.String(proxyotel.ErrorType, errorType(err))}
	if d != nil {
		attrs = append(attrs, attribute.String(proxyotel.GenAIProviderName, d.ProviderName), attribute.String(proxyotel.GenAIResponseModel, d.ActualModel))
	}
	o.span.AddEvent("llmproxy.provider_attempt.failed", trace.WithAttributes(attrs...))
}

func (o *operationSpan) finish(result *router.RouteResult, usage *model.Usage, cost float64) {
	if o == nil || result == nil {
		return
	}
	d := result.DeploymentUsed
	if d == nil && len(result.DeploymentsTried) > 0 {
		d = result.DeploymentsTried[len(result.DeploymentsTried)-1]
	}
	if d != nil {
		o.span.SetAttributes(attribute.String(proxyotel.GenAIProviderName, d.ProviderName), attribute.String(proxyotel.GenAIResponseModel, d.ActualModel))
		if u, err := url.Parse(d.APIBase); err == nil && u.Hostname() != "" {
			o.span.SetAttributes(attribute.String(proxyotel.ServerAddress, u.Hostname()))
			port := u.Port()
			if port == "" {
				if u.Scheme == "https" {
					port = "443"
				} else if u.Scheme == "http" {
					port = "80"
				}
			}
			if n, err := strconv.Atoi(port); err == nil {
				o.span.SetAttributes(attribute.Int(proxyotel.ServerPort, n))
			}
		}
	}
	if result.PoolName != "" {
		o.span.SetAttributes(attribute.String(proxyotel.PoolName, result.PoolName))
	}
	if usage != nil {
		o.span.SetAttributes(attribute.Int(proxyotel.GenAIInputTokens, usage.PromptTokens), attribute.Int(proxyotel.GenAIOutputTokens, usage.CompletionTokens))
	}
	if usage != nil {
		o.span.SetAttributes(attribute.Float64(proxyotel.CostTotalUSD, cost))
	}
}

func (o *operationSpan) fail(err error) {
	if o == nil || err == nil {
		return
	}
	typeName := errorType(err)
	o.span.SetAttributes(attribute.String(proxyotel.ErrorType, typeName))
	// Record only the error type: upstream error strings may contain payloads or credentials.
	o.span.RecordError(fmt.Errorf("%s", typeName))
	o.span.SetStatus(codes.Error, typeName)
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	return reflect.TypeOf(err).String()
}
