package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/SpielerNogard/claude-router/internal/config"
	"github.com/SpielerNogard/claude-router/internal/convert"
	"github.com/SpielerNogard/claude-router/internal/upstream"
)

const maxRequestBody = 64 << 20

func readLimited(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
}

// Replaces only the model field and leaves everything else untouched; this
// matters for passthrough, where unknown fields must survive.
func replaceModel(body []byte, model string) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	b, err := json.Marshal(model)
	if err != nil {
		return body
	}
	m["model"] = b
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

// Objects with more than usageCarry bytes between the "usage" anchor and the closing
// brace are lost. For Anthropic it is about 150 bytes.
const usageCarry = 8 << 10

// Sniffs token counts from a passed-through response without touching the stream.
// Continuous, because the final counts come at the end of the stream.
type usageSniffer struct {
	out   *Outcome
	carry []byte
}

func (u *usageSniffer) Write(p []byte) (int, error) {
	buf := append(u.carry, p...)
	u.scan(buf)
	if len(buf) > usageCarry {
		buf = buf[len(buf)-usageCarry:]
	}
	u.carry = append(u.carry[:0], buf...)
	return len(p), nil
}

func (u *usageSniffer) scan(b []byte) {
	for i := 0; ; {
		j := bytes.Index(b[i:], []byte(`"usage"`))
		if j < 0 {
			return
		}
		i += j + len(`"usage"`)
		obj := balancedObject(b[i:])
		if obj == nil {
			continue
		}
		var usage convert.AnthropicUsage
		if json.Unmarshal(obj, &usage) == nil {
			u.apply(usage)
		}
	}
}

// Later values win: in Anthropic SSE the final message_delta carries the
// final output_tokens.
func (u *usageSniffer) apply(usage convert.AnthropicUsage) {
	if usage.InputTokens > 0 {
		u.out.InputTokens = usage.InputTokens
	}
	if usage.OutputTokens > 0 {
		u.out.OutputTokens = usage.OutputTokens
	}
	if usage.CacheReadInputTokens > 0 {
		u.out.CacheRead = usage.CacheReadInputTokens
	}
	if usage.CacheCreationInputTokens > 0 {
		u.out.CacheWrite = usage.CacheCreationInputTokens
	}
}

// Returns the JSON object following the colon, including nesting.
func balancedObject(b []byte) []byte {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == ':' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	if i >= len(b) || b[i] != '{' {
		return nil
	}
	depth, inStr, esc := 0, false, false
	for j := i; j < len(b); j++ {
		c := b[j]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return b[i : j+1]
			}
		}
	}
	return nil
}

type sseWriter struct{ w http.ResponseWriter }

func (s sseWriter) WriteEvent(name string, data []byte) error {
	_, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data)
	return err
}

func (s sseWriter) Flush() {
	if fl, ok := s.w.(http.Flusher); ok {
		fl.Flush()
	}
}

func (px *Proxy) translate(w http.ResponseWriter, r *http.Request, tgt *Target,
	a *convert.AnthropicRequest, d *docs) (*Outcome, error) {

	oreq, err := convert.ToOpenAIRequest(a, tgt.UpstreamModel)
	if err != nil {
		writeError(w, 400, "invalid_request_error", err.Error())
		return &Outcome{Status: 400, ErrorCode: "400"}, nil
	}
	payload, err := json.Marshal(oreq)
	if err != nil {
		return nil, err
	}
	d.put("translated", payload)
	url := strings.TrimSuffix(tgt.Provider.BaseURL, "/") + "/v1/chat/completions"
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	if err := upstream.ApplyAuth(req.Header, tgt.Provider, r.Header); err != nil {
		writeError(w, 401, "authentication_error", err.Error())
		return &Outcome{Status: 401, ErrorCode: "401"}, nil
	}

	resp, err := px.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		d.put("upstream", raw)
		d.put("headers", limitHeaders(resp.Header))
		if resp.StatusCode == 429 && tgt.Provider.Fallback != "" {
			return &Outcome{Status: 429, ErrorCode: "429",
				Pending: &pendingError{Status: 429, Headers: resp.Header.Clone(),
					Body: convert.ErrorBody("rate_limit_error", upstreamMessage(raw, providerKey(tgt.Provider)))}}, nil
		}
		writeError(w, resp.StatusCode, convert.StatusToKind(resp.StatusCode), upstreamMessage(raw, providerKey(tgt.Provider)))
		return &Outcome{Status: resp.StatusCode, ErrorCode: fmt.Sprint(resp.StatusCode)}, nil
	}
	if a.Stream {
		return px.streamBack(w, resp, a.Model, providerKey(tgt.Provider), d, cancel)
	}
	return px.jsonBack(w, resp, a.Model, providerKey(tgt.Provider), d)
}

func (px *Proxy) jsonBack(w http.ResponseWriter, resp *http.Response, requestedModel, key string, d *docs) (*Outcome, error) {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRequestBody))
	if err != nil {
		return nil, err
	}
	d.put("upstream", capped(raw))
	var o convert.OpenAIResponse
	if err := json.Unmarshal(raw, &o); err != nil {
		writeError(w, 502, "api_error", "provider response is not valid JSON: "+snippet(raw, key))
		return &Outcome{Status: 502, ErrorCode: "502"}, nil
	}
	a := convert.ToAnthropicResponse(&o, requestedModel)
	out, err := json.Marshal(a)
	if err != nil {
		return &Outcome{Status: 200, Partial: true}, nil
	}
	d.put("out", capped(out))
	w.Header().Set("content-type", "application/json")
	if _, err := w.Write(out); err != nil {
		return &Outcome{Status: 200, Partial: true}, nil
	}
	return &Outcome{Status: 200, InputTokens: a.Usage.InputTokens, OutputTokens: a.Usage.OutputTokens}, nil
}

func (px *Proxy) streamBack(w http.ResponseWriter, resp *http.Response, requestedModel, key string, d *docs, cancel context.CancelFunc) (*Outcome, error) {
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	w.WriteHeader(200)

	var sw convert.StreamWriter = sseWriter{w}
	var outCap, upstreamCap *captureWriter
	if d != nil {
		outCap = &captureWriter{limit: maxLogCapture}
		upstreamCap = &captureWriter{limit: maxLogCapture}
		sw = teeEventWriter{sseWriter{w}, outCap}
	}
	st := convert.NewStreamState(requestedModel, sw)
	src := newIdleReader(resp.Body, px.streamIdle(), cancel)
	defer src.Close()
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)

	out := &Outcome{Status: 200}
	for sc.Scan() {
		if upstreamCap != nil {
			upstreamCap.Write(sc.Bytes())
			upstreamCap.Write([]byte("\n"))
		}
		if err := st.Feed(sc.Bytes()); err != nil {
			out.Partial = true
			break
		}
	}
	if sc.Err() != nil {
		out.Partial = true
	}
	if err := resp.Request.Context().Err(); err != nil {
		out.Partial = true
	}
	if msg := st.ProviderError(); msg != "" {
		redacted := redactSecrets(msg, key)
		sw.WriteEvent("error", convert.ErrorBody("api_error", redacted))
		sw.Flush()
		out.Partial = true
		out.ErrorCode = "provider_error"
		log.Printf("provider reported in stream: %s", redacted)
	}
	st.Finish()
	if d != nil {
		d.put("upstream", upstreamCap.buf.Bytes())
		d.put("out", outCap.buf.Bytes())
	}
	out.InputTokens, out.OutputTokens = st.Usage()
	if n := st.Skipped(); n > 0 {
		log.Printf("dropped %d unreadable fragments from stream", n)
	}
	return out, nil
}

// Mirrors every event written to SSE into a bounded buffer for the message log.
type teeEventWriter struct {
	w   sseWriter
	buf *captureWriter
}

func (t teeEventWriter) WriteEvent(name string, data []byte) error {
	fmt.Fprintf(t.buf, "event: %s\ndata: %s\n\n", name, data)
	return t.w.WriteEvent(name, data)
}

func (t teeEventWriter) Flush() { t.w.Flush() }

// Redacts strings that look like a key. Deliberately narrow: a mangled error
// message does more harm than an unrecognized format.
const minKeyLen = 8

var secretPattern = regexp.MustCompile(
	`(?:\b(?:sk|rk|gsk|xai|pplx|ghp|glpat|shpat)[-_][A-Za-z0-9_-]{16,})` +
		`|(?:\bAIza[A-Za-z0-9_-]{30,})` +
		`|(?:\beyJ[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,})`)

// key is the current provider's key; it is replaced literally because the
// pattern list misses formats without a known prefix.
func redactSecrets(s, key string) string {
	// Replace literally only from a plausible key length: a short value would
	// occur in every message and mangle it.
	if len(key) >= minKeyLen {
		s = strings.ReplaceAll(s, key, "[redacted]")
	}
	return secretPattern.ReplaceAllString(s, "[redacted]")
}

// providerKey returns the key the router injects itself; passthrough has none,
// since the client brings its own credential there.
func providerKey(p config.Provider) string {
	if p.Auth != "key" {
		return ""
	}
	return os.Getenv(p.APIKeyEnv)
}

func upstreamMessage(raw []byte, key string) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
		return redactSecrets(e.Error.Message, key)
	}
	return snippet(raw, key)
}

func snippet(raw []byte, key string) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	if s == "" {
		s = "(empty response)"
	}
	return redactSecrets(s, key)
}
