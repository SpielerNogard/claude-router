package proxy

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// stallingBody returns a few bytes and then stays silent until closed,
// like a dead upstream connection.
type stallingBody struct {
	chunks [][]byte
	closed chan struct{}
}

func newStallingBody(chunks ...[]byte) *stallingBody {
	return &stallingBody{chunks: chunks, closed: make(chan struct{})}
}

func (s *stallingBody) Read(p []byte) (int, error) {
	if len(s.chunks) > 0 {
		n := copy(p, s.chunks[0])
		s.chunks = s.chunks[1:]
		return n, nil
	}
	<-s.closed
	return 0, io.ErrClosedPipe
}

func (s *stallingBody) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

func TestIdleReaderAbortsOnSilence(t *testing.T) {
	body := newStallingBody([]byte("eins"), []byte("zwei"))
	_, cancel := context.WithCancel(context.Background())

	r := newIdleReader(body, 80*time.Millisecond, func() {
		cancel()
		body.Close() // like the transport closing the connection
	})
	defer r.Close()

	started := time.Now()
	_, err := io.Copy(io.Discard, r)
	elapsed := time.Since(started)

	var stall *StallError
	if !errors.As(err, &stall) {
		t.Fatalf("expected StallError, got %v", err)
	}
	if elapsed > time.Second {
		t.Errorf("abort only came after %v: the watchdog fires too late", elapsed)
	}
	if !r.Stalled() {
		t.Error("Stalled() must report the watchdog abort")
	}
}

func TestIdleReaderLetsSlowButAliveStreamsThrough(t *testing.T) {
	// Three fragments with pauses each below the limit: the stream is slow
	// but alive and must not be aborted.
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < 3; i++ {
			time.Sleep(40 * time.Millisecond)
			pw.Write([]byte("daten"))
		}
		pw.Close()
	}()

	r := newIdleReader(pr, 200*time.Millisecond, func() { t.Error("the watchdog should not have fired") })
	defer r.Close()

	n, err := io.Copy(io.Discard, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 15 {
		t.Errorf("read %d bytes, want 15", n)
	}
}

func TestIdleReaderWithoutTimeoutNeverAborts(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		time.Sleep(120 * time.Millisecond)
		pw.Write([]byte("spaet"))
		pw.Close()
	}()

	r := newIdleReader(pr, 0, func() { t.Error("without a timeout nothing may abort") })
	defer r.Close()

	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIdleReaderCloseStopsTheWatchdog(t *testing.T) {
	body := newStallingBody([]byte("x"))
	r := newIdleReader(body, 50*time.Millisecond, func() { t.Error("the watchdog must not fire after Close") })
	r.Read(make([]byte, 8))
	r.Close()
	time.Sleep(120 * time.Millisecond)
}
