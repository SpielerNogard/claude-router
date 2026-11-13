package proxy

import (
	"net/http"

	"context"
	"fmt"
	"github.com/SpielerNogard/claude-router/internal/convert"
	"io"
	"sync/atomic"
	"time"
)

// Anthropic keeps long streams alive with ping events. If it stays silent this
// long, the upstream connection is dead; without this watchdog io.Copy waits
// forever, and the client waits on the router.
// Claude Code itself aborts a silent stream after 3 minutes, behind a gateway
// after 5. The router must fire earlier so the error comes from it and does
// not reach the client as a silent timeout.
const defaultStreamIdle = 120 * time.Second

// idleReader aborts when nothing arrives between two reads for too long.
// Aborting goes through the upstream request's context: only that unblocks a
// Read that is already blocking.
type idleReader struct {
	rc      io.ReadCloser
	timeout time.Duration
	timer   *time.Timer
	stalled atomic.Bool
	stopped atomic.Bool
}

func newIdleReader(rc io.ReadCloser, timeout time.Duration, cancel context.CancelFunc) *idleReader {
	r := &idleReader{rc: rc, timeout: timeout}
	if timeout <= 0 {
		return r
	}
	r.timer = time.AfterFunc(timeout, func() {
		r.stalled.Store(true)
		cancel()
	})
	return r
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if n > 0 && r.timer != nil && !r.stopped.Load() {
		r.timer.Reset(r.timeout)
	}
	if err != nil && r.stalled.Load() {
		return n, &StallError{After: r.timeout}
	}
	return n, err
}

func (r *idleReader) Close() error {
	r.stopped.Store(true)
	if r.timer != nil {
		r.timer.Stop()
	}
	return r.rc.Close()
}

// Stalled reports whether the abort came from the watchdog rather than the client.
func (r *idleReader) Stalled() bool { return r.stalled.Load() }

type StallError struct{ After time.Duration }

func (e *StallError) Error() string {
	return fmt.Sprintf("provider sent nothing for %s, connection aborted", e.After)
}

// streamIdle returns the configured idle limit, or the default.
// An explicit 0 disables the watchdog.
func (px *Proxy) streamIdle() time.Duration {
	if d := px.cfg().StreamIdle; d != nil {
		return *d
	}
	return defaultStreamIdle
}

// writeStallEvent appends an Anthropic error event to an aborted stream;
// otherwise truncated text looks like a normal end to the client.
func writeStallEvent(w http.ResponseWriter, after time.Duration) {
	fmt.Fprintf(w, "event: error\ndata: %s\n\n",
		convert.ErrorBody("api_error", (&StallError{After: after}).Error()))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
