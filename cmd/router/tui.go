package main

import (
	"context"
	"log"

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

// runForeground blocks while the router serves; only a deliberate quit in the UI ends it early, a broken UI must not take the proxy down.
func runForeground(ctx context.Context, ui func(context.Context) error) {
	if err := ui(ctx); err != nil {
		log.Printf("terminal UI: %v — continuing with plain logs", err)
		<-ctx.Done()
	}
}
