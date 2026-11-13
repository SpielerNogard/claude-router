package upstream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/credstore"
)

func writeToken(t *testing.T, dir, token string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"claudeAiOauth": map[string]any{
		"accessToken": token, "expiresAt": time.Now().Add(2 * time.Hour).UnixMilli(),
	}})
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Anthropic accepts only "valid"; everything else counts as revoked.
func tokenServer(t *testing.T, calls *atomic.Int32) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("authorization") != "Bearer valid" {
			w.WriteHeader(401)
			w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"OAuth access token has been revoked."}}`))
			return
		}
		w.Write([]byte(`ok`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func cachedRequest(t *testing.T, dir, url string, p config.Provider) *http.Request {
	t.Helper()
	req, err := http.NewRequest("POST", url, strings.NewReader(`{"model":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyAuth(req.Header, p, http.Header{}); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestSendRetriesOnceWithRenewedToken(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { credstore.Forget(dir) })
	p := config.Provider{Auth: "claude-credential-helper", ConfigDir: dir}
	var calls atomic.Int32
	srv := tokenServer(t, &calls)

	writeToken(t, dir, "revoked")
	req := cachedRequest(t, dir, srv.URL, p)
	// Claude Code renews the token while the router still holds the old one in its cache.
	writeToken(t, dir, "valid")

	resp, err := Send(srv.Client(), req, p)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || calls.Load() != 2 {
		t.Errorf("status %d after %d calls, want 200 after 2", resp.StatusCode, calls.Load())
	}
}

func TestSendPassesRealLogoutThrough(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { credstore.Forget(dir) })
	p := config.Provider{Auth: "claude-credential-helper", ConfigDir: dir}
	var calls atomic.Int32
	srv := tokenServer(t, &calls)

	writeToken(t, dir, "revoked")
	resp, err := Send(srv.Client(), cachedRequest(t, dir, srv.URL, p), p)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 || calls.Load() != 1 {
		t.Errorf("status %d after %d calls, want the 401 without a retry", resp.StatusCode, calls.Load())
	}
}

func TestSendDoesNotRetryOtherAuthModes(t *testing.T) {
	var calls atomic.Int32
	srv := tokenServer(t, &calls)
	p := config.Provider{Auth: "passthrough"}
	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(`{}`))
	resp, err := Send(srv.Client(), req, p)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls.Load() != 1 {
		t.Errorf("passthrough must not retry, %d calls", calls.Load())
	}
}
