package proxy

import (
	"net/http"
	"strconv"
	"time"
)

const (
	// A reset in the past must not trigger an endless retry.
	minCooldown = 5 * time.Second
	// An implausible reset far in the future must not park the subscription.
	maxCooldown = 7 * 24 * time.Hour
)

// confirmedLimit reads from a 429 response's headers until when the subscription
// counts as exhausted. The condition matches Claude Code's: status must be
// "rejected", and as long as Anthropic offers overage the request is not blocked.
func confirmedLimit(h http.Header, now time.Time) (time.Time, bool) {
	if h.Get("anthropic-ratelimit-unified-status") != "rejected" {
		return time.Time{}, false
	}
	if s := h.Get("anthropic-ratelimit-unified-overage-status"); s != "" && s != "rejected" {
		return time.Time{}, false
	}
	if sec, ok := headerSeconds(h.Get("anthropic-ratelimit-unified-reset")); ok {
		return clampCooldown(time.Unix(sec, 0), now), true
	}
	if sec, ok := headerSeconds(h.Get("retry-after")); ok {
		return clampCooldown(now.Add(time.Duration(sec)*time.Second), now), true
	}
	return time.Time{}, false
}

// headerSeconds parses a number of seconds. The RFC 9110 date form of
// retry-after is deliberately not supported.
func headerSeconds(v string) (int64, bool) {
	if v == "" {
		return 0, false
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return sec, true
}

func clampCooldown(until, now time.Time) time.Time {
	if until.Before(now.Add(minCooldown)) {
		return now.Add(minCooldown)
	}
	if until.After(now.Add(maxCooldown)) {
		return now.Add(maxCooldown)
	}
	return until
}
