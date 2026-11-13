package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/convert"
	"github.com/SpielerNogard/claude-router/internal/pricing"
	"github.com/SpielerNogard/claude-router/internal/store"
)

// newRequestID gives each request its own ID; unlike the alias (the same for
// every request to the same model) it uniquely links a message log entry to
// its cost row in requests.
func newRequestID() string {
	b := make([]byte, 16)
	// crypto/rand.Read practically never fails on supported platforms; on error
	// b stays partly or fully zero and the request proceeds anyway.
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type Recorder interface{ Record(store.Record) }

type Proxy struct {
	cfg    func() *config.Config
	client *http.Client
	rec    Recorder
	log    *store.MsgLog
	onReq  func(provider string, h http.Header)
	cool   *Cooldown
	limits *limits
	debug  bool
	logWG  sync.WaitGroup
}

func (px *Proxy) SetDebug(on bool) { px.debug = on }

func New(cfgFn func() *config.Config, rec Recorder) *Proxy {
	return &Proxy{
		cfg: cfgFn, rec: rec,
		// No overall timeout: a long stream is normal. Cancellation is driven by
		// the client's request context.
		client: &http.Client{Timeout: 0},
		cool:   NewCooldown(),
		limits: newLimits(),
	}
}

func (px *Proxy) SetMessageLog(l *store.MsgLog) { px.log = l }

// BlockedProviders reports exhausted providers with their reset time, so an
// active block is visible beyond the log.
func (px *Proxy) BlockedProviders() map[string]string {
	blocked := px.cool.BlockedUntil(time.Now())
	if len(blocked) == 0 {
		return nil
	}
	out := make(map[string]string, len(blocked))
	for name, until := range blocked {
		out[name] = until.Format(time.RFC3339)
	}
	return out
}

func (px *Proxy) OnRequest(fn func(provider string, h http.Header)) { px.onReq = fn }

// Collects a request's documents for the message log; on passthrough
// "translated" and "upstream" stay empty since nothing is translated.
type docs struct {
	m map[string][]byte
}

func newDocs() *docs { return &docs{m: map[string][]byte{}} }

func (d *docs) put(kind string, b []byte) {
	if d == nil || len(b) == 0 {
		return
	}
	d.m[kind] = append([]byte(nil), b...)
}

// Writing runs in its own goroutine: for a stream the final chunk only goes
// out once the handler returns, so logging must not hold up the client.
func (px *Proxy) writeLog(requestID string, d *docs) {
	px.logWG.Add(1)
	go func() {
		defer px.logWG.Done()
		px.writeLogNow(requestID, d)
	}()
}

// WaitForLogs waits until all pending log writes are done, for shutdown and
// for tests that check what was written.
func (px *Proxy) WaitForLogs() { px.logWG.Wait() }

func (px *Proxy) writeLogNow(requestID string, d *docs) {
	if px.log == nil || !px.log.Enabled() {
		return
	}
	// At level meta an entry without documents is created, by design.
	m := map[string][]byte{}
	if d != nil {
		m = d.m
	}
	if err := px.log.Write(requestID, m); err != nil {
		log.Printf("message-log: %v", err)
	}
}

func (px *Proxy) Messages(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	body, err := readLimited(r)
	if err != nil {
		writeError(w, 400, "invalid_request_error", err.Error())
		return
	}
	var a convert.AnthropicRequest
	if err := json.Unmarshal(body, &a); err != nil {
		writeError(w, 400, "invalid_request_error", "request body is not valid JSON: "+err.Error())
		return
	}
	tgt, rerr := Resolve(px.cfg(), a.Model)
	if rerr != nil {
		if px.debug {
			logResolution(a.Model, tgt, rerr, a.Stream)
		}
		writeError(w, rerr.Status, rerr.Kind, rerr.Message)
		return
	}
	tgt = px.applyCooldown(tgt)
	wantAlias := tgt.Alias
	if px.debug {
		// Only after rerouting: in debug mode the log is the only record of which
		// model actually served the request.
		logResolution(a.Model, tgt, nil, a.Stream)
	}

	if px.onReq != nil {
		px.onReq(tgt.Provider.Name, r.Header)
	}
	reqID := newRequestID()
	var d *docs
	if px.debug || (px.log != nil && px.log.StoresDocuments()) {
		d = newDocs()
		d.put("in", body)
	}
	if px.log != nil && px.log.Enabled() {
		defer px.writeLog(reqID, d)
	}
	if px.debug {
		defer dumpDocs(reqID, d)
	}

	var out *Outcome
	dispatch := func(t *Target) (*Outcome, error) {
		if t.Provider.Format == "anthropic" {
			return px.passthrough(w, r, t, body, d)
		}
		return px.translate(w, r, t, &a, d)
	}
	out, err = dispatch(tgt)

	// Only a confirmed limit justifies switching models: a 429 without these
	// headers may be a short rate limit, and while Anthropic offers overage the
	// request is not blocked at all.
	// If the fallback also reports a limit, we continue down the chain; the config rules out cycles.
	for out != nil && out.Pending != nil && tgt.Provider.Fallback != "" {
		until, confirmed := confirmedLimit(out.Pending.Headers, time.Now())
		if !confirmed || tgt.Provider.Format != "anthropic" {
			break
		}
		px.cool.Block(tgt.Provider.Name, until)
		log.Printf("limit: %s exhausted until %s, further requests go to %s",
			tgt.Provider.Name, until.Format(time.RFC3339), tgt.Provider.Fallback)
		fb := px.applyCooldown(tgt)
		if fb.Provider.Name == tgt.Provider.Name {
			break
		}
		fb.Alias = wantAlias
		fbOut, fbErr := dispatch(fb)
		if fbErr != nil {
			// The 429 with its reset is more useful than a 502 that hides the limit.
			log.Printf("fallback %s unreachable: %v, passing through the error from %s",
				fb.Provider.Name, fbErr, tgt.Provider.Name)
			break
		}
		tgt, out, err = fb, fbOut, fbErr
	}
	if out != nil && out.Pending != nil {
		writePending(w, out.Pending)
		out.Pending = nil
	}
	if err != nil {
		writeError(w, 502, "api_error", "provider unreachable: "+err.Error())
		out = &Outcome{Status: 502, ErrorCode: "502"}
	}
	px.record(started, tgt, out, r, reqID)
}

// applyCooldown follows the fallback chain to the first unblocked provider, otherwise to the last.
func (px *Proxy) applyCooldown(tgt *Target) *Target {
	seen := map[string]bool{}
	for tgt.Provider.Fallback != "" && !seen[tgt.Provider.Name] &&
		px.cool.Blocked(tgt.Provider.Name, time.Now()) {
		seen[tgt.Provider.Name] = true
		fb, rerr := fallbackTarget(px.cfg(), tgt)
		if rerr != nil {
			log.Printf("fallback %s: %s, request stays with %s",
				tgt.Provider.Fallback, rerr.Message, tgt.Provider.Name)
			return tgt
		}
		fb.Alias = tgt.Alias
		tgt = fb
	}
	return tgt
}

// If fallback names only the provider, the model stays the same (account switch).
func fallbackTarget(cfg *config.Config, tgt *Target) (*Target, *ResolveError) {
	if strings.Contains(tgt.Provider.Fallback, ":") {
		return Resolve(cfg, tgt.Provider.Fallback)
	}
	p, ok := cfg.Providers[tgt.Provider.Fallback]
	if !ok {
		return nil, &ResolveError{404, "not_found_error",
			fmt.Sprintf("unknown provider %q", tgt.Provider.Fallback)}
	}
	return &Target{Provider: p, UpstreamModel: tgt.UpstreamModel}, nil
}

func (px *Proxy) record(started time.Time, tgt *Target, out *Outcome, r *http.Request, reqID string) {
	if out == nil {
		return
	}
	status := "ok"
	switch {
	case out.Partial:
		status = "partial"
	case out.Status >= 400:
		status = "error"
	}
	// If a provider reports no tokens at all, usage is unknown, and NULL is more honest than 0.00.
	var cost *float64
	if out.InputTokens > 0 || out.OutputTokens > 0 || out.CacheRead > 0 || out.CacheWrite > 0 {
		cost = pricing.Cost(pricing.Lookup(tgt.UpstreamModel),
			out.InputTokens, out.OutputTokens, out.CacheRead, out.CacheWrite)
	}
	px.rec.Record(store.Record{
		TS: started.UTC(), Alias: tgt.Alias, Provider: tgt.Provider.Name,
		UpstreamModel: tgt.UpstreamModel, AuthMode: tgt.Provider.Auth,
		InputTokens: out.InputTokens, OutputTokens: out.OutputTokens,
		CacheRead: out.CacheRead, CacheWrite: out.CacheWrite, CostUSD: cost,
		DurationMS: time.Since(started).Milliseconds(),
		Status:     status, ErrorCode: out.ErrorCode, Client: r.Header.Get("user-agent"),
		RequestID: reqID,
	})
}

func writeError(w http.ResponseWriter, status int, kind, msg string) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	w.Write(convert.ErrorBody(kind, msg))
}

type pendingError struct {
	Status  int
	Headers http.Header
	Body    []byte
}

// writePending writes an error response held back until the fallback decision.
// Only the limit headers are copied from the provider: its content-length no
// longer fits once the body was truncated for the log, and would make the client hit an unexpected EOF.
func writePending(w http.ResponseWriter, p *pendingError) {
	for k, vs := range p.Headers {
		lk := strings.ToLower(k)
		if !strings.HasPrefix(lk, limitHeaderPrefix) && lk != "retry-after" {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(p.Status)
	w.Write(p.Body)
}

// Shows which model name the client actually sent and where it lands, which
// cannot be answered without capturing it.
func logResolution(requested string, tgt *Target, rerr *ResolveError, stream bool) {
	mode := ""
	if stream {
		mode = " stream"
	}
	if rerr != nil {
		log.Printf("  model %q -> rejected: %s%s", requested, rerr.Message, mode)
		return
	}
	log.Printf("  model %q -> %s:%s (format=%s auth=%s)%s",
		requested, tgt.Provider.Name, tgt.UpstreamModel, tgt.Provider.Format, tgt.Provider.Auth, mode)
}

const debugDumpLimit = 4 << 10

// Prints a request's documents in the order they are created: incoming,
// translated, provider response, translated back.
func dumpDocs(reqID string, d *docs) {
	if d == nil {
		return
	}
	labels := []struct{ key, title string }{
		{"in", "incoming (Anthropic)"},
		{"translated", "translated (OpenAI)"},
		{"upstream", "provider response"},
		{"headers", "limit headers"},
		{"out", "translated back (Anthropic)"},
	}
	for _, l := range labels {
		b, ok := d.m[l.key]
		if !ok || len(b) == 0 {
			continue
		}
		fmt.Fprintf(os.Stderr, "  ── %s · %s (%d Bytes)\n%s\n", reqID[:8], l.title, len(b), indent(cut(b)))
	}
}

func cut(b []byte) string {
	if len(b) <= debugDumpLimit {
		return string(b)
	}
	return string(b[:debugDumpLimit]) + fmt.Sprintf("\n… %d more bytes", len(b)-debugDumpLimit)
}

func indent(s string) string {
	return "     " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n     ")
}
