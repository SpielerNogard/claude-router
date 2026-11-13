//go:build !windows

package tui

import (
	"context"
	"io"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The router cancels ctx on SIGTERM; Run must still return when bubbletea sees the same signal.
func TestRunReturnsOnSIGTERM(t *testing.T) {
	orig := programOpts
	programOpts = func() []tea.ProgramOption {
		return append(orig(), tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard))
	}
	defer func() { programOpts = orig }()

	for i := 0; i < 100; i++ {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- Run(ctx, fakeSource{}, NewLogBuffer(5), "x") }()
		time.Sleep(5 * time.Millisecond)
		syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("iteration %d: Run hung after SIGTERM", i)
		}
		stop()
	}
}
