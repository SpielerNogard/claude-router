package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Provider struct {
	Name      string   `toml:"-" json:"name"`
	Format    string   `toml:"format" json:"format"`
	Auth      string   `toml:"auth" json:"auth"`
	BaseURL   string   `toml:"base_url" json:"base_url"`
	APIKeyEnv string   `toml:"api_key_env" json:"api_key_env"`
	Models    []string `toml:"models" json:"models"`
	// ExtraModels are added on top of discovery; Include/Exclude don't apply to them — listing them is intentional.
	ExtraModels []string `toml:"extra_models" json:"extra_models"`
	Include     []string `toml:"include" json:"include"`
	Exclude     []string `toml:"exclude" json:"exclude"`
	Default     bool     `toml:"default" json:"default"`
	ConfigDir   string   `toml:"config_dir" json:"config_dir"`
	// Refresh lets the router renew the token — only if no Claude Code uses this directory.
	Refresh bool `toml:"refresh" json:"refresh"`
	// Fallback applies while this provider is exhausted: provider:model, or just provider for the same model.
	Fallback string `toml:"fallback" json:"fallback"`
}

type LogConfig struct {
	Level     string `toml:"level" json:"level"`
	KeepDays  int    `toml:"keep_days" json:"keep_days"`
	MaxSizeMB int    `toml:"max_size_mb" json:"max_size_mb"`
	MaxBodyKB int    `toml:"max_body_kb" json:"max_body_kb"`
}

type Config struct {
	Listen string `toml:"-" json:"listen"`
	Server struct {
		Listen     string `toml:"listen"`
		StreamIdle string `toml:"stream_idle_timeout"`
	} `toml:"server" json:"-"`
	// Nil means default; an explicit "0s" disables the watchdog.
	StreamIdle *time.Duration      `toml:"-" json:"stream_idle_timeout"`
	Log        LogConfig           `toml:"log" json:"log"`
	Providers  map[string]Provider `toml:"providers" json:"providers"`
}

func Load(path string) (*Config, error) {
	c, err := LoadUnvalidated(path)
	if err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return c, nil
}

// LoadUnvalidated reads the file without validating it, for tools that report the errors Validate would fail on.
func LoadUnvalidated(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	c.applyDefaults()
	return &c, nil
}

func (c *Config) applyDefaults() {
	// From TOML the value sits under [server], via the JSON API it arrives flat; an already set value wins, else it would be lost on write.
	if c.Listen == "" {
		c.Listen = c.Server.Listen
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:4141"
	}
	if c.StreamIdle == nil && c.Server.StreamIdle != "" {
		if d, err := time.ParseDuration(c.Server.StreamIdle); err == nil {
			c.StreamIdle = &d
		}
	}
	if c.Log.Level == "" {
		c.Log.Level = "off"
	}
	if c.Log.KeepDays == 0 {
		c.Log.KeepDays = 7
	}
	if c.Log.MaxSizeMB == 0 {
		c.Log.MaxSizeMB = 500
	}
	if c.Log.MaxBodyKB == 0 {
		c.Log.MaxBodyKB = 256
	}
	for name, p := range c.Providers {
		p.Name = name
		c.Providers[name] = p
	}
}

func (c *Config) Validate() error {
	if raw := c.Server.StreamIdle; raw != "" && c.StreamIdle == nil {
		return fmt.Errorf("server.stream_idle_timeout %q is not a duration (e.g. \"60s\"; \"0s\" disables it)", raw)
	}
	if c.StreamIdle != nil && *c.StreamIdle < 0 {
		return fmt.Errorf("server.stream_idle_timeout must not be negative")
	}
	switch c.Log.Level {
	case "off", "meta", "full":
	default:
		return fmt.Errorf("log.level %q: allowed are off, meta, full", c.Log.Level)
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("no provider configured")
	}
	var defaults []string
	for name, p := range c.Providers {
		if p.Default {
			defaults = append(defaults, name)
		}
	}
	if len(defaults) > 1 {
		sort.Strings(defaults)
		return fmt.Errorf("multiple providers are marked default = true (%s) — a model name without prefix "+
			"can only have one target", strings.Join(defaults, ", "))
	}
	for name, p := range c.Providers {
		if strings.Contains(name, ":") {
			return fmt.Errorf("provider %q: colon is not allowed in the name, it separates alias from model", name)
		}
		switch p.Format {
		case "anthropic":
		case "openai":
			if p.BaseURL == "" {
				return fmt.Errorf("provider %q: format=openai requires base_url", name)
			}
		default:
			return fmt.Errorf("provider %q: format %q, allowed are anthropic, openai", name, p.Format)
		}
		if p.Format == "openai" && p.Auth == "passthrough" {
			return fmt.Errorf(
				"provider %q: format=openai with auth=passthrough is not allowed — passthrough forwards the client's "+
					"credential unchanged, and that is an Anthropic token which has no business at an OpenAI-style "+
					"provider", name)
		}
		switch p.Auth {
		case "none":
			if p.BaseURL == "" {
				return fmt.Errorf("provider %q: auth=none is meant for local servers and requires base_url", name)
			}
		case "claude-credential-helper":
			if p.Format != "anthropic" {
				return fmt.Errorf("provider %q: auth=claude-credential-helper requires format=anthropic — "+
					"the credential is only valid at Anthropic", name)
			}
		case "passthrough":
		case "key":
			if p.APIKeyEnv == "" {
				return fmt.Errorf("provider %q: auth=key requires api_key_env", name)
			}
			if os.Getenv(p.APIKeyEnv) == "" {
				return fmt.Errorf("provider %q: environment variable %s is empty or unset", name, p.APIKeyEnv)
			}
		default:
			return fmt.Errorf("provider %q: auth %q, allowed are passthrough, key, none, claude-credential-helper", name, p.Auth)
		}
		if p.Refresh && p.Auth != "claude-credential-helper" {
			return fmt.Errorf("provider %q: refresh only applies to auth=claude-credential-helper", name)
		}
		if p.Fallback != "" {
			// Without a model ("account2") the requested model is kept — switching to a second account.
			fbProvider, fbModel, found := strings.Cut(p.Fallback, ":")
			if found && fbModel == "" {
				return fmt.Errorf("provider %q: fallback %q — expected provider:model "+
					"or just provider", name, p.Fallback)
			}
			if fbProvider == name {
				return fmt.Errorf("provider %q: fallback points to the provider itself — "+
					"that would create a loop", name)
			}
			if _, ok := c.Providers[fbProvider]; !ok {
				return fmt.Errorf("provider %q: fallback %q names an unknown provider %q",
					name, p.Fallback, fbProvider)
			}
		}
	}
	return c.checkFallbackCycles()
}

// The chain is followed at runtime, so it must not contain a cycle anywhere.
func (c *Config) checkFallbackCycles() error {
	for start := range c.Providers {
		seen := map[string]bool{start: true}
		for cur := start; ; {
			next, _, _ := strings.Cut(c.Providers[cur].Fallback, ":")
			if next == "" {
				break
			}
			if seen[next] {
				return fmt.Errorf("provider %q: fallback chain returns to %q — "+
					"that would create a loop", start, next)
			}
			seen[next] = true
			cur = next
		}
	}
	return nil
}
