package credstore

import (
	"fmt"
	"sync"
	"time"
)

// Reading spawns a subprocess on macOS, so cache it, but only until shortly before expiry so a renewed token arrives quickly.
const refreshMargin = 10 * time.Minute

type entry struct {
	cred     Credential
	loadedAt time.Time
}

var (
	mu    sync.Mutex
	cache = map[string]entry{}
)

// Token returns the current access token; with refresh it renews an expiring one itself.
func Token(configDir string, refresh bool) (string, error) {
	mu.Lock()
	defer mu.Unlock()

	if e, ok := cache[configDir]; ok && usable(e) {
		return e.cred.AccessToken, nil
	}
	c, err := load(configDir)
	if err != nil {
		return "", err
	}
	// ponytail: the global lock stalls all providers while renewing; per-directory lock if that hurts.
	if refresh && !c.ExpiresAt.IsZero() && time.Now().After(c.ExpiresAt.Add(-refreshMargin)) {
		if c, err = renew(configDir); err != nil {
			return "", err
		}
	}
	if c.Expired() {
		return "", fmt.Errorf("access token expired at %s — start Claude Code normally once so it gets renewed", c.ExpiresAt.Format(time.RFC3339))
	}
	cache[configDir] = entry{cred: c, loadedAt: time.Now()}
	return c.AccessToken, nil
}

func usable(e entry) bool {
	if e.cred.ExpiresAt.IsZero() {
		return time.Since(e.loadedAt) < 5*time.Minute
	}
	return time.Now().Before(e.cred.ExpiresAt.Add(-refreshMargin))
}

// Forget drops the cache — after a 401 a fresh read is worthwhile.
func Forget(configDir string) {
	mu.Lock()
	delete(cache, configDir)
	mu.Unlock()
}
