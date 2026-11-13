package proxy

import (
	"net/http"
	"testing"
	"time"
)

func TestConfirmedLimit(t *testing.T) {
	now := time.Unix(1790330000, 0)
	reset := now.Add(2 * time.Hour)

	cases := map[string]struct {
		header map[string]string
		want   time.Time
		ok     bool
	}{
		"rejected with reset": {
			header: map[string]string{
				"anthropic-ratelimit-unified-status": "rejected",
				"anthropic-ratelimit-unified-reset":  "1790337200",
			},
			want: reset, ok: true,
		},
		"rejected without reset, but retry-after": {
			header: map[string]string{
				"anthropic-ratelimit-unified-status": "rejected",
				"retry-after":                        "120",
			},
			want: now.Add(120 * time.Second), ok: true,
		},
		"overage allowed": {
			header: map[string]string{
				"anthropic-ratelimit-unified-status":         "rejected",
				"anthropic-ratelimit-unified-overage-status": "allowed",
				"anthropic-ratelimit-unified-reset":          "1790337200",
			},
		},
		"overage also rejected": {
			header: map[string]string{
				"anthropic-ratelimit-unified-status":         "rejected",
				"anthropic-ratelimit-unified-overage-status": "rejected",
				"anthropic-ratelimit-unified-reset":          "1790337200",
			},
			want: reset, ok: true,
		},
		"status allowed": {
			header: map[string]string{
				"anthropic-ratelimit-unified-status": "allowed",
				"anthropic-ratelimit-unified-reset":  "1790337200",
			},
		},
		"nothing at all": {header: map[string]string{}},
		"reset unreadable": {
			header: map[string]string{
				"anthropic-ratelimit-unified-status": "rejected",
				"anthropic-ratelimit-unified-reset":  "morgen",
			},
		},
	}

	for name, c := range cases {
		h := http.Header{}
		for k, v := range c.header {
			h.Set(k, v)
		}
		got, ok := confirmedLimit(h, now)
		if ok != c.ok {
			t.Errorf("%s: ok = %v, want %v", name, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("%s: until = %v, want %v", name, got, c.want)
		}
	}
}

func TestConfirmedLimitClampsFarFutureReset(t *testing.T) {
	now := time.Unix(1790330000, 0)
	h := http.Header{}
	h.Set("anthropic-ratelimit-unified-status", "rejected")
	// Year 2099: such a value must not park the subscription permanently.
	h.Set("anthropic-ratelimit-unified-reset", "4102444800")

	got, ok := confirmedLimit(h, now)
	if !ok {
		t.Fatal("ok = false")
	}
	if max := now.Add(maxCooldown); got.After(max) {
		t.Errorf("until = %v: upper bound %v not respected", got, max)
	}
}

func TestConfirmedLimitKeepsShortResetShort(t *testing.T) {
	now := time.Unix(1790330000, 0)
	h := http.Header{}
	h.Set("anthropic-ratelimit-unified-status", "rejected")
	// A reset in the past must not trigger an endless retry.
	h.Set("anthropic-ratelimit-unified-reset", "1790329000")

	got, ok := confirmedLimit(h, now)
	if !ok {
		t.Fatal("ok = false")
	}
	if got.Before(now.Add(minCooldown)) {
		t.Errorf("until = %v: lower bound %v not respected", got, now.Add(minCooldown))
	}
}
