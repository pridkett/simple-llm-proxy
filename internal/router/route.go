package router

import (
	"context"
	"errors"
	"fmt"

	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// FailoverReason describes why a deployment was skipped during routing.
type FailoverReason string

const (
	FailoverRateLimited     FailoverReason = "rate_limited"
	FailoverCooldown        FailoverReason = "cooldown"
	FailoverBackoff         FailoverReason = "backoff"
	FailoverError           FailoverReason = "error"
	FailoverBudgetExhausted FailoverReason = "budget_exhausted"
)

// RouteResult carries the outcome of a Route() call, including metadata
// for downstream response headers (X-Provider-Used, X-Providers-Tried).
type RouteResult struct {
	Response         *model.ChatCompletionResponse
	Stream           provider.Stream  // nil for non-streaming
	DeploymentUsed   *provider.Deployment
	DeploymentsTried []*provider.Deployment
	FailoverReasons  []FailoverReason
	Error            error
}

// RouteCallback is invoked by Route() for each deployment attempt.
// For streaming: return (nil, stream, nil). For non-streaming: return (resp, nil, nil).
type RouteCallback func(d *provider.Deployment) (*model.ChatCompletionResponse, provider.Stream, error)

// Route selects a deployment for the given model and invokes the callback.
// stickyKey is the SHA-256 hash of the API key for session affinity (empty string = no sticky).
// Plan 03 wires sticky session logic; until then stickyKey is accepted but unused.
//
// Route owns all retry/failover logic. The handler only needs to provide a callback
// that performs the actual provider call. Route does NOT call ReportSuccess — the
// handler calls it after confirming the full response was delivered (for streaming,
// at EOF per STREAM-01).
func (r *Router) Route(ctx context.Context, modelName string, stickyKey string, cb RouteCallback) *RouteResult {
	result := &RouteResult{}

	// Check context cancellation before starting.
	if ctx.Err() != nil {
		result.Error = ctx.Err()
		return result
	}

	// Check if this model belongs to a pool.
	r.mu.RLock()
	pool, hasPool := r.modelToPool[modelName]
	r.mu.RUnlock()

	if hasPool {
		return r.routePool(ctx, pool, cb, result)
	}
	return r.routeLegacy(ctx, modelName, cb, result)
}

// routePool routes through a pool's member deployments, trying each healthy
// member in the order determined by the pool's strategy until one succeeds.
func (r *Router) routePool(ctx context.Context, pool *Pool, cb RouteCallback, result *RouteResult) *RouteResult {
	// Build healthy member list: filter members not in cooldown and not in backoff.
	healthy := make([]*provider.Deployment, 0, len(pool.Members))
	for _, d := range pool.Members {
		if !r.cooldown.InCooldown(d) && !r.backoff.InBackoff(d.DeploymentKey()) {
			healthy = append(healthy, d)
		}
	}

	var lastErr error

	for len(healthy) > 0 {
		deployment := pool.Strategy.Select(healthy)
		if deployment == nil {
			break
		}

		result.DeploymentsTried = append(result.DeploymentsTried, deployment)

		// Check context cancellation before each attempt.
		if ctx.Err() != nil {
			result.Error = ctx.Err()
			return result
		}

		resp, stream, err := cb(deployment)
		if err == nil {
			result.Response = resp
			result.Stream = stream
			result.DeploymentUsed = deployment
			return result
		}

		lastErr = err

		var rlErr *provider.RateLimitError
		if errors.As(err, &rlErr) {
			r.ReportRateLimit(deployment, rlErr.RetryAfter)
			result.FailoverReasons = append(result.FailoverReasons, FailoverRateLimited)
		} else {
			r.ReportFailure(deployment)
			result.FailoverReasons = append(result.FailoverReasons, FailoverError)
		}

		// Remove the failed deployment from the healthy slice.
		healthy = removeDeployment(healthy, deployment)
	}

	// All pool members exhausted.
	result.Error = lastErr
	return result
}

// routeLegacy routes using the pre-pool GetDeploymentWithRetry path for
// backward compatibility with models not assigned to any pool.
func (r *Router) routeLegacy(ctx context.Context, modelName string, cb RouteCallback, result *RouteResult) *RouteResult {
	tried := make(map[*provider.Deployment]bool)
	var lastErr error

	for attempt := 0; attempt <= r.NumRetries(); attempt++ {
		d, err := r.GetDeploymentWithRetry(modelName, tried)
		if err != nil {
			if attempt == 0 {
				// First attempt — model doesn't exist.
				result.Error = fmt.Errorf("model not found: %s", modelName)
				return result
			}
			// All deployments tried.
			break
		}

		result.DeploymentsTried = append(result.DeploymentsTried, d)
		tried[d] = true

		// Check context cancellation before each attempt.
		if ctx.Err() != nil {
			result.Error = ctx.Err()
			return result
		}

		resp, stream, err := cb(d)
		if err == nil {
			result.Response = resp
			result.Stream = stream
			result.DeploymentUsed = d
			return result
		}

		lastErr = err

		var rlErr *provider.RateLimitError
		if errors.As(err, &rlErr) {
			r.ReportRateLimit(d, rlErr.RetryAfter)
			result.FailoverReasons = append(result.FailoverReasons, FailoverRateLimited)
		} else {
			r.ReportFailure(d)
			result.FailoverReasons = append(result.FailoverReasons, FailoverError)
		}
	}

	// All retries exhausted.
	result.Error = lastErr
	return result
}

// removeDeployment returns a new slice with the target deployment removed.
func removeDeployment(ds []*provider.Deployment, target *provider.Deployment) []*provider.Deployment {
	out := make([]*provider.Deployment, 0, len(ds)-1)
	for _, d := range ds {
		if d != target {
			out = append(out, d)
		}
	}
	return out
}
