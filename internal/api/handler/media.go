package handler

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"

	"github.com/pwagstro/simple_llm_proxy/internal/accounting"
	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/webhook"
	"go.opentelemetry.io/otel/trace"
)

type OpenAIMedia struct {
	Router   *router.Router
	Store    storage.Storage
	Spend    *keystore.SpendAccumulator
	CostMap  *costmap.Manager
	Webhooks *webhook.WebhookDispatcher
	Tracer   trace.Tracer
}

var errRealtimeBudget = errors.New("Realtime sessions require a pool without a spend cap")

// HTTP serves the explicitly registered native media endpoints.
func (h OpenAIMedia) HTTP(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		realtime := strings.HasPrefix(path, "/realtime/")
		if realtime && middleware.APIKeyFromContext(req.Context()) != nil {
			model.WriteError(w, model.ErrForbidden("WebRTC setup requires the proxy master key"))
			return
		}
		input, err := readMediaBody(w, req, path)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) || errors.Is(err, multipart.ErrMessageTooLarge) {
				model.WriteError(w, &model.ProxyError{StatusCode: http.StatusRequestEntityTooLarge, Message: "media request exceeds size limit", Type: "invalid_request_error"})
				return
			}
			model.WriteError(w, model.ErrBadRequest(err.Error()))
			return
		}
		if input.cleanup != nil {
			defer input.cleanup()
		}

		var response *http.Response
		h.route(w, req, input.model, input.sessionID, realtime, func(ctx context.Context, p provider.MediaProvider, actual string) error {
			body, contentType, err := input.open(actual)
			if err != nil {
				return err
			}
			defer body.Close()
			response, err = p.ForwardMedia(ctx, path, contentType, body, req.Header)
			return err
		}, func(ctx context.Context, _ string) (int, bool, error) {
			defer response.Body.Close()
			for _, name := range []string{"Content-Type", "Content-Disposition", "Location", "OpenAI-Request-ID"} {
				if value := response.Header.Get(name); value != "" {
					w.Header().Set(name, value)
				}
			}
			w.Header().Set("Cache-Control", "no-store")
			contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
			sse := contentType == "text/event-stream"
			audio := strings.HasPrefix(contentType, "audio/") || contentType == "application/octet-stream"
			var output io.Writer = w
			if sse || audio {
				w.Header().Set("X-Accel-Buffering", "no")
				output = mediaFlushWriter{w}
			}
			w.WriteHeader(response.StatusCode)
			if sse || audio {
				_ = http.NewResponseController(w).Flush()
			}
			if sse {
				return response.StatusCode, true, copyMediaEvents(ctx, output, response.Body)
			}
			if contentType == "application/json" {
				return response.StatusCode, false, copyMediaJSON(ctx, output, response.Body)
			}
			_, err := io.Copy(output, response.Body)
			return response.StatusCode, audio, err
		})
	}
}

func (h OpenAIMedia) route(w http.ResponseWriter, req *http.Request, alias, sessionID string, realtime bool,
	attempt func(context.Context, provider.MediaProvider, string) error,
	deliver func(context.Context, string) (int, bool, error),
) {
	ctx := req.Context()
	var keyID *int64
	var sticky string
	if ck := middleware.APIKeyFromContext(ctx); ck != nil {
		if len(ck.AllowedModels) > 0 && !slices.Contains(ck.AllowedModels, alias) {
			model.WriteError(w, model.ErrForbidden("model not allowed: "+alias))
			return
		}
		if realtime && ck.Key.MaxBudget != nil {
			model.WriteError(w, model.ErrForbidden("Realtime sessions require an API key without a spend cap"))
			return
		}
		keyID = &ck.Key.ID
		sticky = ck.Key.KeyHash
	}

	ctx, operation := beginOperation(ctx, h.Tracer, "openai_media", alias, realtime, sessionID)
	defer operation.end()
	ctx, acc := startAccounting(ctx, w, req, h.Store, h.Spend, h.CostMap, h.Router, alias, req.URL.Path, sessionID, keyID, true)
	defer acc.close()
	options := acc.options(1)
	options.Eligible = func(d *provider.Deployment) bool {
		_, supported := d.Provider.(provider.MediaProvider)
		return supported && d.Decision == nil
	}
	var pool string
	before := options.BeforeAttempt
	options.BeforeAttempt = func(d *provider.Deployment, name string) {
		before(d, name)
		pool = name
		accounting.SkipEstimate(acc.attemptCtx, "local pricing does not cover this media operation")
	}
	result := h.Router.Route(ctx, alias, sticky, func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
		if realtime && h.Router.BudgetManager() != nil {
			if _, capped := h.Router.BudgetManager().GetCap(pool); capped {
				return nil, nil, provider.NewHTTPError("openai", http.StatusUnprocessableEntity, nil, nil, errRealtimeBudget)
			}
		}
		err := attempt(acc.attemptCtx, d.Provider.(provider.MediaProvider), d.ActualModel)
		if err != nil {
			operation.failedAttempt(d, err)
		}
		return nil, nil, err
	}, options)

	emitRoutingEvents(h.Webhooks, h.Router, result, alias, middleware.RequestIDFromContext(ctx))
	router.SetRouteHeaders(w, result)
	if result.Error != nil {
		acc.err = result.Error
		acc.streaming = false
		acc.complete()
		operation.fail(result.Error)
		model.WriteError(w, mediaRouteError(result))
		return
	}
	acc.responseStatus, acc.streaming, acc.err = deliver(acc.attemptCtx, result.DeploymentUsed.ActualModel)
	if acc.err == nil {
		h.Router.ReportSuccess(result.DeploymentUsed)
	} else {
		operation.fail(acc.err)
	}
	cost := acc.cost(nil)
	operation.finish(result, nil, cost, acc.hasCost())
}

func mediaRouteError(result *router.RouteResult) *model.ProxyError {
	if errors.Is(result.Error, errRealtimeBudget) {
		return model.ErrForbidden(errRealtimeBudget.Error())
	}
	if slices.Contains(result.FailoverReasons, router.FailoverBudgetExhausted) {
		return model.ErrBudgetExceeded("budget exhausted for all available pools")
	}
	var proxyErr *model.ProxyError
	if errors.As(result.Error, &proxyErr) {
		return proxyErr
	}
	return model.ErrProviderError("openai", result.Error)
}

type mediaFlushWriter struct{ http.ResponseWriter }

func (w mediaFlushWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if err == nil {
		err = http.NewResponseController(w.ResponseWriter).Flush()
	}
	return n, err
}

// Limit accounting inspection, while forwarding even large images unchanged.
const mediaUsageLimit = 8 << 20

func copyMediaJSON(ctx context.Context, output io.Writer, upstream io.Reader) error {
	var sample bytes.Buffer
	n, err := io.Copy(io.MultiWriter(output, &sample), io.LimitReader(upstream, mediaUsageLimit+1))
	if err != nil {
		return err
	}
	if n <= mediaUsageLimit {
		accounting.ObserveJSON(ctx, sample.Bytes())
	}
	_, err = io.Copy(output, upstream)
	return err
}

func copyMediaEvents(ctx context.Context, output io.Writer, upstream io.Reader) error {
	reader := bufio.NewReader(upstream)
	var frame bytes.Buffer
	skipping := false
	continued := false
	for {
		line, err := reader.ReadSlice('\n')
		if len(line) > 0 {
			if _, writeErr := output.Write(line); writeErr != nil {
				return writeErr
			}
		}
		blank := !continued && (bytes.Equal(line, []byte("\n")) || bytes.Equal(line, []byte("\r\n")))
		continued = err == bufio.ErrBufferFull
		if !skipping {
			if len(line) <= mediaUsageLimit-frame.Len() {
				frame.Write(line)
			} else {
				skipping = true
			}
		}
		if blank || err == io.EOF {
			if !skipping {
				_, data, readErr := provider.NewSSEReader(&frame).Next()
				if readErr == nil {
					accounting.ObserveJSON(ctx, data)
				}
			}
			frame.Reset()
			skipping = false
		}
		if err == io.EOF {
			return nil
		}
		if err != nil && !continued {
			return err
		}
	}
}
