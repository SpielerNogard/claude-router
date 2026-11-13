package alias

import "testing"

func TestRoundTrip(t *testing.T) {
	for _, in := range []string{
		"deepseek:deepseek-flash",
		"sub:claude-opus-5",
		"openrouter:vendor/model:free",
		"llama3:8b",
	} {
		enc := Encode(in)
		if !IsEncoded(enc) {
			t.Errorf("%q does not yield a recognizable ID: %q", in, enc)
		}
		got, ok := Decode(enc)
		if !ok || got != in {
			t.Errorf("Decode(Encode(%q)) = %q, %v", in, got, ok)
		}
	}
}

func TestEncodedLooksLikeAnAnthropicModel(t *testing.T) {
	// Claude Code only puts IDs that look like Anthropic models in the picker — the whole point of this encoding.
	enc := Encode("deepseek:deepseek-flash")
	if len(enc) < len(prefix) || enc[:len(prefix)] != prefix {
		t.Fatalf("ID %q does not start with %q", enc, prefix)
	}
}

func TestContextSuffixStaysReadable(t *testing.T) {
	// Claude Code interprets [1m] itself, so it must not disappear into the hex.
	enc := Encode("sub:claude-opus-5[1m]")
	if enc[len(enc)-4:] != "[1m]" {
		t.Errorf("ID %q does not carry the suffix visibly", enc)
	}
	got, ok := Decode(enc)
	if !ok || got != "sub:claude-opus-5[1m]" {
		t.Errorf("Decode = %q, %v", got, ok)
	}
}

func TestDecodeLeavesPlainNamesAlone(t *testing.T) {
	for _, in := range []string{"deepseek:deepseek-flash", "claude-opus-5", "anthropic/claude-sonnet-5"} {
		if got, ok := Decode(in); ok {
			t.Errorf("Decode(%q) = %q, should not count as encoded", in, got)
		}
	}
}

func TestDecodeRejectsBrokenHex(t *testing.T) {
	if _, ok := Decode(prefix + "nothex!"); ok {
		t.Error("unreadable hex must not count as valid")
	}
}
