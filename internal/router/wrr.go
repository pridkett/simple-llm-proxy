package router

import (
	"sync"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// WeightedRoundRobin implements the Nginx-style smooth weighted round-robin
// algorithm. Deployments are selected proportionally to their configured weights
// over time, producing a smooth interleaving rather than bursty batches.
//
// The algorithm:
//  1. For each deployment, add its configured weight to its currentWeight.
//  2. Select the deployment with the highest currentWeight.
//  3. Subtract totalWeight from the winner's currentWeight.
//
// This produces an even distribution where a deployment with weight W appears
// W/totalWeight fraction of the time.
type WeightedRoundRobin struct {
	mu            sync.Mutex
	currentWeight map[string]int // DeploymentKey() -> running current weight
	weights       map[string]int // DeploymentKey() -> configured weight
}

// Name returns the strategy name.
func (w *WeightedRoundRobin) Name() string { return "weighted-round-robin" }

// NewWeightedRoundRobin creates a new WeightedRoundRobin strategy.
// The weights map uses DeploymentKey() as keys. Keys not present in the map
// default to weight 1 during selection.
func NewWeightedRoundRobin(weights map[string]int) *WeightedRoundRobin {
	w := make(map[string]int, len(weights))
	for k, v := range weights {
		w[k] = v
	}
	return &WeightedRoundRobin{
		currentWeight: make(map[string]int),
		weights:       w,
	}
}

// Select chooses the next deployment using smooth weighted round-robin.
// Only the deployments passed in (healthy ones) are considered.
// Returns nil if the slice is empty.
func (w *WeightedRoundRobin) Select(deployments []*provider.Deployment) *provider.Deployment {
	if len(deployments) == 0 {
		return nil
	}
	if len(deployments) == 1 {
		return deployments[0]
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Step 1: Compute totalWeight and add configured weights to currentWeight.
	totalWeight := 0
	for _, d := range deployments {
		key := d.DeploymentKey()
		cw := w.weightFor(key)
		totalWeight += cw
		w.currentWeight[key] += cw
	}

	// Step 2: Select the deployment with the highest currentWeight.
	var best *provider.Deployment
	bestWeight := 0
	for i, d := range deployments {
		key := d.DeploymentKey()
		cw := w.currentWeight[key]
		if i == 0 || cw > bestWeight {
			best = d
			bestWeight = cw
		}
	}

	// Step 3: Subtract totalWeight from winner's currentWeight.
	if best != nil {
		key := best.DeploymentKey()
		w.currentWeight[key] -= totalWeight
	}

	return best
}

// weightFor returns the configured weight for a deployment key, defaulting to 1.
func (w *WeightedRoundRobin) weightFor(key string) int {
	if wt, ok := w.weights[key]; ok {
		return wt
	}
	return 1
}
