package handler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"reflect"
	"strconv"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	proxyotel "github.com/pwagstro/simple_llm_proxy/internal/otel"
	"github.com/pwagstro/simple_llm_proxy/internal/otel/content"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type operationSpan struct {
	span    trace.Span
	capture *content.Capture
}

func beginOperation(ctx context.Context, tracer trace.Tracer, operation, model string, stream bool, sessionID string) (context.Context, *operationSpan) {
	if tracer == nil {
		return ctx, nil
	}
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
	if sessionID != "" {
		id := traceSessionID(ctx, sessionID)
		session := []attribute.KeyValue{
			attribute.String(proxyotel.GenAIConversationID, id),
			attribute.String(proxyotel.LangfuseSessionID, id),
		}
		trace.SpanFromContext(ctx).SetAttributes(session...)
		attrs = append(attrs, session...)
	}
	ctx, span := tracer.Start(ctx, operation+" "+model, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return ctx, &operationSpan{span: span, capture: middleware.MessageCaptureFromContext(ctx)}
}

// A digest keeps all accepted UTF-8 IDs within Langfuse's ASCII length limit.
func traceSessionID(ctx context.Context, sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	if key := middleware.APIKeyFromContext(ctx); key != nil && key.Key != nil {
		return fmt.Sprintf("app:%d:%x", key.Key.ApplicationID, digest)
	}
	if user := middleware.UserFromContext(ctx); user != nil {
		return fmt.Sprintf("user:%x:%x", sha256.Sum256([]byte(user.ID)), digest)
	}
	return fmt.Sprintf("master:%x", digest)
}

func (o *operationSpan) end() {
	if o != nil {
		if o.capture != nil && o.span.IsRecording() {
			o.span.SetAttributes(o.capture.Attributes()...)
		}
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

func (o *operationSpan) finish(result *router.RouteResult, usage *model.Usage, cost float64, known ...bool) {
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
	costKnown := usage != nil
	if len(known) > 0 {
		costKnown = known[0]
	}
	if costKnown {
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
