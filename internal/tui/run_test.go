package tui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRunReturnsOnCancel(t *testing.T) {
	orig := programOpts
	programOpts = func() []tea.ProgramOption {
		return []tea.ProgramOption{tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard)}
	}
	defer func() { programOpts = orig }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, fakeSource{}, NewLogBuffer(5), "x") }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
