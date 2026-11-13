package main

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// No colors without a terminal, keeping pipes and log files readable.
var colored = isatty.IsTerminal(os.Stdout.Fd())

var (
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	styleFail    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	styleSkip    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleName    = lipgloss.NewStyle().Bold(true)
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleHeading = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

func paint(s lipgloss.Style, text string) string {
	if !colored {
		return text
	}
	return s.Render(text)
}
