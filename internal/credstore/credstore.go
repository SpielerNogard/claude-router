// Package credstore reads Claude Code's access token; it renews only on request, since two parties sharing a refresh token invalidate each other.
package credstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Credential struct {
	AccessToken string
	ExpiresAt   time.Time
}

func (c Credential) Expired() bool {
	return !c.ExpiresAt.IsZero() && time.Now().After(c.ExpiresAt)
}

func sha8(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// ServiceName names the keychain entry like Claude Code: no suffix for the default directory, else a SHA-256 prefix.
func ServiceName(configDir string) string {
	const base = "Claude Code-credentials"
	if configDir == "" {
		return base
	}
	return base + "-" + sha8(configDir)
}

// ConfigDir returns the explicit value, else CLAUDE_CONFIG_DIR, else the default directory.
func ConfigDir(explicit string) string {
	if explicit != "" {
		return expand(explicit)
	}
	return expand(os.Getenv("CLAUDE_CONFIG_DIR"))
}

func expand(p string) string {
	if len(p) > 1 && p[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

type wire struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
		// Claude Code sometimes writes the milliseconds with a fractional part.
		ExpiresAt float64 `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

func parse(raw []byte) (Credential, error) {
	var w wire
	if err := json.Unmarshal(raw, &w); err != nil {
		return Credential{}, fmt.Errorf("unreadable credential: %w", err)
	}
	if w.ClaudeAiOauth.AccessToken == "" {
		return Credential{}, fmt.Errorf("no claudeAiOauth.accessToken found — is this Claude Code signed in?")
	}
	c := Credential{AccessToken: w.ClaudeAiOauth.AccessToken}
	if w.ClaudeAiOauth.ExpiresAt > 0 {
		c.ExpiresAt = time.UnixMilli(int64(w.ClaudeAiOauth.ExpiresAt))
	}
	return c, nil
}

func load(configDir string) (Credential, error) {
	raw, _, err := source(configDir)
	if err != nil {
		return Credential{}, err
	}
	return parse(raw)
}

func loadFromFile(configDir string) (Credential, error) {
	raw, _, err := fileSource(configDir)
	if err != nil {
		return Credential{}, err
	}
	return parse(raw)
}

// fileSource returns .credentials.json along with a function that writes it back atomically.
func fileSource(configDir string) ([]byte, func([]byte) error, error) {
	dir := configDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		dir = filepath.Join(home, ".claude")
	}
	path := filepath.Join(dir, ".credentials.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	save := func(b []byte) error {
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
	return raw, save, nil
}
