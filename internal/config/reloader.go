package config

import "sync/atomic"

// Reloader provides thread-safe config loading and hot-reloading.
type Reloader struct {
	path string
	ptr  atomic.Pointer[Config]
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

// Config returns the current config.
func (r *Reloader) Config() *Config {
	return r.ptr.Load()
}

// Reload re-reads the config file and atomically replaces the current config.
// Returns the new config on success.
func (r *Reloader) Reload() (*Config, error) {
	cfg, err := Load(r.path)
	if err != nil {
		return nil, err
	}
	r.ptr.Store(cfg)
	return cfg, nil
}
