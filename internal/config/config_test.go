package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAppliesDefaults(t *testing.T) {
	c, err := Load(write(t, `
[providers.sub]
format = "anthropic"
auth   = "passthrough"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Listen != "127.0.0.1:4141" {
		t.Errorf("Listen = %q, want 127.0.0.1:4141", c.Listen)
	}
	if c.Log.Level != "off" {
		t.Errorf("Log.Level = %q, want off", c.Log.Level)
	}
	if c.Log.KeepDays != 7 || c.Log.MaxSizeMB != 500 || c.Log.MaxBodyKB != 256 {
		t.Errorf("Log defaults = %+v", c.Log)
	}
	if c.Providers["sub"].Name != "sub" {
		t.Errorf("Name not set from the key: %+v", c.Providers["sub"])
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]string{
		"openai without base_url": `
[providers.x]
format = "openai"
auth = "key"
api_key_env = "X_KEY"
`,
		"key without api_key_env": `
[providers.x]
format = "openai"
auth = "key"
base_url = "https://example.invalid"
`,
		"colon in name": `
[providers."a:b"]
format = "anthropic"
auth = "passthrough"
`,
		"unknown format": `
[providers.x]
format = "cohere"
auth = "passthrough"
`,
		"unknown auth": `
[providers.x]
format = "anthropic"
auth = "magic"
`,
		"openai with passthrough": `
[providers.x]
format = "openai"
auth = "passthrough"
base_url = "https://example.invalid"
`,
		"fallback without colon": `
[providers.a]
format = "anthropic"
auth = "passthrough"
fallback = "deepseek"
`,
		"fallback without model": `
[providers.a]
format = "anthropic"
auth = "passthrough"
fallback = "b:"
`,
		"fallback names unknown provider": `
[providers.a]
format = "anthropic"
auth = "passthrough"
fallback = "nosuch:m"
`,
		"fallback points to itself": `
[providers.a]
format = "anthropic"
auth = "passthrough"
fallback = "a:m"
`,
		"fallback cycle": `
[providers.a]
format = "anthropic"
auth = "passthrough"
fallback = "b:m"

[providers.b]
format = "anthropic"
auth = "passthrough"
fallback = "a:m"
`,
		"refresh without credential-helper": `
[providers.a]
format = "anthropic"
auth = "passthrough"
refresh = true
`,
		"fallback cycle across three": `
[providers.a]
format = "anthropic"
auth = "passthrough"
fallback = "b"

[providers.b]
format = "anthropic"
auth = "passthrough"
fallback = "c"

[providers.c]
format = "anthropic"
auth = "passthrough"
fallback = "a:m"
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, body)); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestRedactKeepsRoundTrippedFields(t *testing.T) {
	// The frontend reads the config via GET /api/config and writes it back via PUT; whatever Redact omits is silently lost.
	c := &Config{Providers: map[string]Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", Default: true,
			Fallback: "ds:m", ExtraModels: []string{"x[1m]"}},
	}}
	raw, err := json.Marshal(Redact(c))
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	p := back.Providers["sub"]
	if p.Fallback != "ds:m" {
		t.Errorf("Fallback = %q — lost on write-back", p.Fallback)
	}
	if len(p.ExtraModels) != 1 || p.ExtraModels[0] != "x[1m]" {
		t.Errorf("ExtraModels = %v — lost on write-back", p.ExtraModels)
	}
	if !p.Default {
		t.Error("Default lost on write-back")
	}
}

func TestValidateAcceptsFallback(t *testing.T) {
	t.Setenv("DS_KEY", "sk-test")
	cfg, err := Load(write(t, `
[providers.sub]
format = "anthropic"
auth = "passthrough"
fallback = "ds:deepseek-flash"

[providers.ds]
format = "openai"
auth = "key"
base_url = "https://api.deepseek.com"
api_key_env = "DS_KEY"
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Providers["sub"].Fallback; got != "ds:deepseek-flash" {
		t.Errorf("Fallback = %q, want ds:deepseek-flash", got)
	}
}

func TestValidateAcceptsFallbackChainWithoutModel(t *testing.T) {
	t.Setenv("DS_KEY", "sk-test")
	_, err := Load(write(t, `
[providers.sub]
format = "anthropic"
auth = "passthrough"
fallback = "account2"

[providers.account2]
format = "anthropic"
auth = "claude-credential-helper"
fallback = "ds:deepseek-flash"

[providers.ds]
format = "openai"
auth = "key"
base_url = "https://api.deepseek.com"
api_key_env = "DS_KEY"
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsEmptyEnvVar(t *testing.T) {
	t.Setenv("ROUTER_TEST_EMPTY", "")
	_, err := Load(write(t, `
[providers.x]
format = "openai"
auth = "key"
base_url = "https://example.invalid"
api_key_env = "ROUTER_TEST_EMPTY"
`))
	if err == nil {
		t.Fatal("empty environment variable must be caught at startup")
	}
}

func TestValidateRejectsTwoDefaults(t *testing.T) {
	_, err := Load(write(t, `
[providers.a]
format = "anthropic"
auth = "passthrough"
default = true

[providers.b]
format = "anthropic"
auth = "passthrough"
default = true
`))
	if err == nil {
		t.Fatal("two default providers must be rejected")
	}
	if !strings.Contains(err.Error(), "a, b") {
		t.Errorf("the message should name both: %v", err)
	}
}

func TestValidateAcceptsOneDefault(t *testing.T) {
	c, err := Load(write(t, `
[providers.a]
format = "anthropic"
auth = "passthrough"
default = true

[providers.b]
format = "anthropic"
auth = "passthrough"
`))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Providers["a"].Default || c.Providers["b"].Default {
		t.Errorf("Default read incorrectly: %+v", c.Providers)
	}
}

const minimalProvider = `
[providers.sub]
format = "anthropic"
auth   = "passthrough"
`

func TestUpdateCheckDefaultsToEnabled(t *testing.T) {
	c, err := Load(write(t, minimalProvider))
	if err != nil {
		t.Fatal(err)
	}
	if !c.UpdateCheckEnabled() {
		t.Error("UpdateCheckEnabled() = false without [update], want true")
	}
	out, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "[update]") {
		t.Errorf("Marshal wrote an empty [update] table:\n%s", out)
	}
}

func TestUpdateCheckCanBeDisabled(t *testing.T) {
	c, err := Load(write(t, minimalProvider+"\n[update]\ncheck = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.UpdateCheckEnabled() {
		t.Fatal("UpdateCheckEnabled() = true with check = false")
	}
	out, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Load(write(t, string(out)))
	if err != nil {
		t.Fatal(err)
	}
	if again.UpdateCheckEnabled() {
		t.Errorf("check = false lost on round trip:\n%s", out)
	}
}
