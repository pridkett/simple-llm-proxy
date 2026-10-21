package router

import "github.com/pwagstro/simple_llm_proxy/internal/provider"

// DecisionDeployments returns a snapshot of decision candidates reachable for an
// alias, including budget failover pools. Non-decision aliases cannot opt into
// decision operations merely by sharing a pool with a decision model.
func (r *Router) DecisionDeployments(name string) ([]*provider.Deployment, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	direct, exists := r.deployments[name]
	if !exists {
		return nil, false
	}
	var out []*provider.Deployment
	for _, d := range direct {
		if d.Decision != nil {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return out, true
	}
	seen := map[*provider.Deployment]bool{}
	for _, d := range out {
		seen[d] = true
	}
	for _, pool := range r.pools {
		serves := false
		for _, d := range pool.Members {
			if d.ModelName == name {
				serves = true
				break
			}
		}
		if serves {
			for _, d := range pool.Members {
				if d.Decision != nil && !seen[d] {
					seen[d] = true
					out = append(out, d)
				}
			}
		}
	}
	return out, true
}
