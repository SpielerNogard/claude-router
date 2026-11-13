package proxy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/SpielerNogard/claude-router/internal/alias"
	"github.com/SpielerNogard/claude-router/internal/config"
)

type Target struct {
	Provider      config.Provider
	UpstreamModel string
	Alias         string
}

type ResolveError struct {
	Status  int
	Kind    string
	Message string
}

func (e *ResolveError) Error() string { return e.Message }

func Resolve(cfg *config.Config, model string) (*Target, *ResolveError) {
	if model == "" {
		return nil, &ResolveError{400, "invalid_request_error", "model missing from request"}
	}
	// Claude Code's picker returns the alias in its encoded form.
	if decoded, ok := alias.Decode(model); ok {
		model = decoded
	}
	// A colon alone does not make an alias: model names like llama3:8b contain
	// one too. Only a configured provider before it counts.
	if name, upstream, found := strings.Cut(model, ":"); found {
		if p, ok := cfg.Providers[name]; ok {
			if upstream == "" {
				return nil, &ResolveError{400, "invalid_request_error",
					fmt.Sprintf("model %q: model name missing after the colon", model)}
			}
			return &Target{Provider: p, UpstreamModel: upstream, Alias: model}, nil
		}
	}
	if p, ok := defaultProvider(cfg); ok {
		return &Target{Provider: p, UpstreamModel: model, Alias: p.Name + ":" + model}, nil
	}
	return nil, &ResolveError{404, "not_found_error",
		fmt.Sprintf("model %q does not match any provider. Available: %s. "+
			"Set default = true on a provider to route unprefixed names there.",
			model, providerNames(cfg))}
}

func defaultProvider(cfg *config.Config) (config.Provider, bool) {
	for _, p := range cfg.Providers {
		if p.Default {
			return p, true
		}
	}
	return config.Provider{}, false
}

func providerNames(cfg *config.Config) string {
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
