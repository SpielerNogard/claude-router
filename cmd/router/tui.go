package main

import (
	"github.com/SpielerNogard/claude-router/internal/proxy"
	"github.com/SpielerNogard/claude-router/internal/store"
)

// --debug writes straight to stderr and would corrupt the screen, so it implies plain logs.
func useTUI(noTUI, debug, stdinTTY, stdoutTTY bool) bool {
	return !noTUI && !debug && stdinTTY && stdoutTTY
}

type tuiSource struct {
	*store.Store
	proxy *proxy.Proxy
}

func (s tuiSource) Limits() map[string]proxy.ProviderLimitView { return s.proxy.ProviderLimits() }
func (s tuiSource) Blocked() map[string]string                 { return s.proxy.BlockedProviders() }
