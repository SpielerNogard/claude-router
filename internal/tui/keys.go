package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Tab, Jump, Range, Scroll, Help, Quit, ForceQuit key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch tab")),
		Jump:      key.NewBinding(key.WithKeys("1", "2"), key.WithHelp("1/2", "overview/logs")),
		Range:     key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "range")),
		Scroll:    key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "scroll"), key.WithDisabled()),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
		Quit:      key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		ForceQuit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit now")),
	}
}

// forTab enables only the keys that do something on the given tab, so the help line never lists dead keys.
func (k keyMap) forTab(tab int) keyMap {
	k.Range.SetEnabled(tab == tabOverview)
	k.Scroll.SetEnabled(tab == tabLogs)
	return k
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Range, k.Scroll, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Tab, k.Jump, k.Range, k.Scroll}, {k.Help, k.Quit, k.ForceQuit}}
}
