package config

import (
	"context"
	"sync/atomic"
	"time"
)

// Reloader provides thread-safe config loading and hot-reloading.
type Reloader struct {
	path               string
	ptr                atomic.Pointer[Config]
	discoveryProviders []DiscoveryProvider
}

// NewReloader creates a Reloader that loads the config from path.
func NewReloader(path string) (*Reloader, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	r := &Reloader{path: path}
	r.ptr.Store(cfg)
	return r, nil
}

// SetDiscoveryProviders registers discovery providers for wildcard expansion.
// Must be called before ExpandWildcardsOnConfig or Reload to take effect.
func (r *Reloader) SetDiscoveryProviders(providers []DiscoveryProvider) {
	r.discoveryProviders = providers
}

// ExpandWildcardsOnConfig runs wildcard expansion on the currently loaded
// config using the registered discovery providers. This should be called
// once at startup after SetDiscoveryProviders and before the router is
// created.
func (r *Reloader) ExpandWildcardsOnConfig(ctx context.Context) error {
	cfg := r.ptr.Load()
	if err := ExpandWildcards(ctx, cfg, r.discoveryProviders); err != nil {
		return err
	}
	r.ptr.Store(cfg)
	return nil
}

// Config returns the current config.
func (r *Reloader) Config() *Config {
	return r.ptr.Load()
}

// Reload re-reads the config file and atomically replaces the current config.
// If discovery providers are registered, wildcard entries are expanded before
// the new config takes effect.
// Returns the new config on success.
func (r *Reloader) Reload() (*Config, error) {
	cfg, err := Load(r.path)
	if err != nil {
		return nil, err
	}

	// Expand wildcards if any discovery providers are registered.
	if len(r.discoveryProviders) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := ExpandWildcards(ctx, cfg, r.discoveryProviders); err != nil {
			return nil, err
		}
	}

	r.ptr.Store(cfg)
	return cfg, nil
}
