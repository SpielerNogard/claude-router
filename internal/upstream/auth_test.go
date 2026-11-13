package upstream

import (
	"fmt"
	"github.com/SpielerNogard/claude-router/internal/credstore"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func TestPassthroughForwardsClientAuth(t *testing.T) {
	incoming := http.Header{}
	incoming.Set("authorization", "Bearer oauth-token-from-subscription")
	incoming.Set("anthropic-beta", "oauth-2025-04-20")
	incoming.Set("anthropic-version", "2024-10-22")

	out := http.Header{}
	_ = ApplyAuth(out, config.Provider{Auth: "passthrough"}, incoming)

	if out.Get("authorization") != "Bearer oauth-token-from-subscription" {
		t.Errorf("authorization = %q, must be forwarded unchanged", out.Get("authorization"))
	}
	if out.Get("anthropic-beta") != "oauth-2025-04-20" {
		t.Error("anthropic-beta must be forwarded, otherwise Anthropic rejects the OAuth token")
	}
	if out.Get("anthropic-version") != "2024-10-22" {
		t.Errorf("anthropic-version = %q, must be forwarded, otherwise Anthropic rejects the OAuth token", out.Get("anthropic-version"))
	}
}

func TestKeyAuthReplacesClientCredential(t *testing.T) {
	t.Setenv("TEST_PROVIDER_KEY", "sk-provider")
	incoming := http.Header{}
	incoming.Set("authorization", "Bearer oauth-token-from-subscription")
	incoming.Set("x-api-key", "sk-ant-from-client")

	out := http.Header{}
	_ = ApplyAuth(out, config.Provider{Auth: "key", APIKeyEnv: "TEST_PROVIDER_KEY", Format: "openai"}, incoming)

	if got := out.Get("authorization"); got != "Bearer sk-provider" {
		t.Errorf("authorization = %q, want Bearer sk-provider", got)
	}
	if out.Get("x-api-key") != "" {
		t.Error("the client credential must not be forwarded with auth=key")
	}
	for _, v := range out {
		for _, s := range v {
			if strings.Contains(s, "oauth-token-from-subscription") {
				t.Fatal("subscription token ended up in the outgoing headers")
			}
		}
	}
}

func TestKeyAuthOnAnthropicFormatUsesXAPIKey(t *testing.T) {
	t.Setenv("ANT_KEY", "sk-ant-own")
	out := http.Header{}
	_ = ApplyAuth(out, config.Provider{Auth: "key", APIKeyEnv: "ANT_KEY", Format: "anthropic"}, http.Header{})
	if out.Get("x-api-key") != "sk-ant-own" {
		t.Errorf("Anthropic format needs x-api-key, got %q", out.Get("x-api-key"))
	}
	if out.Get("authorization") != "" {
		t.Error("with x-api-key no authorization may be set")
	}
}

func TestKeyAuthOnAnthropicSetsVersionHeader(t *testing.T) {
	t.Setenv("ANT_KEY", "sk-ant-own")
	p := config.Provider{Auth: "key", APIKeyEnv: "ANT_KEY", Format: "anthropic"}

	out := http.Header{}
	_ = ApplyAuth(out, p, http.Header{})
	if out.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("without a client value the default must be set, got %q", out.Get("anthropic-version"))
	}

	incoming := http.Header{}
	incoming.Set("anthropic-version", "2024-10-22")
	out = http.Header{}
	_ = ApplyAuth(out, p, incoming)
	if out.Get("anthropic-version") != "2024-10-22" {
		t.Errorf("the client value takes precedence, got %q", out.Get("anthropic-version"))
	}
}

func TestCopyForwardableHeadersDropsCredentialsAndHopHeaders(t *testing.T) {
	in := http.Header{}
	in.Set("authorization", "Bearer secret")
	in.Set("x-api-key", "sk-secret")
	in.Set("connection", "keep-alive")
	in.Set("transfer-encoding", "chunked")
	in.Set("content-length", "123")
	in.Set("host", "example.invalid")
	in.Set("accept-encoding", "gzip, deflate")
	in.Set("user-agent", "claude-cli/2.0")
	in.Set("anthropic-beta", "oauth-2025-04-20")
	in.Add("x-multi", "one")
	in.Add("x-multi", "two")

	out := http.Header{}
	CopyForwardableHeaders(out, in)

	for _, k := range []string{"authorization", "x-api-key", "connection", "transfer-encoding", "content-length", "host", "accept-encoding"} {
		if out.Get(k) != "" {
			t.Errorf("%s must not be forwarded, but is %q", k, out.Get(k))
		}
	}
	if out.Get("user-agent") != "claude-cli/2.0" {
		t.Errorf("user-agent must be forwarded, got %q", out.Get("user-agent"))
	}
	if out.Get("anthropic-beta") != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta must be forwarded, got %q", out.Get("anthropic-beta"))
	}
	if got := out.Values("x-multi"); len(got) != 2 {
		t.Errorf("repeated headers must arrive complete, got %v", got)
	}
}

func TestCopyForwardableHeadersIgnoresCase(t *testing.T) {
	in := http.Header{}
	// Raw map keys bypass http.Header canonicalization; the filter must still match.
	in["AUTHORIZATION"] = []string{"Bearer secret"}
	in["X-Api-Key"] = []string{"sk-secret"}

	out := http.Header{}
	CopyForwardableHeaders(out, in)

	for k, v := range out {
		for _, s := range v {
			if strings.Contains(s, "secret") {
				t.Fatalf("credential leaked through header %q", k)
			}
		}
	}
}

func TestClaudeCredentialHelperUsesTheStoredToken(t *testing.T) {
	dir := t.TempDir()
	raw := `{"claudeAiOauth":{"accessToken":"sk-ant-oat-from-store",
	  "refreshToken":"must-never-be-used","expiresAt":` +
		fmt.Sprint(time.Now().Add(time.Hour).UnixMilli()) + `}}`
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	credstore.Forget(dir)

	out := http.Header{}
	p := config.Provider{Name: "sub", Format: "anthropic", Auth: "claude-credential-helper", ConfigDir: dir}
	if err := ApplyAuth(out, p, http.Header{}); err != nil {
		t.Fatalf("ApplyAuth: %v", err)
	}
	if out.Get("authorization") != "Bearer sk-ant-oat-from-store" {
		t.Errorf("authorization = %q", out.Get("authorization"))
	}
	if out.Get("anthropic-beta") != "oauth-2025-04-20" {
		t.Errorf("without anthropic-beta Anthropic rejects a subscription token, got %q", out.Get("anthropic-beta"))
	}
	for _, v := range out {
		for _, s := range v {
			if strings.Contains(s, "must-never-be-used") {
				t.Fatal("the refresh token ended up in the headers")
			}
		}
	}
}

func TestClaudeCredentialHelperReportsExpiredToken(t *testing.T) {
	dir := t.TempDir()
	raw := `{"claudeAiOauth":{"accessToken":"old","expiresAt":` +
		fmt.Sprint(time.Now().Add(-time.Hour).UnixMilli()) + `}}`
	os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(raw), 0o600)
	credstore.Forget(dir)

	err := ApplyAuth(http.Header{}, config.Provider{
		Format: "anthropic", Auth: "claude-credential-helper", ConfigDir: dir}, http.Header{})
	if err == nil {
		t.Fatal("an expired token must be reported")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("the message should name the cause: %v", err)
	}
}

func TestAuthNoneSendsNoCredential(t *testing.T) {
	incoming := http.Header{}
	incoming.Set("authorization", "Bearer oauth-from-subscription")
	incoming.Set("x-api-key", "sk-ant-from-client")

	out := http.Header{}
	if err := ApplyAuth(out, config.Provider{
		Name: "lmstudio", Format: "openai", Auth: "none", BaseURL: "http://127.0.0.1:1234"}, incoming); err != nil {
		t.Fatal(err)
	}
	if out.Get("authorization") != "" || out.Get("x-api-key") != "" {
		t.Errorf("a local server must not see any credential: %v", out)
	}
}

func TestMergeBetasKeepsAllAndAddsRequired(t *testing.T) {
	cases := []struct {
		existing []string
		want     string
	}{
		{nil, "oauth-2025-04-20"},
		{[]string{"claude-code-20250219"}, "claude-code-20250219,oauth-2025-04-20"},
		{[]string{"a,b"}, "a,b,oauth-2025-04-20"},
		{[]string{"a", "b"}, "a,b,oauth-2025-04-20"},
		{[]string{"oauth-2025-04-20,a"}, "oauth-2025-04-20,a"},
		{[]string{" a , a "}, "a,oauth-2025-04-20"},
	}
	for _, c := range cases {
		if got := mergeBetas(c.existing, "oauth-2025-04-20"); got != c.want {
			t.Errorf("mergeBetas(%v) = %q, want %q", c.existing, got, c.want)
		}
	}
}

func TestCredentialHelperKeepsClientBetas(t *testing.T) {
	dir := t.TempDir()
	raw := `{"claudeAiOauth":{"accessToken":"sk-ant-oat-x","expiresAt":` +
		fmt.Sprint(time.Now().Add(time.Hour).UnixMilli()) + `}}`
	os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(raw), 0o600)
	credstore.Forget(dir)

	incoming := http.Header{}
	incoming.Add("anthropic-beta", "claude-code-20250219")
	incoming.Add("anthropic-beta", "fine-grained-tool-streaming-2025-05-14")

	out := http.Header{}
	if err := ApplyAuth(out, config.Provider{
		Format: "anthropic", Auth: "claude-credential-helper", ConfigDir: dir}, incoming); err != nil {
		t.Fatal(err)
	}
	got := out.Get("anthropic-beta")
	for _, required := range []string{"claude-code-20250219", "fine-grained-tool-streaming-2025-05-14", "oauth-2025-04-20"} {
		if !strings.Contains(got, required) {
			t.Errorf("anthropic-beta = %q — %q missing", got, required)
		}
	}
}

func TestPassthroughDoesNotCollapseCopiedBetas(t *testing.T) {
	incoming := http.Header{}
	incoming.Set("authorization", "Bearer oauth-from-subscription")
	incoming.Add("anthropic-beta", "one")
	incoming.Add("anthropic-beta", "two")

	// This is how the proxy does it: copy first, then add the credentials.
	out := http.Header{}
	CopyForwardableHeaders(out, incoming)
	if err := ApplyAuth(out, config.Provider{Format: "anthropic", Auth: "passthrough"}, incoming); err != nil {
		t.Fatal(err)
	}
	if got := out.Values("anthropic-beta"); len(got) != 2 {
		t.Errorf("anthropic-beta = %v — both values must be kept", got)
	}
	if out.Get("authorization") != "Bearer oauth-from-subscription" {
		t.Errorf("the credential is missing: %q", out.Get("authorization"))
	}
}

func TestPassthroughFillsInWhenNothingWasCopied(t *testing.T) {
	// Discovery calls ApplyAuth without a prior copy step.
	incoming := http.Header{}
	incoming.Set("authorization", "Bearer oauth")
	incoming.Set("anthropic-beta", "one")

	out := http.Header{}
	if err := ApplyAuth(out, config.Provider{Format: "anthropic", Auth: "passthrough"}, incoming); err != nil {
		t.Fatal(err)
	}
	if out.Get("anthropic-beta") != "one" || out.Get("authorization") != "Bearer oauth" {
		t.Errorf("without a copy step ApplyAuth must fill in: %v", out)
	}
}
