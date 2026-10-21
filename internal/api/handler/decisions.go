package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/pwagstro/simple_llm_proxy/internal/api/middleware"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
	decisionprovider "github.com/pwagstro/simple_llm_proxy/internal/provider/decision"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/webhook"
	"go.opentelemetry.io/otel/trace"
)

// Decisions serves both decision dialects. KeyAuth supplies rate/budget checks;
// this handler enforces the coarse permission plus the requested alias allowlist.
func Decisions(r *router.Router, store storage.Storage, sa *keystore.SpendAccumulator, dispatcher *webhook.WebhookDispatcher, getCfg func() config.GeneralSettings, format string, tracer trace.Tracer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		start := time.Now()
		ck := middleware.APIKeyFromContext(ctx)
		if ck != nil && !ck.Key.AllowDecisions {
			model.WriteError(w, model.ErrForbidden("decision models are not enabled for this API key"))
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, decisionprovider.MaxBodyBytes)
		var fields map[string]json.RawMessage
		sessionID, err := decodeSessionRequest(req, &fields, true)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				model.WriteError(w, &model.ProxyError{StatusCode: http.StatusRequestEntityTooLarge, Message: "decision request exceeds size limit", Type: "invalid_request_error"})
				return
			}
			model.WriteError(w, model.ErrBadRequest("invalid request body: "+err.Error()))
			return
		}
		decisionReq, err := model.ParseDecisionRequest(fields, format)
		if err != nil {
			model.WriteError(w, model.ErrBadRequest(err.Error()))
			return
		}
		if ck != nil && len(ck.AllowedModels) > 0 && !slices.Contains(ck.AllowedModels, decisionReq.Model) {
			model.WriteError(w, model.ErrForbidden("model not allowed: "+decisionReq.Model))
			return
		}
		deployments, exists := r.DecisionDeployments(decisionReq.Model)
		if !exists {
			model.WriteError(w, model.ErrModelNotFound(decisionReq.Model))
			return
		}
		if len(deployments) == 0 {
			model.WriteError(w, model.ErrBadRequest("model is not declared as a decision model"))
			return
		}
		// Preflight candidates without consuming RPM or cooling down incompatible
		// providers. The same predicate is evaluated during routing after reloads.
		eligible := func(d *provider.Deployment) bool {
			if d.Decision == nil {
				return false
			}
			_, err := decisionprovider.Prepare(decisionReq, d.Decision, d.ActualModel)
			return err == nil
		}
		compatible := false
		var lastErr error
		for _, d := range deployments {
			if _, err := decisionprovider.Prepare(decisionReq, d.Decision, d.ActualModel); err == nil {
				compatible = true
				break
			} else {
				lastErr = err
			}
		}
		if !compatible {
			model.WriteError(w, model.ErrBadRequest("no compatible decision deployment: "+lastErr.Error()))
			return
		}
		var keyID *int64
		sticky := ""
		if ck != nil {
			id := ck.Key.ID
			keyID = &id
			sticky = ck.Key.KeyHash
		}
		ctx, operation := beginOperation(ctx, tracer, "decision", decisionReq.Model, false)
		defer operation.end()
		var response *model.DecisionResponse
		result := r.Route(ctx, decisionReq.Model, sticky, func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error) {
			p, ok := d.Provider.(provider.DecisionsProvider)
			if !ok {
				return nil, nil, fmt.Errorf("deployment lacks decision provider")
			}
			copy := *decisionReq
			copy.Model = d.ActualModel
			var err error
			response, err = p.Decide(ctx, &copy)
			if err != nil {
				operation.failedAttempt(d, err)
				return nil, nil, err
			}
			return &model.ChatCompletionResponse{Usage: response.Usage}, nil, nil
		}, router.RouteOptions{EstimatedTokens: decisionReq.EstimatedTokens(), Eligible: eligible})
		requestID := middleware.RequestIDFromContext(ctx)
		emitRoutingEvents(dispatcher, r, result, decisionReq.Model, requestID)
		cfg := getCfg()
		var snippet *string
		if cfg.BodySnippetLimit > 0 {
			value := middleware.ReqBodySnippetFromContext(ctx)
			snippet = &value
		}
		d := result.DeploymentUsed
		if d == nil && len(result.DeploymentsTried) > 0 {
			d = result.DeploymentsTried[len(result.DeploymentsTried)-1]
		}
		if result.Error != nil {
			operation.finish(result, nil, 0)
			operation.fail(result.Error)
			err := model.ErrProviderError("decision", result.Error)
			var proxyErr *model.ProxyError
			if errors.As(result.Error, &proxyErr) {
				err = proxyErr
			}
			for _, reason := range result.FailoverReasons {
				if reason == router.FailoverBudgetExhausted {
					err = model.ErrBudgetExceeded("budget exhausted for all available decision pools")
					break
				}
			}
			if d == nil {
				d = &provider.Deployment{ModelName: decisionReq.Model}
			}
			if store != nil {
				logged := *d
				logged.ModelName = decisionReq.Model
				logRequest(logRequestParams{Store: store, Deployment: &logged, Endpoint: "/v1/" + format, APIKeyID: keyID, Status: err.StatusCode, StartTime: start, RequestID: requestID, SessionID: sessionID, PoolName: result.PoolName, CostKnown: true, ReqBodySnippet: snippet})
			}
			model.WriteError(w, err)
			return
		}
		r.ReportSuccess(d)
		cost, known := decisionCost(d.Decision.Pricing, response.Usage)
		operation.finish(result, response.Usage, cost)
		// Spend is credited synchronously before returning so the next call sees
		// the new key/pool balance. LogRequest must not credit it a second time.
		if keyID != nil && sa != nil && cost > 0 {
			sa.Credit(*keyID, cost)
		}
		if result.PoolName != "" && cost > 0 {
			r.BudgetManager().Credit(result.PoolName, cost)
		}
		if store != nil {
			logged := *d
			logged.ModelName = decisionReq.Model
			body, _ := json.Marshal(response.Fields)
			logRequest(logRequestParams{Store: store, Deployment: &logged, Endpoint: "/v1/" + format, APIKeyID: keyID, Usage: response.Usage, TotalCost: cost, CostKnown: true, Status: http.StatusOK, StartTime: start, RequestID: requestID, SessionID: sessionID, PoolName: result.PoolName, ReqBodySnippet: snippet, RespBodySnippet: decisionSnippet(body, cfg.BodySnippetLimit)})
		}
		router.SetRouteHeaders(w, result)
		if known {
			w.Header().Set("X-Request-Cost", strconv.FormatFloat(cost, 'g', -1, 64))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response.Fields)
	}
}

func decisionCost(p *config.DecisionPricing, usage *model.Usage) (float64, bool) {
	cost := p.CostPerRequest
	read, write := p.InputCostPerToken, p.InputCostPerToken
	if p.CacheReadInputTokenCost != nil {
		read = *p.CacheReadInputTokenCost
	}
	if p.CacheCreationInputTokenCost != nil {
		write = *p.CacheCreationInputTokenCost
	}
	if usage == nil {
		return cost, p.InputCostPerToken == 0 && p.OutputCostPerToken == 0 && read == 0 && write == 0
	}
	return cost + float64(usage.PromptTokens)*p.InputCostPerToken + float64(usage.CompletionTokens)*p.OutputCostPerToken + float64(usage.CacheReadTokens)*read + float64(usage.CacheWriteTokens)*write, true
}

func decisionSnippet(body []byte, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(body) <= limit {
		return string(body)
	}
	for limit > 0 && !utf8.RuneStart(body[limit]) {
		limit--
	}
	return string(body[:limit])
}
