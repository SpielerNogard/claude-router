package config

import (
	"sync"
	"sync/atomic"
)

// Get returns a pointer that is never mutated, so an in-flight request finishes on a consistent snapshot.
type Registry struct {
	path string
	cur  atomic.Pointer[Config]
	mu   sync.Mutex // serializes writes; Get stays lock-free
}

func NewRegistry(path string, initial *Config) *Registry {
	r := &Registry{path: path}
	r.cur.Store(initial)
	return r
}

func (r *Registry) Get() *Config { return r.cur.Load() }

func (r *Registry) Path() string { return r.path }

func (r *Registry) Replace(c *Config) error {
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := WriteFile(r.path, c); err != nil {
		return err
	}
	r.cur.Store(c)
	return nil
}
