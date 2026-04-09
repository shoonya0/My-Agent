package connectors

import (
	"fmt"
	"sync"

	"myAgent/pkg/types"
)

// Registry holds a set of named PlatformConnectors keyed by platform name.
// It is safe for concurrent reads; registration is expected at startup only.
type Registry struct {
	mu         sync.RWMutex
	connectors map[string]types.PlatformConnector
}

// NewRegistry returns an empty connector registry.
func NewRegistry() *Registry {
	return &Registry{
		connectors: make(map[string]types.PlatformConnector),
	}
}

// Register adds a connector under the given platform name. Overwrites any
// previously registered connector for the same platform.
func (r *Registry) Register(platform string, c types.PlatformConnector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connectors[platform] = c
}

// Get returns the connector for the given platform or an error if none is
// registered.
func (r *Registry) Get(platform string) (types.PlatformConnector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.connectors[platform]
	if !ok {
		return nil, fmt.Errorf("connectors: no connector registered for platform %q", platform)
	}
	return c, nil
}

// Platforms returns the names of all registered platforms.
func (r *Registry) Platforms() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.connectors))
	for name := range r.connectors {
		names = append(names, name)
	}
	return names
}
