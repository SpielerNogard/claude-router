package tui

import (
	"strings"
	"sync"
)

// LogBuffer captures log output while the TUI owns the terminal.
type LogBuffer struct {
	mu    sync.Mutex
	max   int
	lines []string
}

func NewLogBuffer(max int) *LogBuffer { return &LogBuffer{max: max} }

func (b *LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		b.lines = append(b.lines, l)
	}
	if over := len(b.lines) - b.max; over > 0 {
		b.lines = append([]string(nil), b.lines[over:]...)
	}
	return len(p), nil
}

func (b *LogBuffer) Last() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lines) == 0 {
		return ""
	}
	return b.lines[len(b.lines)-1]
}
