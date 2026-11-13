package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/store"
)

type nopRecorder struct{ last store.Record }

func (n *nopRecorder) Record(r store.Record) { n.last = r }

func proxyWith(t *testing.T, upstream *httptest.Server, format string) (*Proxy, *nopRecorder) {
	t.Helper()
	t.Setenv("UP_KEY", "sk-test")
	cfg := &config.Config{Providers: map[string]config.Provider{
		"up": {Name: "up", Format: format, Auth: "key", BaseURL: upstream.URL, APIKeyEnv: "UP_KEY"},
	}}
	rec := &nopRecorder{}
	return New(func() *config.Config { return cfg }, rec), rec
}

func post(t *testing.T, px *Proxy, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	w := httptest.NewRecorder()
	px.Messages(w, r)
	px.WaitForLogs()
	return w
}

func TestTranslatePathReturnsAnthropicShape(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer sk-test" {
			t.Errorf("provider key missing: %q", r.Header.Get("authorization"))
		}
		io.WriteString(w, `{"id":"c1","model":"m","choices":[{"index":0,
		  "message":{"role":"assistant","content":"hallo"},"finish_reason":"stop"}],
		  "usage":{"prompt_tokens":7,"completion_tokens":3}}`)
	}))
	defer up.Close()

	px, rec := proxyWith(t, up, "openai")
	w := post(t, px, `{"model":"up:m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != 200 {
		t.Fatalf("Status = %d, Body = %s", w.Code, w.Body.String())
	}
	var a map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if a["type"] != "message" || a["model"] != "up:m" {
		t.Errorf("response = %v", a)
	}
	if rec.last.InputTokens != 7 || rec.last.OutputTokens != 3 {
		t.Errorf("Recorder = %+v", rec.last)
	}
}

func TestMissingUsageKeepsCostNil(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"c1","model":"m","choices":[{"index":0,
		  "message":{"role":"assistant","content":"hallo"},"finish_reason":"stop"}]}`)
	}))
	defer up.Close()

	px, rec := proxyWith(t, up, "openai")
	w := post(t, px, `{"model":"up:gpt-4o","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != 200 {
		t.Fatalf("Status = %d, Body = %s", w.Code, w.Body.String())
	}
	if rec.last.CostUSD != nil {
		t.Errorf("without token counts CostUSD must stay nil, got %v", *rec.last.CostUSD)
	}
}

func TestCachedOnlyRequestStillGetsCost(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"type":"message","role":"assistant","content":[],
		  "usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":5000}}`)
	}))
	defer up.Close()

	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: up.URL},
	}}
	rec := &nopRecorder{}
	px := New(func() *config.Config { return cfg }, rec)

	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"sub:claude-sonnet-4-5","max_tokens":5,"messages":[]}`)))
	px.WaitForLogs()

	if rec.last.CacheRead != 5000 {
		t.Fatalf("CacheRead = %d, want 5000", rec.last.CacheRead)
	}
	if rec.last.CostUSD == nil {
		t.Error("a request served entirely from cache has real usage: CostUSD must not be nil")
	}
}

func TestUpstreamErrorStatusIsPreserved(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer up.Close()

	px, rec := proxyWith(t, up, "openai")
	w := post(t, px, `{"model":"up:m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != 429 {
		t.Fatalf("429 from provider must arrive as 429, got %d", w.Code)
	}
	if rec.last.Status != "error" || rec.last.ErrorCode != "429" {
		t.Errorf("error must be recorded: %+v", rec.last)
	}
}

func TestNonJSONUpstreamBecomes502NotPanic(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html")
		io.WriteString(w, "<html><body>502 Bad Gateway</body></html>")
	}))
	defer up.Close()

	px, _ := proxyWith(t, up, "openai")
	w := post(t, px, `{"model":"up:m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != 502 {
		t.Fatalf("unreadable provider response must yield 502, got %d", w.Code)
	}
	var e struct {
		Type  string                         `json:"type"`
		Error struct{ Type, Message string } `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("the error must also have Anthropic shape: %s", w.Body.String())
	}
	if e.Type != "error" {
		t.Errorf("error shape = %+v", e)
	}
	if !strings.Contains(e.Error.Message, "Bad Gateway") {
		t.Error("the message should contain an excerpt of the provider response")
	}
}

func TestUnknownAliasIs404InAnthropicShape(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer up.Close()
	px, _ := proxyWith(t, up, "openai")
	w := post(t, px, `{"model":"gibtsnicht:m","max_tokens":10,"messages":[]}`)
	if w.Code != 404 {
		t.Fatalf("Status = %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "not_found_error") {
		t.Errorf("Body = %s", w.Body.String())
	}
}

func TestMalformedRequestBodyIs400(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer up.Close()
	px, _ := proxyWith(t, up, "openai")
	w := post(t, px, `{this is not json`)
	if w.Code != 400 {
		t.Fatalf("Status = %d, want 400", w.Code)
	}
}

func TestDebugLogsResolution(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"x"},
		  "finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer up.Close()
	t.Setenv("UP_KEY", "sk")

	cfg := &config.Config{Providers: map[string]config.Provider{
		"up": {Name: "up", Format: "openai", Auth: "key", BaseURL: up.URL, APIKeyEnv: "UP_KEY", Default: true},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})
	px.SetDebug(true)

	px.Messages(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"claude-opus-5","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`)))
	px.WaitForLogs()

	out := buf.String()
	if !strings.Contains(out, `model "claude-opus-5"`) {
		t.Errorf("the requested name is missing: %s", out)
	}
	if !strings.Contains(out, "up:claude-opus-5") {
		t.Errorf("the resolved target is missing: %s", out)
	}
}
