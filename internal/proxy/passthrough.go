package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/SpielerNogard/claude-router/internal/upstream"
)

type Outcome struct {
	Status       int
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheWrite   int64
	ErrorCode    string
	Partial      bool
	// Pending is an error response not yet written to the client; on 429 the
	// caller may try the fallback target instead.
	Pending *pendingError
}

// Cap on everything captured for the message log, independent of the actual
// response size, so a long stream cannot blow up memory.
const maxLogCapture = 1 << 20

// Captures the head and stops; the log does the final truncation itself.
type captureWriter struct {
	buf   bytes.Buffer
	limit int
}

func (c *captureWriter) Write(p []byte) (int, error) {
	if n := c.limit - c.buf.Len(); n > 0 {
		if len(p) < n {
			n = len(p)
		}
		c.buf.Write(p[:n])
	}
	return len(p), nil
}

// For paths that already hold the full response in memory (no stream):
// caps the logged excerpt at the same limit as captureWriter.
func capped(b []byte) []byte {
	if len(b) > maxLogCapture {
		return b[:maxLogCapture]
	}
	return b
}

// Passthrough rewrites nothing: the body goes through byte for byte,
// in both directions.
func (px *Proxy) passthrough(w http.ResponseWriter, r *http.Request, tgt *Target, body []byte, d *docs) (*Outcome, error) {
	base := tgt.Provider.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	upstreamBody := replaceModel(body, tgt.UpstreamModel)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method,
		strings.TrimSuffix(base, "/")+r.URL.RequestURI(), strings.NewReader(string(upstreamBody)))
	if err != nil {
		return nil, err
	}
	upstream.CopyForwardableHeaders(req.Header, r.Header)
	if err := upstream.ApplyAuth(req.Header, tgt.Provider, r.Header); err != nil {
		writeError(w, 401, "authentication_error", err.Error())
		return &Outcome{Status: 401, ErrorCode: "401"}, nil
	}
	req.Header.Set("content-type", "application/json")

	resp, err := upstream.Send(px.client, req, tgt.Provider)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	px.limits.note(tgt.Provider.Name, resp.Header, time.Now())

	if resp.StatusCode == 429 && tgt.Provider.Fallback != "" {
		// Nothing written to the client yet: the caller decides whether to
		// retry with the fallback provider.
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxRequestBody))
		d.put("out", capped(raw))
		d.put("headers", limitHeaders(resp.Header))
		return &Outcome{Status: 429, ErrorCode: "429",
			Pending: &pendingError{Status: 429, Headers: resp.Header.Clone(), Body: raw}}, nil
	}

	if resp.StatusCode >= 400 {
		d.put("headers", limitHeaders(resp.Header))
	}

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	out := &Outcome{Status: resp.StatusCode}
	if resp.StatusCode >= 400 {
		out.ErrorCode = fmt.Sprint(resp.StatusCode)
	}
	tee := &usageSniffer{out: out}
	mw := io.MultiWriter(flushWriter{w}, tee)
	var capture *captureWriter
	if d != nil {
		capture = &captureWriter{limit: maxLogCapture}
		mw = io.MultiWriter(mw, capture)
	}
	// Only a stream can go silent: a non-streaming response arrives in one
	// piece, so any idle limit would be a false alarm.
	idle := time.Duration(0)
	streaming := strings.HasPrefix(resp.Header.Get("content-type"), "text/event-stream")
	if streaming {
		idle = px.streamIdle()
	}
	src := newIdleReader(resp.Body, idle, cancel)
	defer src.Close()
	if _, err := io.Copy(mw, src); err != nil {
		out.Partial = true
		if src.Stalled() {
			out.ErrorCode = "stalled"
			log.Printf("stream: %s sent nothing for %s, aborted", tgt.Provider.Name, idle)
			// Without this event the client only sees truncated text.
			writeStallEvent(w, idle)
		}
	}
	if capture != nil {
		d.put("out", capture.buf.Bytes())
	}
	return out, nil
}
