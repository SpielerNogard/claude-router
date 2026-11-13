package proxy

import (
	"testing"
	"time"
)

func TestCooldownBlockedUntilExpiry(t *testing.T) {
	c := NewCooldown()
	now := time.Unix(1790330000, 0)

	if c.Blocked("sub", now) {
		t.Error("a never-blocked provider counts as blocked")
	}
	c.Block("sub", now.Add(time.Hour))
	if !c.Blocked("sub", now) {
		t.Error("not blocked within the block period")
	}
	if c.Blocked("ds", now) {
		t.Error("the block affects the wrong provider")
	}
	if c.Blocked("sub", now.Add(2*time.Hour)) {
		t.Error("still blocked after expiry")
	}
}

func TestCooldownBlockedUntilReportsOnlyActiveBlocks(t *testing.T) {
	c := NewCooldown()
	now := time.Unix(1790330000, 0)
	c.Block("sub", now.Add(time.Hour))
	c.Block("ds", now.Add(-time.Hour))

	got := c.BlockedUntil(now)
	if _, ok := got["sub"]; !ok {
		t.Error("the active block is missing")
	}
	if _, ok := got["ds"]; ok {
		t.Error("an expired block is reported")
	}
}

func TestCooldownKeepsLatestReset(t *testing.T) {
	c := NewCooldown()
	now := time.Unix(1790330000, 0)
	late := now.Add(6 * time.Hour)

	// Two concurrent requests must not pull the block forward.
	c.Block("sub", late)
	c.Block("sub", now.Add(time.Minute))

	if !c.Blocked("sub", now.Add(5*time.Hour)) {
		t.Error("the later reset was overwritten by the earlier one")
	}
}
