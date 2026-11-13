package tui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Swapped in tests, which have no terminal.
var programOpts = func() []tea.ProgramOption { return []tea.ProgramOption{tea.WithAltScreen()} }

// Run shows the TUI until the user quits or ctx is cancelled; the terminal is restored when it returns.
func Run(ctx context.Context, src Source, logs *LogBuffer, listen string) error {
	// The router owns SIGINT/SIGTERM via ctx; bubbletea's own handler can block forever sending QuitMsg after a ctx shutdown.
	opts := append(programOpts(), tea.WithContext(ctx), tea.WithoutSignalHandler())
	_, err := tea.NewProgram(newModel(src, logs, listen, time.Now), opts...).Run()
	if errors.Is(err, tea.ErrProgramPanic) {
		return err
	}
	if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}
