package proxy

import (
	"github.com/SpielerNogard/claude-router/internal/alias"
	"strings"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func testConfig() *config.Config {
	return &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough"},
		"ds":  {Name: "ds", Format: "openai", Auth: "key", BaseURL: "https://api.deepseek.com", APIKeyEnv: "DS"},
	}}
}

func TestResolveSplitsOnFirstColon(t *testing.T) {
	tgt, rerr := Resolve(testConfig(), "ds:deepseek-chat")
	if rerr != nil {
		t.Fatalf("unexpected error: %v", rerr)
	}
	if tgt.Provider.Name != "ds" || tgt.UpstreamModel != "deepseek-chat" {
		t.Errorf("Target = %+v", tgt)
	}
}

func TestResolveKeepsLaterColons(t *testing.T) {
	tgt, rerr := Resolve(testConfig(), "ds:vendor/model:free")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if tgt.UpstreamModel != "vendor/model:free" {
		t.Errorf("UpstreamModel = %q: only the first colon splits", tgt.UpstreamModel)
	}
}

func TestResolveMissingColonIs404(t *testing.T) {
	_, rerr := Resolve(testConfig(), "opus-5")
	if rerr == nil {
		t.Fatal("alias without colon must be rejected")
	}
	if rerr.Status != 404 || rerr.Kind != "not_found_error" {
		t.Errorf("Status/Kind = %d/%q, want 404/not_found_error", rerr.Status, rerr.Kind)
	}
	if !strings.Contains(rerr.Message, "sub") || !strings.Contains(rerr.Message, "ds") {
		t.Errorf("the message should list the available providers: %q", rerr.Message)
	}
}

func TestResolveUnknownProviderIs404(t *testing.T) {
	_, rerr := Resolve(testConfig(), "gibtsnicht:modell")
	if rerr == nil || rerr.Status != 404 {
		t.Fatalf("unknown provider must yield 404, got %+v", rerr)
	}
}

func TestResolveEmptyModelIs400(t *testing.T) {
	for _, in := range []string{"", "ds:"} {
		_, rerr := Resolve(testConfig(), in)
		if rerr == nil || rerr.Status != 400 {
			t.Errorf("Resolve(%q): expected 400, got %+v", in, rerr)
		}
	}
}

func withDefault() *config.Config {
	return &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", Default: true},
		"ds":  {Name: "ds", Format: "openai", Auth: "key", BaseURL: "https://api.deepseek.com", APIKeyEnv: "DS"},
	}}
}

func TestResolveUnprefixedGoesToDefaultProvider(t *testing.T) {
	tgt, rerr := Resolve(withDefault(), "claude-opus-5[1m]")
	if rerr != nil {
		t.Fatalf("unexpected error: %v", rerr)
	}
	if tgt.Provider.Name != "sub" {
		t.Errorf("Provider = %q, want sub", tgt.Provider.Name)
	}
	if tgt.UpstreamModel != "claude-opus-5[1m]" {
		t.Errorf("UpstreamModel = %q: the name must be passed on unchanged", tgt.UpstreamModel)
	}
}

func TestResolveModelNameWithColonGoesToDefault(t *testing.T) {
	// Ollama names models like llama3:8b; llama3 is not a provider, so the
	// whole name goes to the default target.
	tgt, rerr := Resolve(withDefault(), "llama3:8b")
	if rerr != nil {
		t.Fatalf("unexpected error: %v", rerr)
	}
	if tgt.Provider.Name != "sub" || tgt.UpstreamModel != "llama3:8b" {
		t.Errorf("Target = %+v: the whole name belongs to the default target", tgt)
	}
}

func TestResolveKnownPrefixStillWinsOverDefault(t *testing.T) {
	tgt, rerr := Resolve(withDefault(), "ds:deepseek-chat")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if tgt.Provider.Name != "ds" || tgt.UpstreamModel != "deepseek-chat" {
		t.Errorf("Target = %+v", tgt)
	}
}

func TestResolveWithoutDefaultStillReports404(t *testing.T) {
	_, rerr := Resolve(testConfig(), "claude-opus-5[1m]")
	if rerr == nil || rerr.Status != 404 {
		t.Fatalf("without a default provider it must still yield 404, got %+v", rerr)
	}
}

func TestResolveAcceptsEncodedAlias(t *testing.T) {
	enc := alias.Encode("ds:deepseek-chat")
	tgt, rerr := Resolve(testConfig(), enc)
	if rerr != nil {
		t.Fatalf("encoded alias %q was rejected: %v", enc, rerr)
	}
	if tgt.Provider.Name != "ds" || tgt.UpstreamModel != "deepseek-chat" {
		t.Errorf("Target = %+v", tgt)
	}
}

func TestResolveEncodedAliasKeepsContextSuffix(t *testing.T) {
	tgt, rerr := Resolve(withDefault(), alias.Encode("sub:claude-opus-5[1m]"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if tgt.UpstreamModel != "claude-opus-5[1m]" {
		t.Errorf("UpstreamModel = %q: the suffix must reach the provider", tgt.UpstreamModel)
	}
}
