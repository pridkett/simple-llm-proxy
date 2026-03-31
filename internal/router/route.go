package router

import (
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
