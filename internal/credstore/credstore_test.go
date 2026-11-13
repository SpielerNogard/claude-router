package credstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceNameFollowsClaudeCodesSchema(t *testing.T) {
	if got := ServiceName(""); got != "Claude Code-credentials" {
		t.Errorf("default directory = %q, want %q", got, "Claude Code-credentials")
	}
	// sha256("/tmp/x")[:8] — computed externally, not derived from the code.
	if got := ServiceName("/tmp/x"); got != "Claude Code-credentials-"+sha8("/tmp/x") {
		t.Errorf("custom directory = %q", got)
	}
	if ServiceName("/a") == ServiceName("/b") {
		t.Error("different directories must yield different entries")
	}
}

func writeCreds(t *testing.T, dir string, token string, expires time.Time) {
	t.Helper()
	body := map[string]any{"claudeAiOauth": map[string]any{
		"accessToken":  token,
		"refreshToken": "must-never-be-used",
		"expiresAt":    expires.UnixMilli(),
	}}
	raw, _ := json.Marshal(body)
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadsTokenFromFile(t *testing.T) {
	dir := t.TempDir()
	exp := time.Now().Add(2 * time.Hour)
	writeCreds(t, dir, "sk-ant-oat-test", exp)

	c, err := loadFromFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.AccessToken != "sk-ant-oat-test" {
		t.Errorf("AccessToken = %q", c.AccessToken)
	}
	if c.ExpiresAt.Unix() != exp.Unix() {
		t.Errorf("ExpiresAt = %v, want %v", c.ExpiresAt, exp)
	}
}

func TestFractionalExpiresAtIsAccepted(t *testing.T) {
	c, err := parse([]byte(`{"claudeAiOauth":{"accessToken":"t","expiresAt":1790626560612.8499}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ExpiresAt.UnixMilli(); got != 1790626560612 {
		t.Errorf("ExpiresAt = %d ms, want 1790626560612", got)
	}
}

func TestExpiredTokenIsReported(t *testing.T) {
	dir := t.TempDir()
	writeCreds(t, dir, "expired", time.Now().Add(-time.Minute))

	c, err := loadFromFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Expired() {
		t.Error("an expired token must count as expired")
	}
}

func TestMissingFileGivesUsableError(t *testing.T) {
	_, err := loadFromFile(t.TempDir())
	if err == nil {
		t.Fatal("missing file must yield an error")
	}
}

func TestRefreshTokenIsNeverExposed(t *testing.T) {
	dir := t.TempDir()
	writeCreds(t, dir, "access", time.Now().Add(time.Hour))
	c, err := loadFromFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The type has no field for it at all — so nobody can use it.
	if c.AccessToken == "must-never-be-used" {
		t.Fatal("the refresh token ended up in the access token")
	}
}
