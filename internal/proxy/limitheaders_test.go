package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/store"
)

func TestLimitHeadersKeepsOnlyLimitHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Anthropic-Ratelimit-Unified-Status", "rejected")
	h.Set("anthropic-ratelimit-unified-reset", "1790340000")
	h.Set("Retry-After", "120")
	h.Set("content-type", "application/json")
	h.Set("request-id", "req_123")

	raw := limitHeaders(h)
	if raw == nil {
		t.Fatal("limitHeaders = nil: the limit headers are missing")
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["anthropic-ratelimit-unified-status"] != "rejected" {
		t.Errorf("status missing: %v", got)
	}
	if got["anthropic-ratelimit-unified-reset"] != "1790340000" {
		t.Errorf("reset missing: %v", got)
	}
	if got["retry-after"] != "120" {
		t.Errorf("retry-after missing: %v", got)
	}
	if len(got) != 3 {
		t.Errorf("captured too much: %v", got)
	}
}

func TestLimitHeadersIsNilWithoutLimitHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("content-type", "application/json")
	if raw := limitHeaders(h); raw != nil {
		t.Errorf("limitHeaders = %q, want nil: otherwise every error response gets an empty document", raw)
	}
}

func TestPassthroughLogsLimitHeadersOnError(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("anthropic-ratelimit-unified-status", "rejected")
		w.Header().Set("anthropic-ratelimit-unified-reset", "1790340000")
		w.WriteHeader(429)
		io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"Error"}}`)
	}))
	defer up.Close()

	l, err := store.OpenLog(filepath.Join(t.TempDir(), "log.db"), store.StaticLimits("full", 7, 500, 256))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	px.SetMessageLog(l)

	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[]}`)))
	px.WaitForLogs()

	entries, err := l.List(10, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %v (%v)", entries, err)
	}
	docs, err := l.Get(entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(docs["headers"]), "unified-status") {
		t.Errorf("headers document missing or empty: %q", docs["headers"])
	}
}
