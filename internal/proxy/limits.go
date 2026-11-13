package proxy

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// The windows Anthropic reports for subscriptions; the UI ignores other claims (e.g. per model).
var limitWindows = []string{"5h", "7d"}

type LimitWindow struct {
	Utilization float64   `json:"utilization"`
	Reset       time.Time `json:"reset"`
	Status      string    `json:"status"`
}

type ProviderLimits struct {
	MeasuredAt *time.Time             `json:"measured_at,omitempty"`
	Windows    map[string]LimitWindow `json:"windows,omitempty"`
}

type limits struct {
	mu sync.Mutex
	m  map[string]ProviderLimits
}

func newLimits() *limits { return &limits{m: map[string]ProviderLimits{}} }

// Without unified-status the limiter did not run; Claude Code ignores such responses too.
func (l *limits) note(provider string, h http.Header, now time.Time) {
	if h.Get(limitHeaderPrefix+"status") == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cur := l.m[provider]
	windows := map[string]LimitWindow{}
	for k, v := range cur.Windows {
		windows[k] = v
	}
	for _, name := range limitWindows {
		u, err := strconv.ParseFloat(h.Get(limitHeaderPrefix+name+"-utilization"), 64)
		if err != nil {
			continue
		}
		w := LimitWindow{Utilization: u, Status: h.Get(limitHeaderPrefix + name + "-status")}
		if sec, ok := headerSeconds(h.Get(limitHeaderPrefix + name + "-reset")); ok {
			w.Reset = time.Unix(sec, 0)
		}
		windows[name] = w
	}
	l.m[provider] = ProviderLimits{MeasuredAt: &now, Windows: windows}
}

func (l *limits) snapshot() map[string]ProviderLimits {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]ProviderLimits, len(l.m))
	for k, v := range l.m {
		out[k] = v
	}
	return out
}

type ProviderLimitView struct {
	Kind string `json:"kind"`
	ProviderLimits
}

// Only Anthropic reports window limits; translated providers count as unlimited.
func (px *Proxy) ProviderLimits() map[string]ProviderLimitView {
	seen := px.limits.snapshot()
	out := map[string]ProviderLimitView{}
	for name, p := range px.cfg().Providers {
		if p.Format != "anthropic" {
			out[name] = ProviderLimitView{Kind: "unlimited"}
			continue
		}
		out[name] = ProviderLimitView{Kind: "window", ProviderLimits: seen[name]}
	}
	return out
}
