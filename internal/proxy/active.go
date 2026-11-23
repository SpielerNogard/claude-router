package proxy

import (
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/SpielerNogard/claude-router/internal/config"
)

// Accounts lists the default provider and the providers its chain reaches with the same model.
func Accounts(cfg *config.Config) []string {
	p, ok := defaultProvider(cfg)
	if !ok {
		return nil
	}
	accounts := []string{p.Name}
	// Validate rules out cycles, so this terminates.
	for next := p.Fallback; next != "" && !strings.Contains(next, ":"); next = cfg.Providers[next].Fallback {
		accounts = append(accounts, next)
	}
	return accounts
}

// Active returns the account serving unprefixed requests and all selectable accounts.
func (px *Proxy) Active() (string, []string) {
	accounts := Accounts(px.cfg())
	px.activeMu.Lock()
	defer px.activeMu.Unlock()
	if slices.Contains(accounts, px.active) {
		return px.active, accounts
	}
	if len(accounts) == 0 {
		return "", nil
	}
	return accounts[0], accounts
}

func (px *Proxy) SetActive(name string) error {
	if !slices.Contains(Accounts(px.cfg()), name) {
		return fmt.Errorf("unknown account %q", name)
	}
	px.activeMu.Lock()
	px.active = name
	px.activeMu.Unlock()
	log.Printf("active account: %s", name)
	return nil
}

// config is the registry's config with the active account moved to the front of the default chain.
func (px *Proxy) config() *config.Config {
	cfg := px.cfg()
	px.activeMu.Lock()
	defer px.activeMu.Unlock()
	accounts := Accounts(cfg)
	i := slices.Index(accounts, px.active)
	if i <= 0 {
		return cfg
	}
	if px.derivedFrom == cfg && px.derivedFor == px.active {
		return px.derived
	}
	order := append([]string{px.active}, slices.Delete(slices.Clone(accounts), i, i+1)...)
	tail := cfg.Providers[accounts[len(accounts)-1]].Fallback
	derived := *cfg
	derived.Providers = make(map[string]config.Provider, len(cfg.Providers))
	for name, p := range cfg.Providers {
		derived.Providers[name] = p
	}
	for j, name := range order {
		p := derived.Providers[name]
		p.Default = j == 0
		p.Fallback = tail
		if j < len(order)-1 {
			p.Fallback = order[j+1]
		}
		derived.Providers[name] = p
	}
	px.derivedFrom, px.derivedFor, px.derived = cfg, px.active, &derived
	return &derived
}
