package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func TestCaptureWriterStopsAtLimit(t *testing.T) {
	c := &captureWriter{limit: 5}
	if n, err := c.Write([]byte("hello world")); err != nil || n != 11 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if got := c.buf.String(); got != "hello" {
		t.Errorf("captureWriter = %q, want %q", got, "hello")
	}
	c.Write([]byte("!"))
	if got := c.buf.String(); got != "hello" {
		t.Errorf("captureWriter grew after reaching the limit: %q", got)
	}
}

func TestReplaceModelKeepsUnknownFields(t *testing.T) {
	in := []byte(`{"model":"sub:opus-5","max_tokens":7,"unbekannt":{"tief":[1,2]},"stream":true}`)
	out := replaceModel(in, "claude-opus-5")

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["model"] != "claude-opus-5" {
		t.Errorf("model = %v", m["model"])
	}
	if m["unbekannt"] == nil {
		t.Error("unknown fields must be preserved")
	}
	if m["stream"] != true || m["max_tokens"].(float64) != 7 {
		t.Errorf("known fields changed: %v", m)
	}
}

func TestPassthroughForwardsAndSniffsUsage(t *testing.T) {
	var gotModel string
	var gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &m)
		gotModel, _ = m["model"].(string)
		gotAuth = r.Header.Get("authorization")

		w.Header().Set("content-type", "text/event-stream")
		// message_start carries a nested cache_creation, which is exactly where
		// the previously used regex failed.
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":120,\"cache_read_input_tokens\":30,\"cache_creation\":{\"ephemeral_5m_input_tokens\":7}}}}\n\n")
		// Over 1 MB of filler: previously nothing after it was sniffed.
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"text\":\"%s\"}\n\n", strings.Repeat("x", 1_200_000))
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":456}}\n\n")
	}))
	defer up.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	rec := &nopRecorder{}
	px := New(func() *config.Config { return cfg }, rec)

	req := httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("authorization", "Bearer oauth-from-subscription")
	px.Messages(httptest.NewRecorder(), req)
	px.WaitForLogs()

	if gotModel != "opus-5" {
		t.Errorf("the provider must see the upstream model, got %q", gotModel)
	}
	if gotAuth != "Bearer oauth-from-subscription" {
		t.Errorf("the client credential must be passed through unchanged, got %q", gotAuth)
	}
	if rec.last.InputTokens != 120 {
		t.Errorf("InputTokens = %d, want 120: nested usage object not read", rec.last.InputTokens)
	}
	if rec.last.CacheRead != 30 {
		t.Errorf("CacheRead = %d, want 30", rec.last.CacheRead)
	}
	if rec.last.OutputTokens != 456 {
		t.Errorf("OutputTokens = %d, want 456: the end of the stream was not sniffed", rec.last.OutputTokens)
	}
}

// Auto mode lets the server check actions within the running request: it sends
// the safeguards field, and the result comes back as safeguard_results. Both
// must pass through unchanged, otherwise Claude Code falls back to its own paid
// classifier requests (siehe https://code.claude.com/docs/en/auto-mode-classifier-billing).
func TestPassthroughKeepsSafeguardsAndBeta(t *testing.T) {
	var gotBody map[string]any
	var gotBeta, gotVersion []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		gotBeta, gotVersion = r.Header.Values("anthropic-beta"), r.Header.Values("anthropic-version")
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5",`+
			`"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"rm -rf /"}}],`+
			`"safeguard_results":[{"tool_use_id":"toolu_1","verdict":"deny","reason":"destruktiv"}],"usage":{"input_tokens":1}}`)
	}))
	defer up.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	rec := &nopRecorder{}
	px := New(func() *config.Config { return cfg }, rec)

	body := `{"model":"sub:claude-sonnet-5","max_tokens":64,"safeguards":{"pending_actions":[{"tool_use_id":"toolu_1"}]},` +
		`"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Add("anthropic-beta", "oauth-2025-04-20")
	req.Header.Add("anthropic-beta", "safeguards-2026-01-01")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("x-anthropic-billing-header", "cc_version=2.1.283.d4e; cc_entrypoint=cli;")
	w := httptest.NewRecorder()
	px.Messages(w, req)
	px.WaitForLogs()

	if gotBody["safeguards"] == nil {
		t.Fatalf("safeguards was dropped: %v", gotBody)
	}
	if strings.Join(gotBeta, ",") != "oauth-2025-04-20,safeguards-2026-01-01" {
		t.Errorf("anthropic-beta = %v, both values must arrive", gotBeta)
	}
	if len(gotVersion) != 1 || gotVersion[0] != "2023-06-01" {
		t.Errorf("anthropic-version = %v", gotVersion)
	}
	if !strings.Contains(w.Body.String(), `"safeguard_results"`) {
		t.Errorf("safeguard_results missing from response: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"id":"toolu_1"`) {
		t.Errorf("tool_use ID was rewritten: %s", w.Body.String())
	}
}

func TestPassthroughRecordsErrorCode(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"too fast"}}`)
	}))
	defer up.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	rec := &nopRecorder{}
	px := New(func() *config.Config { return cfg }, rec)

	w := httptest.NewRecorder()
	px.Messages(w, httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[]}`)))
	px.WaitForLogs()

	if w.Code != 429 {
		t.Fatalf("Status = %d, want 429", w.Code)
	}
	if rec.last.Status != "error" || rec.last.ErrorCode != "429" {
		t.Errorf("Recorder = %+v, expected status=error and ErrorCode=429", rec.last)
	}
}

func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		in       string
		wantGone string
		mustKeep string
	}{
		{`Incorrect API key provided: sk-proj-AbCdEf0123456789XyZw`, "sk-proj-AbCdEf0123456789XyZw", "Incorrect API key provided"},
		{`auth failed: gsk_0123456789abcdefghij`, "gsk_0123456789abcdefghij", "auth failed"},
		{`token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0`, "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0", "token"},
		{`error code api_key_invalid`, "", "api_key_invalid"},
		{`token-limit-exceeded for this model`, "", "token-limit-exceeded"},
		{`key-not-found in namespace`, "", "key-not-found"},
	}
	for _, c := range cases {
		got := redactSecrets(c.in, "")
		if c.wantGone != "" && strings.Contains(got, c.wantGone) {
			t.Errorf("redactSecrets(%q) = %q: the secret is still there", c.in, got)
		}
		if !strings.Contains(got, c.mustKeep) {
			t.Errorf("redactSecrets(%q) = %q: %q should have been kept", c.in, got, c.mustKeep)
		}
	}
}

func TestBalancedObject(t *testing.T) {
	cases := map[string]string{
		`:{"a":1}`:         `{"a":1}`,
		`: {"a":{"b":2}}`:  `{"a":{"b":2}}`,
		`:{"a":"}{"}`:      `{"a":"}{"}`,
		`:{"a":"x\"}"}`:    `{"a":"x\"}"}`,
		`:{"a":1`:          ``,
		`:"not an object"`: ``,
	}
	for in, want := range cases {
		got := balancedObject([]byte(in))
		if string(got) != want {
			t.Errorf("balancedObject(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUsageSplitAcrossWrites(t *testing.T) {
	out := &Outcome{}
	u := &usageSniffer{out: out}
	// The "usage" anchor and its object are split across three writes.
	u.Write([]byte(`data: {"type":"message_delta","us`))
	u.Write([]byte(`age":{"output_`))
	u.Write([]byte(`tokens":789}}`))
	if out.OutputTokens != 789 {
		t.Errorf("OutputTokens = %d, want 789: a usage object split across writes was not found", out.OutputTokens)
	}
}

func TestRedactSecretsRemovesTheActualKey(t *testing.T) {
	// A key without a known prefix is not caught by the pattern list; the
	// actual value must remove it anyway.
	for _, key := range []string{"hauseigen_4711_ohne_praefix", "8f3c9a1b2d", "sk-proj-AbCdEf0123456789XyZw"} {
		msg := "Incorrect API key provided: " + key
		got := redactSecrets(msg, key)
		if strings.Contains(got, key) {
			t.Errorf("key %q still present in %q", key, got)
		}
		if !strings.Contains(got, "Incorrect API key provided") {
			t.Errorf("the rest of the message was lost: %q", got)
		}
	}
}

func TestProviderKeyIsEmptyForPassthrough(t *testing.T) {
	t.Setenv("SOME_KEY", "sk-secret")
	if got := providerKey(config.Provider{Auth: "passthrough", APIKeyEnv: "SOME_KEY"}); got != "" {
		t.Errorf("providerKey = %q: with passthrough the router holds no key", got)
	}
	if got := providerKey(config.Provider{Auth: "key", APIKeyEnv: "SOME_KEY"}); got != "sk-secret" {
		t.Errorf("providerKey = %q, want sk-secret", got)
	}
}

func TestRedactSecretsIgnoresImplausiblyShortKeys(t *testing.T) {
	// A value like "x" occurs in every message: literal replacement would
	// mangle the text instead of protecting a secret.
	got := redactSecrets("Your api key: x is invalid, expected a prefix", "x")
	if !strings.Contains(got, "expected a prefix") {
		t.Errorf("the message was mangled: %q", got)
	}
}

func TestPassthroughForwardsQueryString(t *testing.T) {
	var seen string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RequestURI()
		io.WriteString(w, `{"type":"message","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer up.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "none", BaseURL: up.URL},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})

	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages?beta=true",
		strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[]}`)))
	px.WaitForLogs()

	if seen != "/v1/messages?beta=true" {
		t.Errorf("provider saw %q: the query must be forwarded", seen)
	}
}
