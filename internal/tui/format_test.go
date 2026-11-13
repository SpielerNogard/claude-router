package tui

import (
	"testing"
	"time"

	"github.com/SpielerNogard/claude-router/internal/proxy"
	"github.com/SpielerNogard/claude-router/internal/store"
)

func f(v float64) *float64 { return &v }

func TestCompact(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 950: "950", 12_400: "12.4k", 1_234_567: "1.2M"} {
		if got := compact(in); got != want {
			t.Errorf("compact(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestUSD(t *testing.T) {
	cases := []struct {
		in   *float64
		want string
	}{{nil, "—"}, {f(3.12), "$3.12"}, {f(0.004), "$0.004"}, {f(0), "$0.00"}}
	for _, c := range cases {
		if got := usd(c.in); got != c.want {
			t.Errorf("usd = %q, want %q", got, c.want)
		}
	}
}

func TestHitRate(t *testing.T) {
	if got := hitRate(store.Totals{InputTokens: 100, CacheRead: 300, CacheWrite: 100}); got != "60%" {
		t.Errorf("hitRate = %q, want 60%%", got)
	}
	if got := hitRate(store.Totals{}); got != "—" {
		t.Errorf("hitRate(empty) = %q, want —", got)
	}
}

func TestLevel(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	cases := []struct {
		w    proxy.LimitWindow
		want string
	}{
		{proxy.LimitWindow{Utilization: 0.1, Reset: now.Add(-time.Minute)}, "stale"},
		{proxy.LimitWindow{Utilization: 1.0, Reset: future}, "bad"},
		{proxy.LimitWindow{Utilization: 0.1, Reset: future, Status: "rejected"}, "bad"},
		{proxy.LimitWindow{Utilization: 0.8, Reset: future}, "warn"},
		{proxy.LimitWindow{Utilization: 0.42, Reset: future}, "ok"},
	}
	for _, c := range cases {
		if got := level(c.w, now); got != c.want {
			t.Errorf("level(%+v) = %q, want %q", c.w, got, c.want)
		}
	}
}

func TestProviderStatus(t *testing.T) {
	time.Local = time.UTC
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	if got := providerStatus(proxy.ProviderLimitView{Kind: "window"}, "2026-11-13T14:05:00Z", now); got != "blocked until 14:05" {
		t.Errorf("blocked: %q", got)
	}
	v := proxy.ProviderLimitView{Kind: "window", ProviderLimits: proxy.ProviderLimits{Windows: map[string]proxy.LimitWindow{
		"5h": {Utilization: 0.85, Reset: future}, "7d": {Utilization: 0.1, Reset: future},
	}}}
	if got := providerStatus(v, "", now); got != "warn" {
		t.Errorf("worst window: %q, want warn", got)
	}
	if got := providerStatus(proxy.ProviderLimitView{Kind: "unlimited"}, "", now); got != "ok" {
		t.Errorf("unlimited: %q, want ok", got)
	}
	stale := proxy.ProviderLimitView{Kind: "window", ProviderLimits: proxy.ProviderLimits{Windows: map[string]proxy.LimitWindow{
		"5h": {Utilization: 1, Reset: now.Add(-time.Minute)},
	}}}
	if got := providerStatus(stale, "", now); got != "ok" {
		t.Errorf("stale only: %q, want ok", got)
	}
}
