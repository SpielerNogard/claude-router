package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/discover"
	"github.com/SpielerNogard/claude-router/internal/proxy"
	"github.com/SpielerNogard/claude-router/internal/store"
)

func testDeps(t *testing.T) deps {
	t.Helper()
	dir := t.TempDir()

	cfg := &config.Config{
		Listen: "127.0.0.1:4141",
		Log:    config.LogConfig{Level: "off", KeepDays: 7, MaxSizeMB: 500, MaxBodyKB: 256},
		Providers: map[string]config.Provider{
			"subscription": {Name: "subscription", Format: "anthropic", Auth: "passthrough"},
		},
	}
	reg := config.NewRegistry(filepath.Join(dir, "config.toml"), cfg)

	metrics, err := store.Open(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { metrics.Close() })

	msglog, err := store.OpenLog(filepath.Join(dir, "messages.db"), store.StaticLimits("off", 7, 500, 256))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { msglog.Close() })

	rec := proxy.NewAsyncRecorder(metrics, 16)
	t.Cleanup(rec.Close)

	models := discover.NewRegistry(reg.Get, metrics)
	px := proxy.New(reg.Get, rec)
	px.SetMessageLog(msglog)

	return deps{reg: reg, proxy: px, store: metrics, log: msglog, models: models}
}

func TestRoutesAreRegistered(t *testing.T) {
	mux := http.NewServeMux()
	registerRoutes(mux, testDeps(t))
	for _, tc := range []struct {
		method, path string
		notWant      int
	}{
		{"POST", "/v1/messages", 404},
		{"POST", "/v1/messages/count_tokens", 404},
		{"GET", "/v1/models", 404},
		{"GET", "/api/usage", 404},
		{"GET", "/api/messages/by-request/x", 404},
		{"GET", "/api/config", 404},
		{"GET", "/healthz", 404},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code == tc.notWant {
			t.Errorf("%s %s is not registered", tc.method, tc.path)
		}
	}
}

func TestHealthzAnswers(t *testing.T) {
	mux := http.NewServeMux()
	registerRoutes(mux, testDeps(t))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
}

func TestConfigDirUsesDotConfigOnNonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("applies only outside Windows")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config")
	if dir != want {
		t.Errorf("configDir() = %q, want %q — spec and README name ~/.config/claude-router", dir, want)
	}
}

func TestConfigMissingMessageNamesPathAndExample(t *testing.T) {
	msg := configMissingMessage("/tmp/somewhere/config.toml")
	if !strings.Contains(msg, "/tmp/somewhere/config.toml") {
		t.Errorf("message should name the expected path: %q", msg)
	}
	if !strings.Contains(msg, "format = \"anthropic\"") || !strings.Contains(msg, "auth   = \"passthrough\"") {
		t.Errorf("message should show a minimal example that starts without environment variables: %q", msg)
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:4141", true},
		{"127.0.0.1", true},
		{"127.0.0.2:4141", true}, // all of 127.0.0.0/8 is loopback
		{"::1", true},
		{"[::1]:4141", true},
		{"[::1]", true}, // brackets without port
		{"localhost:4141", true},
		{"localhost", true},
		{"LOCALHOST:4141", true}, // hostname is case-insensitive
		{":4141", false},
		{"", false},
		{"0.0.0.0:4141", false},
		{"192.168.1.5:4141", false},
		{"example.com:4141", false},
	}
	for _, tc := range cases {
		if got := isLoopbackAddr(tc.addr); got != tc.want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestAPIGate(t *testing.T) {
	cases := []struct {
		token, listen string
		hasKey        bool
		wantErr       bool
	}{
		{"", "127.0.0.1:4141", false, false},
		{"", "localhost:4141", false, false},
		{"", "[::1]:4141", false, false},
		{"", ":4141", false, true},
		{"", "0.0.0.0:4141", false, true},
		{"", "192.168.1.5:4141", false, true},
		{"", "0.0.0.0:4141", true, true},
		{"", "127.0.0.1:4141", true, false},
		{"secret", "0.0.0.0:4141", false, false},
		{"secret", "0.0.0.0:4141", true, false},
		{"secret", "127.0.0.1:4141", false, false},
	}
	for _, c := range cases {
		err := apiGate(c.token, c.listen, c.hasKey)
		if (err != nil) != c.wantErr {
			t.Errorf("apiGate(%q, %q, %v) = %v, want error: %v", c.token, c.listen, c.hasKey, err, c.wantErr)
		}
	}
}

func TestAPIGateMessageMentionsBothRisks(t *testing.T) {
	err := apiGate("", "0.0.0.0:4141", true)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "/api/") || !strings.Contains(err.Error(), "/v1/") {
		t.Errorf("message should name both cases: %v", err)
	}
}

func alwaysGate() bool { return true }
func neverGate() bool  { return false }

func TestWithAPITokenNoTokenPassesThrough(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := withAPIToken("", neverGate, inner)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/config", nil))
	if w.Code != 200 {
		t.Errorf("without a token /api/ must pass through, status = %d", w.Code)
	}
}

func TestWithAPITokenRejectsMissingOrWrongToken(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := withAPIToken("secret", neverGate, inner)

	for _, authHeader := range []string{"", "Bearer", "Bearer wrong", "secret"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/config", nil)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		h.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Authorization=%q: status = %d, want 401", authHeader, w.Code)
		}
		if ct := w.Header().Get("content-type"); ct != "application/json" {
			t.Errorf("Authorization=%q: content-type = %q, want application/json", authHeader, ct)
		}
	}
}

func TestWithAPITokenAcceptsCorrectBearer(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := withAPIToken("secret", neverGate, inner)

	for _, authHeader := range []string{"Bearer secret", "bearer secret", "BEARER secret"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/config", nil)
		req.Header.Set("Authorization", authHeader)
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("Authorization=%q: status = %d, want 200", authHeader, w.Code)
		}
	}
}

func TestWithAPITokenProtectsMessageDetail(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("GET /api/messages/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := withAPIToken("secret", neverGate, inner)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/messages/42", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("without token: status = %d, want 401", w.Code)
	}

	w = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/messages/42", nil)
	req.Header.Set("Authorization", "Bearer secret")
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("with correct token: status = %d, want 200", w.Code)
	}
}

func v1Handler() *http.ServeMux {
	inner := http.NewServeMux()
	inner.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	return inner
}

func TestV1GatedCombinations(t *testing.T) {
	withKey := &config.Config{Providers: map[string]config.Provider{"p": {Auth: "key"}}}
	withoutKey := &config.Config{Providers: map[string]config.Provider{"p": {Auth: "passthrough"}}}
	cases := []struct {
		name   string
		listen string
		cfg    *config.Config
		want   bool
	}{
		{"loopback, without key provider", "127.0.0.1:4141", withoutKey, false},
		{"loopback, with key provider", "127.0.0.1:4141", withKey, false},
		{"non-loopback, without key provider", "0.0.0.0:4141", withoutKey, false},
		{"non-loopback, with key provider", "0.0.0.0:4141", withKey, true},
	}
	for _, c := range cases {
		if got := v1Gated(c.listen, c.cfg); got != c.want {
			t.Errorf("%s: v1Gated = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWithAPITokenV1HeaderCheck(t *testing.T) {
	cases := []struct {
		name    string
		header  string
		wantErr bool
	}{
		{"no header", "", true},
		{"wrong header", "wrong", true},
		{"correct header", "secret", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := withAPIToken("secret", alwaysGate, v1Handler())
			req := httptest.NewRequest("POST", "/v1/messages", nil)
			if c.header != "" {
				req.Header.Set("X-Router-Token", c.header)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if (w.Code != 200) != c.wantErr {
				t.Errorf("status = %d, want error: %v", w.Code, c.wantErr)
			}
		})
	}
}

func TestWithAPITokenV1RejectionIsAnthropicShape(t *testing.T) {
	h := withAPIToken("secret", alwaysGate, v1Handler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))

	var v struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("not valid JSON: %s", w.Body.String())
	}
	if v.Type != "error" || v.Error.Type == "" {
		t.Errorf("want Anthropic error shape, got: %s", w.Body.String())
	}
	if !strings.Contains(v.Error.Message, "ANTHROPIC_CUSTOM_HEADERS") {
		t.Errorf("message should point to ANTHROPIC_CUSTOM_HEADERS: %q", v.Error.Message)
	}
}

func TestWithAPITokenV1GateOffLeavesV1Unprotected(t *testing.T) {
	h := withAPIToken("secret", neverGate, v1Handler())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	if w.Code != 200 {
		t.Errorf("without a key provider or on loopback /v1/ must stay reachable without header, status = %d", w.Code)
	}
}

func TestWithAPITokenLeavesUIOpen(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("GET /ui/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := withAPIToken("secret", neverGate, inner)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/ui/", nil))
	if w.Code != 200 {
		t.Errorf("/ui/ serves only static files and needs no token, status = %d", w.Code)
	}
}

func TestWithAPITokenProtectsLimits(t *testing.T) {
	inner := http.NewServeMux()
	inner.HandleFunc("GET /api/limits", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	w := httptest.NewRecorder()
	withAPIToken("secret", neverGate, inner).ServeHTTP(w, httptest.NewRequest("GET", "/api/limits", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("/api/limits without token: status = %d, want 401", w.Code)
	}
}

func TestCleanupDue(t *testing.T) {
	last := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		over bool
		now  time.Time
		want bool
	}{
		{"under the limit, one hour later", false, last.Add(time.Hour), false},
		{"over the limit", true, last.Add(time.Hour), true},
		{"daily cycle for keep_days", false, last.Add(24 * time.Hour), true},
	} {
		if got := cleanupDue(tc.over, last, tc.now); got != tc.want {
			t.Errorf("%s: cleanupDue = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestUseTUI(t *testing.T) {
	cases := []struct {
		name                        string
		noTUI, debug, stdin, stdout bool
		want                        bool
	}{
		{"terminal", false, false, true, true, true},
		{"--no-tui", true, false, true, true, false},
		{"--debug", false, true, true, true, false},
		{"stdin not a terminal", false, false, false, true, false},
		{"stdout piped", false, false, true, false, false},
	}
	for _, c := range cases {
		if got := useTUI(c.noTUI, c.debug, c.stdin, c.stdout); got != c.want {
			t.Errorf("%s: useTUI = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRunForegroundKeepsServingWhenUIFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runForeground(ctx, func(context.Context) error { return errors.New("boom") })
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("router stopped because the UI failed")
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runForeground did not return after cancel")
	}
}

func TestRunForegroundReturnsOnUIQuit(t *testing.T) {
	done := make(chan struct{})
	go func() {
		runForeground(context.Background(), func(context.Context) error { return nil })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("quit in the UI did not stop the router")
	}
}
