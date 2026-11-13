package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SpielerNogard/claude-router/internal/proxy"
	"github.com/SpielerNogard/claude-router/internal/store"
)

type fakeSource struct{}

func (fakeSource) Stats(time.Time, time.Duration, time.Time) (*store.Stats, error) {
	return &store.Stats{}, nil
}
func (fakeSource) Recent(store.RequestFilter) ([]store.Record, int, error) { return nil, 0, nil }
func (fakeSource) Limits() map[string]proxy.ProviderLimitView              { return nil }
func (fakeSource) Blocked() map[string]string                              { return nil }

var testNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func newTestModel() model {
	return newModel(fakeSource{}, NewLogBuffer(10), "127.0.0.1:4141", func() time.Time { return testNow })
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func send(m model, msgs ...tea.Msg) (model, tea.Cmd) {
	var cmd tea.Cmd
	for _, msg := range msgs {
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(model)
	}
	return m, cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func twoProviders() snapshotMsg {
	return snapshotMsg{
		stats:  &store.Stats{ByProvider: map[string]store.Totals{}},
		limits: map[string]proxy.ProviderLimitView{"subscription": {Kind: "window"}, "deepseek": {Kind: "unlimited"}},
	}
}

func TestTabSwitch(t *testing.T) {
	m, _ := send(newTestModel(), tea.KeyMsg{Type: tea.KeyTab})
	if m.tab != tabLogs {
		t.Fatalf("tab = %d, want logs", m.tab)
	}
	m, _ = send(m, runes("1"))
	if m.tab != tabOverview {
		t.Fatalf("tab = %d, want overview", m.tab)
	}
	m, _ = send(m, runes("2"))
	if m.tab != tabLogs {
		t.Fatalf("tab = %d, want logs", m.tab)
	}
}

func TestRangeCycle(t *testing.T) {
	m, _ := send(newTestModel(), tea.KeyMsg{Type: tea.KeyLeft})
	if ranges[m.rng].key != "1h" {
		t.Errorf("left from 1h = %s, want 1h", ranges[m.rng].key)
	}
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRight})
	if ranges[m.rng].key != "3h" {
		t.Errorf("right from 1h = %s, want 3h", ranges[m.rng].key)
	}
}

func TestQuitConfirm(t *testing.T) {
	m, cmd := send(newTestModel(), runes("q"))
	if isQuit(cmd) {
		t.Fatal("q quit without confirmation")
	}
	if !strings.Contains(m.View(), "Stop router? (y/n)") {
		t.Fatal("prompt not shown")
	}
	m, cmd = send(m, runes("n"))
	if isQuit(cmd) || strings.Contains(m.View(), "Stop router?") {
		t.Fatal("n did not cancel")
	}
	_, cmd = send(m, runes("q"), runes("y"))
	if !isQuit(cmd) {
		t.Fatal("q y did not quit")
	}
}

func TestCtrlCQuits(t *testing.T) {
	if _, cmd := send(newTestModel(), tea.KeyMsg{Type: tea.KeyCtrlC}); !isQuit(cmd) {
		t.Fatal("ctrl+c did not quit")
	}
}

func recs(from, to int64) []store.Record {
	var out []store.Record
	for id := to; id >= from; id-- {
		out = append(out, store.Record{ID: id, Status: "ok"})
	}
	return out
}

func TestRecordsMergeAndCap(t *testing.T) {
	m, _ := send(newTestModel(), snapshotMsg{records: recs(1, 10)}, snapshotMsg{records: recs(8, 12)})
	if len(m.records) != 12 || m.records[0].ID != 12 || m.records[11].ID != 1 {
		t.Fatalf("merge: len %d, first %d", len(m.records), m.records[0].ID)
	}
	m, _ = send(m, snapshotMsg{records: recs(13, 600)})
	if len(m.records) != maxRecords || m.records[0].ID != 600 {
		t.Fatalf("cap: len %d, first %d", len(m.records), m.records[0].ID)
	}
	if m.lastID != 600 {
		t.Errorf("lastID = %d, want 600", m.lastID)
	}
}

func TestEmptyStoreOverview(t *testing.T) {
	m, _ := send(newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 30}, twoProviders())
	v := m.View()
	for _, want := range []string{"subscription", "deepseek", "—"} {
		if !strings.Contains(v, want) {
			t.Errorf("%q missing:\n%s", want, v)
		}
	}
}

func TestPollErrorKeepsData(t *testing.T) {
	good := twoProviders()
	good.stats.Totals.Requests = 4242
	m, _ := send(newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 30}, good, snapshotMsg{err: errors.New("db locked")})
	v := m.View()
	if !strings.Contains(v, "4242") || !strings.Contains(v, "db locked") {
		t.Errorf("view lost data or error:\n%s", v)
	}
}

func TestStatusLineShowsLog(t *testing.T) {
	m := newTestModel()
	m.logs.Write([]byte("message log cleanup: done\n"))
	if !strings.Contains(m.View(), "message log cleanup: done") {
		t.Error("last log line not shown")
	}
}

func TestNarrowWidth(t *testing.T) {
	_ = newTestModel().View()
	msg := twoProviders()
	msg.records = recs(1, 3)
	for _, tab := range []tea.Msg{runes("1"), runes("2")} {
		m, _ := send(newTestModel(), tea.WindowSizeMsg{Width: 40, Height: 10}, msg, tab)
		for _, line := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(line); w > 40 {
				t.Errorf("line width %d > 40: %q", w, line)
			}
		}
	}
}

func TestBlockedProviderShown(t *testing.T) {
	msg := twoProviders()
	msg.blocked = map[string]string{"subscription": "2026-11-13T14:05:00Z"}
	m, _ := send(newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 30}, msg)
	if !strings.Contains(m.View(), "blocked until") {
		t.Errorf("blocked status missing:\n%s", m.View())
	}
}

func TestLogsTabShowsRecords(t *testing.T) {
	msg := twoProviders()
	msg.records = []store.Record{{ID: 1, TS: testNow, Provider: "deepseek", UpstreamModel: "deepseek-chat", Status: "error", ErrorCode: "429"}}
	m, _ := send(newTestModel(), tea.WindowSizeMsg{Width: 160, Height: 30}, msg, runes("2"))
	v := m.View()
	for _, want := range []string{"deepseek-chat", "429"} {
		if !strings.Contains(v, want) {
			t.Errorf("%q missing:\n%s", want, v)
		}
	}
}

func TestStaleWindowShowsDash(t *testing.T) {
	msg := twoProviders()
	msg.limits["subscription"] = proxy.ProviderLimitView{Kind: "window", ProviderLimits: proxy.ProviderLimits{
		Windows: map[string]proxy.LimitWindow{"5h": {Utilization: 1, Reset: testNow.Add(-time.Minute)}}}}
	m, _ := send(newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 30}, msg)
	if strings.Contains(m.View(), "100%") {
		t.Errorf("stale window still shows its old utilization:\n%s", m.View())
	}
}

func TestScrollStaysOnRowWhenNewRecordsArrive(t *testing.T) {
	m, _ := send(newTestModel(), snapshotMsg{records: recs(1, 10)}, runes("2"), tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown})
	top := m.records[m.scroll].ID
	m, _ = send(m, snapshotMsg{records: recs(11, 13)})
	if got := m.records[m.scroll].ID; got != top {
		t.Errorf("top row moved from id %d to %d", top, got)
	}
}

func TestRangeKeysOnlyOnOverview(t *testing.T) {
	m, _ := send(newTestModel(), runes("2"), tea.KeyMsg{Type: tea.KeyRight})
	if ranges[m.rng].key != "1h" {
		t.Errorf("→ on Logs changed range to %s", ranges[m.rng].key)
	}
}

func TestScrollKeysOnlyOnLogs(t *testing.T) {
	m, _ := send(newTestModel(), snapshotMsg{records: recs(1, 10)}, tea.KeyMsg{Type: tea.KeyDown})
	if m.scroll != 0 {
		t.Errorf("↓ on Overview scrolled to %d", m.scroll)
	}
}

func TestHelpLineAlwaysVisible(t *testing.T) {
	m := newTestModel()
	m.logs.Write([]byte("claude-router listening on 127.0.0.1:4141\n"))
	m, _ = send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
	v := m.View()
	if !strings.Contains(v, "listening on") || !strings.Contains(v, "quit") {
		t.Errorf("log line and key help must both show:\n%s", v)
	}
}

func TestHelpToggle(t *testing.T) {
	m, _ := send(newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 30})
	if strings.Contains(m.View(), "ctrl+c") {
		t.Fatal("full help shown before ?")
	}
	m, _ = send(m, runes("?"))
	if !strings.Contains(m.View(), "ctrl+c") {
		t.Fatal("? did not open full help")
	}
	m, _ = send(m, runes("?"))
	if strings.Contains(m.View(), "ctrl+c") {
		t.Fatal("? did not close full help")
	}
}
