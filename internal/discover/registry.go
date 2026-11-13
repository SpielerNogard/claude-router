package discover

import (
	"context"
	"log"
	"net/http"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/store"
)

type Registry struct {
	cfgFn  func() *config.Config
	store  *store.Store
	client *http.Client

	mu     sync.RWMutex
	models map[string][]string

	probed     sync.Map // provider name → already probed via passthrough
	wg         sync.WaitGroup
	refreshing atomic.Bool
}

func NewRegistry(cfgFn func() *config.Config, s *store.Store) *Registry {
	r := &Registry{
		cfgFn: cfgFn, store: s,
		client: &http.Client{Timeout: 15 * time.Second},
		models: map[string][]string{},
	}
	if saved, err := s.Models(); err == nil {
		r.models = saved
	}
	return r
}

// Static lists from the config take precedence over anything discovered.
func (r *Registry) Aliases() []string {
	cfg := r.cfgFn()
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []string
	for name, p := range cfg.Providers {
		models := p.Models
		if len(models) == 0 {
			models = r.models[name]
		}
		for _, m := range Filter(models, p.Include, p.Exclude) {
			out = append(out, name+":"+m)
		}
		for _, m := range p.ExtraModels {
			out = append(out, name+":"+m)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

func (r *Registry) RefreshKeyed() {
	if !r.refreshing.CompareAndSwap(false, true) {
		return
	}
	defer r.refreshing.Store(false)
	for name, p := range r.cfgFn().Providers {
		// Only passthrough needs a credential from an in-flight request (see NoteCredential); the other auth modes have their own.
		if p.Auth == "passthrough" || len(p.Models) > 0 {
			continue
		}
		r.discover(name, p, http.Header{})
	}
}

// For passthrough providers an in-flight request is the only source of a valid credential; it is used once and discarded.
func (r *Registry) NoteCredential(provider string, h http.Header) {
	p, ok := r.cfgFn().Providers[provider]
	if !ok || p.Auth != "passthrough" || len(p.Models) > 0 {
		return
	}
	if _, seen := r.probed.LoadOrStore(provider, true); seen {
		return
	}
	auth := h.Clone()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		// A network hiccup on the first attempt must not lock the provider out forever.
		if err := r.discover(provider, p, auth); err != nil {
			r.probed.Delete(provider)
		}
	}()
}

func (r *Registry) discover(name string, p config.Provider, auth http.Header) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := Discover(ctx, r.client, p, auth)
	if err != nil {
		log.Printf("discovery %s: %v", name, err)
		return err
	}
	filtered := Filter(models, p.Include, p.Exclude)
	r.mu.Lock()
	r.models[name] = filtered
	r.mu.Unlock()
	if err := r.store.SaveModels(name, filtered); err != nil {
		log.Printf("discovery %s save: %v", name, err)
	}
	return nil
}

// test only
func (r *Registry) wait() { r.wg.Wait() }
