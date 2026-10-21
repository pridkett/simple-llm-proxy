package config

import (
	"fmt"
	"math"
	"net/url"
	"slices"
)

// NormalizeDecision validates both parsed YAML and programmatically built config.
// It returns a copy so router construction/reload never mutates a live config.
func NormalizeDecision(d *DecisionConfig) (*DecisionConfig, error) {
	if d == nil {
		return nil, nil
	}
	c := *d
	if !slices.Contains([]string{"decisions", "systemone", "cloudflare"}, c.Protocol) {
		return nil, fmt.Errorf("decision.protocol must be decisions, systemone, or cloudflare")
	}
	if c.Path != "" {
		u, err := url.Parse(c.Path)
		if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || c.Path[0] != '/' {
			return nil, fmt.Errorf("decision.path must be an absolute path without a host, query, or fragment")
		}
	}
	if len(c.Modalities) == 0 {
		c.Modalities = []string{"text"}
	} else {
		c.Modalities = slices.Clone(c.Modalities)
	}
	if len(c.QuestionTypes) == 0 {
		c.QuestionTypes = []string{"predicate", "choice", "score"}
	} else {
		c.QuestionTypes = slices.Clone(c.QuestionTypes)
	}
	for _, m := range c.Modalities {
		if !slices.Contains([]string{"text", "image", "audio", "video"}, m) {
			return nil, fmt.Errorf("unknown decision modality %q", m)
		}
	}
	for _, q := range c.QuestionTypes {
		if !slices.Contains([]string{"predicate", "choice", "score", "box"}, q) {
			return nil, fmt.Errorf("unknown decision question type %q (use predicate for noul)", q)
		}
	}
	if c.Pricing == nil {
		return nil, fmt.Errorf("decision.pricing is required; use {} for free/self-hosted inference")
	}
	p := *c.Pricing
	c.Pricing = &p
	rates := []float64{p.InputCostPerToken, p.OutputCostPerToken, p.CostPerRequest}
	if p.CacheReadInputTokenCost != nil {
		v := *p.CacheReadInputTokenCost
		p.CacheReadInputTokenCost = &v
		rates = append(rates, v)
	}
	if p.CacheCreationInputTokenCost != nil {
		v := *p.CacheCreationInputTokenCost
		p.CacheCreationInputTokenCost = &v
		rates = append(rates, v)
	}
	for _, rate := range rates {
		if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return nil, fmt.Errorf("decision prices must be finite non-negative USD amounts")
		}
	}
	return &c, nil
}
