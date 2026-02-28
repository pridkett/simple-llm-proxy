package router

import (
	"math/rand"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// Shuffle implements a simple random selection strategy.
type Shuffle struct{}

// NewShuffle creates a new shuffle strategy.
func NewShuffle() *Shuffle {
	return &Shuffle{}
}

// Select randomly selects a deployment.
func (s *Shuffle) Select(deployments []*provider.Deployment) *provider.Deployment {
	if len(deployments) == 0 {
		return nil
	}
	if len(deployments) == 1 {
		return deployments[0]
	}
	return deployments[rand.Intn(len(deployments))]
}
