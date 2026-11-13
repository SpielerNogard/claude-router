package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/SpielerNogard/claude-router/internal/proxy"
	"github.com/SpielerNogard/claude-router/internal/store"
)

const maxRecords = 500

// Source is what the TUI reads; the router passes its store and proxy.
type Source interface {
	Stats(since time.Time, step time.Duration, now time.Time) (*store.Stats, error)
	Recent(f store.RequestFilter) ([]store.Record, int, error)
	Limits() map[string]proxy.ProviderLimitView
	Blocked() map[string]string
}

// Same ranges and steps as web/src/lib/live.ts.
var ranges = []struct {
	key  string
	span time.Duration
	step time.Duration
}{
	{"1h", time.Hour, time.Minute},
	{"3h", 3 * time.Hour, 5 * time.Minute},
	{"6h", 6 * time.Hour, 10 * time.Minute},
	{"24h", 24 * time.Hour, 30 * time.Minute},
	{"7d", 7 * 24 * time.Hour, 3 * time.Hour},
}

const (
	tabOverview = iota
	tabLogs
)

type snapshotMsg struct {
	rng     int
	stats   *store.Stats
	limits  map[string]proxy.ProviderLimitView
	blocked map[string]string
	records []store.Record
	err     error
}

type tickMsg struct{}

type model struct {
	src    Source
	logs   *LogBuffer
	listen string
	now    func() time.Time

	tab, rng      int
	width, height int
	scroll        int
	confirm       bool
	keys          keyMap
	help          help.Model

	stats   *store.Stats
	limits  map[string]proxy.ProviderLimitView
	blocked map[string]string
	records []store.Record
	lastID  int64
	err     error
}

func newModel(src Source, logs *LogBuffer, listen string, now func() time.Time) model {
	return model{src: src, logs: logs, listen: listen, now: now, keys: newKeyMap(), help: help.New()}
}

func (m model) Init() tea.Cmd { return m.fetch() }

// fetch runs as a tea.Cmd, off the UI goroutine; the next tick is scheduled only after it returns, so slow queries never pile up.
func (m model) fetch() tea.Cmd {
	src, rng, after, now := m.src, m.rng, m.lastID, m.now()
	return func() tea.Msg {
		r := ranges[rng]
		st, err := src.Stats(now.Add(-r.span), r.step, now)
		if err != nil {
			return snapshotMsg{rng: rng, err: err}
		}
		recs, _, err := src.Recent(store.RequestFilter{After: after, Limit: maxRecords})
		if err != nil {
			return snapshotMsg{rng: rng, err: err}
		}
		return snapshotMsg{rng: rng, stats: st, limits: src.Limits(), blocked: src.Blocked(), records: recs}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
	case tickMsg:
		return m, m.fetch()
	case snapshotMsg:
		m.apply(msg)
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *model) apply(msg snapshotMsg) {
	m.err = msg.err
	if msg.err != nil {
		return
	}
	// A snapshot fetched before a range switch would show old totals under the new label.
	if msg.rng == m.rng {
		m.stats = msg.stats
	}
	m.limits, m.blocked = msg.limits, msg.blocked
	var fresh []store.Record
	for _, r := range msg.records {
		if r.ID > m.lastID {
			fresh = append(fresh, r)
		}
	}
	if len(fresh) == 0 {
		return
	}
	m.lastID = fresh[0].ID
	m.records = append(fresh, m.records...)
	if len(m.records) > maxRecords {
		m.records = m.records[:maxRecords]
	}
	// Keep a scrolled-down view on the same row instead of drifting as new requests arrive on top.
	if m.scroll > 0 {
		m.scroll = min(m.scroll+len(fresh), len(m.records)-1)
	}
}

func (m model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(k, m.keys.ForceQuit) {
		return m, tea.Quit
	}
	if m.confirm {
		m.confirm = false
		if k.String() == "y" {
			return m, tea.Quit
		}
		return m, nil
	}
	switch {
	case key.Matches(k, m.keys.Quit):
		m.confirm = true
	case key.Matches(k, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
	case key.Matches(k, m.keys.Tab):
		m.tab = (m.tab + 1) % 2
	case key.Matches(k, m.keys.Jump):
		m.tab = map[string]int{"1": tabOverview, "2": tabLogs}[k.String()]
	case key.Matches(k, m.keys.Range):
		if k.String() == "left" {
			m.rng = max(m.rng-1, 0)
		} else {
			m.rng = min(m.rng+1, len(ranges)-1)
		}
	case key.Matches(k, m.keys.Scroll):
		if k.String() == "up" {
			m.scroll = max(m.scroll-1, 0)
		} else {
			m.scroll = min(m.scroll+1, max(len(m.records)-1, 0))
		}
	}
	m.keys = m.keys.forTab(m.tab)
	return m, nil
}
