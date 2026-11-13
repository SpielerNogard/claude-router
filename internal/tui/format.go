package tui

import (
	"fmt"
	"time"

	"github.com/SpielerNogard/claude-router/internal/proxy"
	"github.com/SpielerNogard/claude-router/internal/store"
)

const dash = "—"

func compact(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprint(n)
}

// nil means unknown price; showing $0 would make a local model look free.
func usd(c *float64) string {
	switch {
	case c == nil:
		return dash
	case *c > 0 && *c < 0.01:
		return fmt.Sprintf("$%.3f", *c)
	}
	return fmt.Sprintf("$%.2f", *c)
}

func hitRate(t store.Totals) string {
	total := t.InputTokens + t.CacheRead + t.CacheWrite
	if total == 0 {
		return dash
	}
	return fmt.Sprintf("%.0f%%", float64(t.CacheRead)/float64(total)*100)
}

// Mirrors level() in web/src/lib/limits.ts.
func level(w proxy.LimitWindow, now time.Time) string {
	switch {
	case !w.Reset.After(now):
		return "stale"
	case w.Status == "rejected" || w.Utilization >= 1:
		return "bad"
	case w.Utilization >= 0.8:
		return "warn"
	}
	return "ok"
}

func providerStatus(v proxy.ProviderLimitView, blockedUntil string, now time.Time) string {
	if t, err := time.Parse(time.RFC3339, blockedUntil); err == nil {
		return "blocked until " + t.Local().Format("15:04")
	}
	rank := map[string]int{"ok": 0, "warn": 1, "bad": 2}
	worst := "ok"
	for _, w := range v.Windows {
		if l := level(w, now); rank[l] > rank[worst] {
			worst = l
		}
	}
	return worst
}
