package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	t.Setenv("DS_KEY", "sk-secret")
	p := filepath.Join(t.TempDir(), "config.toml")
	body := `
[providers.ds]
format = "openai"
auth = "key"
base_url = "https://api.deepseek.com"
api_key_env = "DS_KEY"
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return NewRegistry(p, c), p
}

func TestInvalidConfigIsRejectedAndFileUnchanged(t *testing.T) {
	reg, path := tempRegistry(t)
	before, _ := os.ReadFile(path)

	err := reg.Replace(&Config{Providers: map[string]Provider{
		"broken": {Name: "broken", Format: "openai", Auth: "key"}, // base_url missing
	}})
	if err == nil {
		t.Fatal("invalid config must be rejected")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("the file must not be touched on rejection")
	}
	if _, ok := reg.Get().Providers["ds"]; !ok {
		t.Error("the old config must stay active")
	}
}

func TestReplaceWritesBackupAndSwaps(t *testing.T) {
	reg, path := tempRegistry(t)
	t.Setenv("NEW_KEY", "sk-new")
	next := &Config{Listen: "127.0.0.1:4141", Log: LogConfig{Level: "off", KeepDays: 7, MaxSizeMB: 500, MaxBodyKB: 256},
		Providers: map[string]Provider{
			"new": {Name: "new", Format: "openai", Auth: "key", BaseURL: "https://x.invalid", APIKeyEnv: "NEW_KEY"},
		}}
	if err := reg.Replace(next); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("no backup created: %v", err)
	}
	if _, ok := reg.Get().Providers["new"]; !ok {
		t.Error("the new config is not active")
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), "NEW_KEY") {
		t.Error("api_key_env missing from the written file")
	}
	if strings.Contains(string(written), "sk-new") {
		t.Fatal("the key value must never be written to the file")
	}
}

func TestRedactNeverLeaksKeys(t *testing.T) {
	reg, _ := tempRegistry(t)
	b, err := json.Marshal(Redact(reg.Get()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "sk-secret") {
		t.Fatal("the key value ended up in the API response")
	}
	if !strings.Contains(string(b), "key_set") {
		t.Error("the UI needs to know whether the key is set")
	}
}

func TestSnapshotStaysStableAcrossReplace(t *testing.T) {
	reg, _ := tempRegistry(t)
	t.Setenv("NEW_KEY", "sk-new")
	before := reg.Get()

	if err := reg.Replace(&Config{Listen: "127.0.0.1:4141",
		Log: LogConfig{Level: "off", KeepDays: 7, MaxSizeMB: 500, MaxBodyKB: 256},
		Providers: map[string]Provider{
			"new": {Name: "new", Format: "openai", Auth: "key", BaseURL: "https://x.invalid", APIKeyEnv: "NEW_KEY"},
		}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := before.Providers["ds"]; !ok {
		t.Error("an in-flight request's snapshot must not change on Replace")
	}
	if _, ok := reg.Get().Providers["new"]; !ok {
		t.Error("a new request must see the new config")
	}
}

func TestBackupContainsThePreviousVersion(t *testing.T) {
	reg, path := tempRegistry(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEW_KEY", "sk-new")
	if err := reg.Replace(&Config{Listen: "127.0.0.1:4141",
		Log: LogConfig{Level: "off", KeepDays: 7, MaxSizeMB: 500, MaxBodyKB: 256},
		Providers: map[string]Provider{
			"new": {Name: "new", Format: "openai", Auth: "key", BaseURL: "https://x.invalid", APIKeyEnv: "NEW_KEY"},
		}}); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != string(original) {
		t.Errorf("the backup does not contain the previous version:\n%s", backup)
	}
}

func TestRoundTripKeepsEverySetting(t *testing.T) {
	t.Setenv("DS_KEY", "sk-secret")
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[server]
listen = "0.0.0.0:9999"

[log]
level = "full"
keep_days = 3
max_size_mb = 42
max_body_kb = 7

[providers.ds]
format = "openai"
auth = "key"
base_url = "https://api.deepseek.com"
api_key_env = "DS_KEY"
include = ["deepseek-*"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(path, first)

	// Like the frontend: read, write back unchanged.
	viaJSON, err := json.Marshal(Redact(reg.Get()))
	if err != nil {
		t.Fatal(err)
	}
	var next Config
	if err := json.Unmarshal(viaJSON, &next); err != nil {
		t.Fatal(err)
	}
	if err := reg.Replace(&next); err != nil {
		t.Fatalf("writing back unchanged was rejected: %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Listen != "0.0.0.0:9999" {
		t.Errorf("Listen = %q — the setting was lost on write-back", again.Listen)
	}
	if again.Log.Level != "full" || again.Log.KeepDays != 3 || again.Log.MaxSizeMB != 42 || again.Log.MaxBodyKB != 7 {
		t.Errorf("log settings changed: %+v", again.Log)
	}
	p := again.Providers["ds"]
	if p.BaseURL != "https://api.deepseek.com" || p.APIKeyEnv != "DS_KEY" || len(p.Include) != 1 {
		t.Errorf("provider changed: %+v", p)
	}
}
