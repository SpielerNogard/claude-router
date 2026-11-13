package tui

import (
	"log"
	"sync"
	"testing"
)

func TestLogBufferKeepsLastLines(t *testing.T) {
	b := NewLogBuffer(2)
	b.Write([]byte("a\n"))
	b.Write([]byte("b\nc\n"))
	if got := b.Last(); got != "c" {
		t.Errorf("Last() = %q, want c", got)
	}
	if len(b.lines) != 2 {
		t.Errorf("kept %d lines, want 2", len(b.lines))
	}
}

func TestLogBufferEmpty(t *testing.T) {
	if got := NewLogBuffer(3).Last(); got != "" {
		t.Errorf("Last() = %q, want empty", got)
	}
}

func TestLogBufferConcurrentWrites(t *testing.T) {
	b := NewLogBuffer(10)
	l := log.New(b, "", 0)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); l.Print("x"); _ = b.Last() }()
	}
	wg.Wait()
	if b.Last() != "x" || len(b.lines) != 10 {
		t.Errorf("got %q, %d lines", b.Last(), len(b.lines))
	}
}
