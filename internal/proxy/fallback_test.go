package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpielerNogard/claude-router/internal/config"
)

// rig wires provider sub with fallback ds (dsStatus nil = success) and counts calls to both.
type rig struct {
	px       *Proxy
	rec      *nopRecorder
	subCalls *int
	dsCalls  *int
}

func newRig(t *testing.T, subStatus func(http.ResponseWriter), dsStatus func(http.ResponseWriter),
	subFallback string) rig {
	t.Helper()
	subCalls, dsCalls := 0, 0
	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subCalls++
		subStatus(w)
	}))
	t.Cleanup(sub.Close)
	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dsCalls++
		if dsStatus != nil {
			dsStatus(w)
			return
		}
		io.WriteString(w, `{"id":"m","type":"message","role":"assistant","content":[],
		  "usage":{"input_tokens":3,"output_tokens":2}}`)
	}))
	t.Cleanup(ds.Close)

	t.Setenv("DS_KEY", "sk-test")
	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough",
			BaseURL: sub.URL, Fallback: subFallback},
		"ds": {Name: "ds", Format: "anthropic", Auth: "key", BaseURL: ds.URL, APIKeyEnv: "DS_KEY"},
	}}
	rec := &nopRecorder{}
	return rig{px: New(func() *config.Config { return cfg }, rec), rec: rec,
		subCalls: &subCalls, dsCalls: &dsCalls}
}

func (r rig) post(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.px.Messages(w, httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[]}`)))
	r.px.WaitForLogs()
	return w
}

// rejectedLimit signals an exhausted window the way Anthropic reports it.
func rejectedLimit(w http.ResponseWriter) {
	w.Header().Set("anthropic-ratelimit-unified-status", "rejected")
	// Reset relative to now: a fixed timestamp would expire after a few hours
	// and the test would start failing on its own.
	w.Header().Set("anthropic-ratelimit-unified-reset",
		strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
	w.WriteHeader(429)
	io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"Error"}}`)
}

func TestConfirmedLimitReroutesAndSticks(t *testing.T) {
	r := newRig(t, rejectedLimit, nil, "ds:deepseek-flash")

	for i := 0; i < 2; i++ {
		if w := r.post(t); w.Code != 200 {
			t.Fatalf("attempt %d: Status = %d, Body = %s", i+1, w.Code, w.Body.String())
		}
	}
	if *r.subCalls != 1 {
		t.Errorf("sub called %d times, want only once after the limit", *r.subCalls)
	}
	if *r.dsCalls != 2 {
		t.Errorf("ds called %d times, want 2: once as rescue, once directly", *r.dsCalls)
	}
	if r.rec.last.Provider != "ds" {
		t.Errorf("Provider = %q, want ds: the record must carry the serving provider", r.rec.last.Provider)
	}
	if r.rec.last.Alias != "sub:opus-5" {
		t.Errorf("Alias = %q, want sub:opus-5: the requested alias is kept", r.rec.last.Alias)
	}
}

func TestUnconfirmed429IsNotRerouted(t *testing.T) {
	// Without evidence of an exhausted window the model is not swapped:
	// neither on an ordinary rate limit nor while overage applies.
	cases := map[string]func(http.ResponseWriter){
		"without headers": func(w http.ResponseWriter) {
			w.WriteHeader(429)
			io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"Error"}}`)
		},
		"overage allowed": func(w http.ResponseWriter) {
			w.Header().Set("anthropic-ratelimit-unified-status", "rejected")
			w.Header().Set("anthropic-ratelimit-unified-overage-status", "allowed")
			w.Header().Set("anthropic-ratelimit-unified-reset",
				strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
			w.WriteHeader(429)
			io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"Error"}}`)
		},
		"status allowed": func(w http.ResponseWriter) {
			w.Header().Set("anthropic-ratelimit-unified-status", "allowed")
			w.WriteHeader(429)
			io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"Error"}}`)
		},
	}

	for name, status := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, status, nil, "ds:deepseek-flash")
			if w := r.post(t); w.Code != 429 {
				t.Errorf("Status = %d, want 429: the subscription's error must pass through", w.Code)
			}
			if *r.dsCalls != 0 {
				t.Errorf("fallback target called %d times, want 0", *r.dsCalls)
			}
			// No block: the next request may try the subscription again.
			r.post(t)
			if *r.subCalls != 2 {
				t.Errorf("sub called %d times, want 2: no block without evidence", *r.subCalls)
			}
		})
	}
}

func Test429WithoutFallbackPassesBodyThroughWhole(t *testing.T) {
	// Without fallback the request must pass through unchanged: the body used
	// to be streamed and arrive complete.
	big := strings.Repeat("x", 2<<20)
	r := newRig(t, func(w http.ResponseWriter) {
		w.Header().Set("content-type", "text/plain")
		w.WriteHeader(429)
		io.WriteString(w, big)
	}, nil, "")

	w := r.post(t)
	if w.Code != 429 {
		t.Fatalf("Status = %d, want 429", w.Code)
	}
	if got := w.Body.Len(); got != len(big) {
		t.Errorf("Body = %d bytes, want %d: the body was truncated", got, len(big))
	}
}

func TestNoCooldownWithoutFallback(t *testing.T) {
	// Without a fallback target there is nothing to reroute, so no block either:
	// otherwise a stale block would apply once a fallback is added later.
	r := newRig(t, rejectedLimit, nil, "")

	if w := r.post(t); w.Code != 429 {
		t.Fatalf("Status = %d, want 429", w.Code)
	}
	if r.px.cool.Blocked("sub", time.Now()) {
		t.Error("sub was blocked although no fallback is configured")
	}
	r.post(t)
	if *r.subCalls != 2 {
		t.Errorf("sub called %d times, want 2", *r.subCalls)
	}
}

func TestFallbackUnreachableStillDeliversTheLimit(t *testing.T) {
	// If the fallback is unreachable, the subscription's 429 with its reset is
	// a more useful answer than a 502 that hides the limit.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rejectedLimit(w)
	}))
	defer sub.Close()
	t.Setenv("DS_KEY", "sk-test")
	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: sub.URL,
			Fallback: "ds:m"},
		"ds": {Name: "ds", Format: "anthropic", Auth: "key", BaseURL: deadURL, APIKeyEnv: "DS_KEY"},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})

	w := httptest.NewRecorder()
	px.Messages(w, httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[]}`)))
	px.WaitForLogs()

	if w.Code != 429 {
		t.Fatalf("Status = %d, want 429: the limit must not be replaced by a 502", w.Code)
	}
	if w.Header().Get("anthropic-ratelimit-unified-reset") == "" {
		t.Error("the reset time is missing, which is exactly what the user needs")
	}
}

func TestFallbackFailing429StillReachesClientOnce(t *testing.T) {
	// The fallback can also answer 429: exactly one error response, no
	// hanging empty 200.
	r := newRig(t, rejectedLimit, func(w http.ResponseWriter) {
		w.WriteHeader(429)
		io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"ds overloaded"}}`)
	}, "ds:m")

	w := r.post(t)
	if w.Code != 429 {
		t.Fatalf("Status = %d, want 429", w.Code)
	}
	if n := strings.Count(w.Body.String(), "rate_limit_error"); n != 1 {
		t.Errorf("body contains %d error objects: %s", n, w.Body.String())
	}
	if *r.subCalls != 1 {
		t.Errorf("sub called %d times, want 1", *r.subCalls)
	}
}

func TestOpenAIFormat429IsNeverTreatedAsLimit(t *testing.T) {
	// Limit headers exist only for Anthropic-format providers. An OpenAI-format
	// one must not be rerouted even if it happens to send them.
	subCalls, dsCalls := 0, 0
	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subCalls++
		rejectedLimit(w)
	}))
	defer sub.Close()
	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dsCalls++
		io.WriteString(w, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"x"},
		  "finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer ds.Close()

	t.Setenv("DS_KEY", "sk-test")
	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub": {Name: "sub", Format: "openai", Auth: "key", BaseURL: sub.URL,
			APIKeyEnv: "DS_KEY", Fallback: "ds:m"},
		"ds": {Name: "ds", Format: "openai", Auth: "key", BaseURL: ds.URL, APIKeyEnv: "DS_KEY"},
	}}
	px := New(func() *config.Config { return cfg }, &nopRecorder{})

	w := httptest.NewRecorder()
	px.Messages(w, httptest.NewRequest("POST", "/v1/messages",
		strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[]}`)))
	px.WaitForLogs()

	if w.Code != 429 {
		t.Errorf("Status = %d, want 429", w.Code)
	}
	if dsCalls != 0 {
		t.Errorf("fallback target called %d times: only Anthropic-format providers are checked", dsCalls)
	}
	if subCalls != 1 {
		t.Errorf("sub called %d times, want 1", subCalls)
	}
}

func TestDebugLogNamesTheTargetActuallyServed(t *testing.T) {
	// In debug mode the log is the only record of where a request went, so it
	// must not name the target before rerouting.
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	r := newRig(t, rejectedLimit, nil, "ds:deepseek-flash")
	r.px.SetDebug(true)
	r.post(t) // first request sets the block
	buf.Reset()
	r.post(t) // from here on the fallback serves requests

	got := buf.String()
	if strings.Contains(got, "-> sub:opus-5") {
		t.Errorf("debug log names the target before rerouting:\n%s", got)
	}
	if !strings.Contains(got, "-> ds:deepseek-flash") {
		t.Errorf("debug log does not name the serving target:\n%s", got)
	}
}

func TestWritePendingDropsUpstreamContentLength(t *testing.T) {
	// The provider's headers are not passed through: a content-length that
	// does not match the truncated body makes the client hit an unexpected EOF.
	p := &pendingError{
		Status:  429,
		Headers: http.Header{"Content-Length": {"3145728"}, "Content-Type": {"application/json"}},
		Body:    []byte(`{"type":"error"}`),
	}
	w := httptest.NewRecorder()
	writePending(w, p)

	if got := w.Header().Get("content-length"); got != "" {
		t.Errorf("content-length = %q, want empty: the provider's value does not match the body", got)
	}
	if w.Code != 429 || w.Body.String() != `{"type":"error"}` {
		t.Errorf("Status %d, Body %q", w.Code, w.Body.String())
	}
}

func TestFallbackChainKeepsModelAndSticks(t *testing.T) {
	// sub and account2 are exhausted: the first request walks the chain, the
	// second goes straight to ds. account2 receives the requested model.
	var account2Models []string
	subCalls, dsCalls := 0, 0
	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subCalls++
		rejectedLimit(w)
	}))
	defer sub.Close()
	account2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		account2Models = append(account2Models, string(body))
		rejectedLimit(w)
	}))
	defer account2.Close()
	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dsCalls++
		io.WriteString(w, `{"id":"m","type":"message","role":"assistant","content":[],
		  "usage":{"input_tokens":3,"output_tokens":2}}`)
	}))
	defer ds.Close()

	t.Setenv("DS_KEY", "sk-test")
	cfg := &config.Config{Providers: map[string]config.Provider{
		"sub":      {Name: "sub", Format: "anthropic", Auth: "passthrough", BaseURL: sub.URL, Fallback: "account2"},
		"account2": {Name: "account2", Format: "anthropic", Auth: "passthrough", BaseURL: account2.URL, Fallback: "ds:m"},
		"ds":       {Name: "ds", Format: "anthropic", Auth: "key", BaseURL: ds.URL, APIKeyEnv: "DS_KEY"},
	}}
	rec := &nopRecorder{}
	px := New(func() *config.Config { return cfg }, rec)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		px.Messages(w, httptest.NewRequest("POST", "/v1/messages",
			strings.NewReader(`{"model":"sub:opus-5","max_tokens":5,"messages":[]}`)))
		px.WaitForLogs()
		if w.Code != 200 {
			t.Fatalf("attempt %d: Status = %d, Body = %s", i+1, w.Code, w.Body.String())
		}
	}
	if subCalls != 1 || len(account2Models) != 1 || dsCalls != 2 {
		t.Errorf("calls sub=%d account2=%d ds=%d, want 1/1/2", subCalls, len(account2Models), dsCalls)
	}
	if len(account2Models) == 1 && !strings.Contains(account2Models[0], `"opus-5"`) {
		t.Errorf("account2 got %s: the requested model must be kept", account2Models[0])
	}
	if rec.last.Provider != "ds" || rec.last.Alias != "sub:opus-5" {
		t.Errorf("record %s/%s, want ds/sub:opus-5", rec.last.Provider, rec.last.Alias)
	}
}
