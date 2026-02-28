package router

import (
	"sync"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/provider"
)

// CooldownManager tracks deployment failures and manages cooldowns.
type CooldownManager struct {
	mu           sync.RWMutex
	failures     map[*provider.Deployment]int
	cooldowns    map[*provider.Deployment]time.Time
	cooldownTime time.Duration
	allowedFails int
}

// NewCooldownManager creates a new cooldown manager.
func NewCooldownManager(cooldownTime time.Duration, allowedFails int) *CooldownManager {
	return &CooldownManager{
		failures:     make(map[*provider.Deployment]int),
		cooldowns:    make(map[*provider.Deployment]time.Time),
		cooldownTime: cooldownTime,
		allowedFails: allowedFails,
	}
}

// InCooldown returns true if the deployment is currently in cooldown.
func (c *CooldownManager) InCooldown(d *provider.Deployment) bool {
	c.mu.RLock()
	cooldownUntil, ok := c.cooldowns[d]
	c.mu.RUnlock()

	if !ok {
		return false
	}

	if time.Now().After(cooldownUntil) {
		// Cooldown expired, remove it
		c.mu.Lock()
		delete(c.cooldowns, d)
		c.failures[d] = 0
		c.mu.Unlock()
		return false
	}

	return true
}

// ReportSuccess resets the failure count for a deployment.
func (c *CooldownManager) ReportSuccess(d *provider.Deployment) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures[d] = 0
	delete(c.cooldowns, d)
}

// ReportFailure increments the failure count and potentially starts cooldown.
func (c *CooldownManager) ReportFailure(d *provider.Deployment) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.failures[d]++
	if c.failures[d] >= c.allowedFails {
		c.cooldowns[d] = time.Now().Add(c.cooldownTime)
	}
}

// DeploymentStatus holds the current runtime status of a deployment.
type DeploymentStatus struct {
	FailureCount  int
	InCooldown    bool
	CooldownUntil time.Time
}

// GetStatus returns the current status for a deployment.
func (c *CooldownManager) GetStatus(d *provider.Deployment) DeploymentStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cooldownUntil, hasCooldown := c.cooldowns[d]
	inCooldown := hasCooldown && time.Now().Before(cooldownUntil)

	return DeploymentStatus{
		FailureCount:  c.failures[d],
		InCooldown:    inCooldown,
		CooldownUntil: cooldownUntil,
	}
}
