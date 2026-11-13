package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SpielerNogard/claude-router/internal/config"
)

func unifiedHeaders(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(kv); i += 2 {
		h.Set("anthropic-ratelimit-unified-"+kv[i], kv[i+1])
	}
	return h
}

func TestLimitsNoteReadsWindows(t *testing.T) {
	l := newLimits()
	now := time.Unix(1790900000, 0)
	l.note("sub", unifiedHeaders(
		"status", "allowed",
		"5h-utilization", "0.64", "5h-reset", "1790936400", "5h-status", "allowed",
		"7d-utilization", "1.0", "7d-reset", "1790996400", "7d-status", "rejected",
	), now)

	got := l.snapshot()["sub"]
	if got.MeasuredAt == nil || !got.MeasuredAt.Equal(now) {
		t.Errorf("measured_at = %v", got.MeasuredAt)
	}
	w5, w7 := got.Windows["5h"], got.Windows["7d"]
	if w5.Utilization != 0.64 || w5.Status != "allowed" || !w5.Reset.Equal(time.Unix(1790936400, 0)) {
		t.Errorf("5h = %+v", w5)
	}
	if w7.Utilization != 1.0 || w7.Status != "rejected" {
		t.Errorf("7d = %+v", w7)
	}
}

func TestLimitsIgnoresResponsesWithoutLimiter(t *testing.T) {
	l := newLimits()
	l.note("sub", unifiedHeaders("5h-utilization", "0.5"), time.Now())
	if _, ok := l.snapshot()["sub"]; ok {
		t.Error("without unified-status the limiter did not run, the values do not count")
	}
}

func TestLimitsKeepOldWindowsWhenAnswerOmitsThem(t *testing.T) {
	l := newLimits()
	l.note("sub", unifiedHeaders("status", "allowed", "7d-utilization", "0.3", "7d-reset", "1790996400"), time.Now())
	l.note("sub", unifiedHeaders("status", "allowed", "5h-utilization", "0.1", "5h-reset", "1790936400"), time.Now())
	if w := l.snapshot()["sub"].Windows; w["7d"].Utilization != 0.3 || w["5h"].Utilization != 0.1 {
		t.Errorf("windows = %+v", w)
	}
}

func TestProviderLimitsFromPassthroughAndKinds(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("anthropic-ratelimit-unified-status", "allowed")
		w.Header().Set("anthropic-ratelimit-unified-5h-utilization", "0.42")
		w.Header().Set("anthropic-ratelimit-unified-5h-reset", "1790936400")
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"id":"m","type":"message","role":"assistant","content":[],"usage":{"input_tokens":1}}`)
	}))
	defer up.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub":      {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
		"account2": {Name: "account2", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
		"deepseek": {Name: "deepseek", Format: "openai", Auth: "none", BaseURL: up.URL},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[]}`)))
	px.WaitForLogs()

	got := px.ProviderLimits()
	if got["sub"].Kind != "window" || got["sub"].Windows["5h"].Utilization != 0.42 {
		t.Errorf("sub = %+v", got["sub"])
	}
	if got["account2"].Kind != "window" || len(got["account2"].Windows) != 0 {
		t.Errorf("account2 without a request must be empty: %+v", got["account2"])
	}
	if got["deepseek"].Kind != "unlimited" {
		t.Errorf("deepseek = %+v", got["deepseek"])
	}
}

func TestProviderLimitsTakenFrom429(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("anthropic-ratelimit-unified-status", "rejected")
		w.Header().Set("anthropic-ratelimit-unified-7d-utilization", "1.0")
		w.Header().Set("anthropic-ratelimit-unified-7d-status", "rejected")
		w.WriteHeader(429)
		io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"x"}}`)
	}))
	defer up.Close()
	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[]}`)))
	px.WaitForLogs()

	if w := px.ProviderLimits()["sub"].Windows["7d"]; w.Utilization != 1.0 || w.Status != "rejected" {
		t.Errorf("7d = %+v", w)
	}
}

func TestProviderLimitsJSONOmitsUnknown(t *testing.T) {
	cfg := &config.Config{Providers: map[string]config.Provider{
		"account2": {Name: "account2", Format: "anthropic", Auth: "passthrough"},
		"lmstudio": {Name: "lmstudio", Format: "openai", Auth: "none"},
	}}
	b, _ := json.Marshal(New(func() *config.Config { return cfg }, &nopRecorder{}).ProviderLimits())
	if got := string(b); got != `{"account2":{"kind":"window"},"lmstudio":{"kind":"unlimited"}}` {
		t.Errorf("JSON = %s", got)
	}
}
