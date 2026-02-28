package provider

import (
	"fmt"
	"sync"
)

// Factory creates a new provider instance.
type Factory func(apiKey, apiBase string) Provider

// Registry manages provider factories.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry creates a new provider registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
	}
}

// Register registers a provider factory.
func (r *Registry) Register(name string, factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// Get returns a provider instance for the given name.
func (r *Registry) Get(name, apiKey, apiBase string) (Provider, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", name)
	}

	return factory(apiKey, apiBase), nil
}

// Has returns true if the provider is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[name]
	return ok
}

// DefaultRegistry is the global provider registry.
var DefaultRegistry = NewRegistry()

// Register registers a provider factory in the default registry.
func Register(name string, factory Factory) {
	DefaultRegistry.Register(name, factory)
}

// Get returns a provider from the default registry.
func Get(name, apiKey, apiBase string) (Provider, error) {
	return DefaultRegistry.Get(name, apiKey, apiBase)
}
