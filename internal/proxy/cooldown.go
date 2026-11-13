package proxy

import (
	"sync"
	"time"
)

// Cooldown tracks, per provider, until when it counts as exhausted. No
// background timer: an expired block simply no longer applies.
type Cooldown struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func NewCooldown() *Cooldown { return &Cooldown{until: map[string]time.Time{}} }

// Block sets the block. The later reset wins so two concurrent requests
// cannot pull it forward.
func (c *Cooldown) Block(name string, until time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, ok := c.until[name]; !ok || until.After(cur) {
		c.until[name] = until
	}
}

func (c *Cooldown) Blocked(name string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.until[name]
	return ok && now.Before(until)
}

// BlockedUntil reports the active blocks; an expired one is not reported.
func (c *Cooldown) BlockedUntil(now time.Time) map[string]time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]time.Time{}
	for name, until := range c.until {
		if now.Before(until) {
			out[name] = until
		}
	}
	return out
}
