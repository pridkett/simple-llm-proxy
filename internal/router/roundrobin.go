package router

import (
	"sync/atomic"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// RoundRobin implements a round-robin selection strategy.
type RoundRobin struct {
	counter uint64
}

// NewRoundRobin creates a new round-robin strategy.
func NewRoundRobin() *RoundRobin {
	return &RoundRobin{}
}

// Select selects the next deployment in round-robin fashion.
func (r *RoundRobin) Select(deployments []*provider.Deployment) *provider.Deployment {
	if len(deployments) == 0 {
		return nil
	}
	if len(deployments) == 1 {
		return deployments[0]
	}
	idx := atomic.AddUint64(&r.counter, 1) % uint64(len(deployments))
	return deployments[idx]
}
