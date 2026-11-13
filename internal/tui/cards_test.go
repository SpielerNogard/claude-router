package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SpielerNogard/claude-router/internal/proxy"
	"github.com/SpielerNogard/claude-router/internal/store"
)

func busySnapshot() snapshotMsg {
	n := 60
	s := store.StatsSeries{InputTokens: make([]int64, n), OutputTokens: make([]int64, n),
		CacheRead: make([]int64, n), CacheWrite: make([]int64, n), CostUSD: make([]float64, n)}
	for i := 0; i < n; i++ {
		s.InputTokens[i], s.OutputTokens[i], s.CacheRead[i], s.CostUSD[i] = int64(i*10), int64(i), int64(i*100), float64(i)
	}
	cost := 303.16
	future := testNow.Add(time.Hour)
	return snapshotMsg{
		stats: &store.Stats{
			Totals:     store.Totals{Requests: 5066, InputTokens: 1572254, OutputTokens: 3379676, CacheRead: 784_000_000, CacheWrite: 16_500_000, CostUSD: &cost},
			ByProvider: map[string]store.Totals{"abo": {Requests: 5066}},
			Series:     s,
		},
		limits: map[string]proxy.ProviderLimitView{
			"abo": {Kind: "window", ProviderLimits: proxy.ProviderLimits{Windows: map[string]proxy.LimitWindow{
				"5h": {Utilization: 0.54, Reset: future}, "7d": {Utilization: 0.59, Reset: future.Add(48 * time.Hour)}}}},
			"deepseek": {Kind: "unlimited"},
			"konto2":   {Kind: "window"},
		},
	}
}

func overviewAt(width int, msg snapshotMsg) string {
	m, _ := send(newTestModel(), tea.WindowSizeMsg{Width: width, Height: 50}, msg)
	return m.View()
}

func TestThousands(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 999: "999", 5066: "5,066", 1572254: "1,572,254"} {
		if got := thousands(in); got != want {
			t.Errorf("thousands(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestResetLabel(t *testing.T) {
	time.Local = time.UTC
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC) // Friday
	if got := resetLabel(time.Date(2026, 11, 13, 11, 50, 0, 0, time.UTC), now); got != "resets 11:50" {
		t.Errorf("same day: %q", got)
	}
	if got := resetLabel(time.Date(2026, 11, 14, 4, 0, 0, 0, time.UTC), now); got != "resets Sat 04:00" {
		t.Errorf("other day: %q", got)
	}
}

func TestCardsShowKPIsAndSparklines(t *testing.T) {
	v := overviewAt(140, busySnapshot())
	for _, want := range []string{"Requests", "5,066", "Tokens in", "1,572,254", "Tokens out", "Cache hit rate", "Cost", "$303.16", "█"} {
		if !strings.Contains(v, want) {
			t.Errorf("%q missing:\n%s", want, v)
		}
	}
}

func TestProviderCardTitles(t *testing.T) {
	msg := busySnapshot()
	msg.blocked = map[string]string{"abo": "2026-11-13T14:05:00Z"}
	v := overviewAt(140, msg)
	for _, want := range []string{"∞ no window limit", "– no data yet", "blocked until", "54%", "resets"} {
		if !strings.Contains(v, want) {
			t.Errorf("%q missing:\n%s", want, v)
		}
	}
}

func TestStaleWindowNoBar(t *testing.T) {
	msg := busySnapshot()
	msg.limits["abo"] = proxy.ProviderLimitView{Kind: "window", ProviderLimits: proxy.ProviderLimits{Windows: map[string]proxy.LimitWindow{
		"5h": {Utilization: 0.54, Reset: testNow.Add(-time.Minute)}}}}
	for _, line := range strings.Split(overviewAt(140, msg), "\n") {
		if strings.Contains(line, "5h") && (strings.Contains(line, "54%") || !strings.Contains(line, "–")) {
			t.Errorf("stale 5h window still drawn: %q", line)
		}
	}
}

func TestCardsFallbackBelow60(t *testing.T) {
	if v := overviewAt(59, busySnapshot()); !strings.Contains(v, "Provider") || strings.Contains(v, "╭") {
		t.Errorf("width 59 should show the table:\n%s", v)
	}
	if v := overviewAt(60, busySnapshot()); !strings.Contains(v, "╭") {
		t.Errorf("width 60 should show cards:\n%s", v)
	}
}

func TestCardsFitWidth(t *testing.T) {
	for _, w := range []int{60, 80, 140} {
		for _, line := range strings.Split(overviewAt(w, busySnapshot()), "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("width %d: line is %d wide: %q", w, lw, line)
			}
		}
	}
}

func TestBarsAligned(t *testing.T) {
	var cols []int
	for _, line := range strings.Split(overviewAt(150, busySnapshot()), "\n") {
		if strings.Contains(line, "resets") {
			cols = append(cols, lipgloss.Width(line[:strings.Index(line, "%")]))
		}
	}
	if len(cols) != 2 || cols[0] != cols[1] {
		t.Errorf("percent columns differ: %v", cols)
	}
}

func linesOf(v string) []string { return strings.Split(v, "\n") }

func TestLogsFitHeight(t *testing.T) {
	msg := busySnapshot()
	msg.records = recs(1, 100)
	for _, w := range []int{50, 120} {
		for _, extra := range [][]tea.Msg{nil, {runes("?")}} {
			m, _ := send(newTestModel(), append([]tea.Msg{tea.WindowSizeMsg{Width: w, Height: 24}, msg, runes("2")}, extra...)...)
			if lines := linesOf(m.View()); len(lines) > 24 || !strings.Contains(lines[0], "Logs") {
				t.Errorf("width %d help %v: %d lines, first %q", w, extra != nil, len(lines), lines[0])
			}
		}
	}
}

func TestOverviewFitsHeight(t *testing.T) {
	m, _ := send(newTestModel(), tea.WindowSizeMsg{Width: 80, Height: 24}, busySnapshot())
	if lines := linesOf(m.View()); len(lines) > 24 || !strings.Contains(lines[0], "Overview") {
		t.Errorf("80x24: %d lines, first %q", len(lines), lines[0])
	}
	m, _ = send(newTestModel(), tea.WindowSizeMsg{Width: 150, Height: 50}, busySnapshot())
	if v := m.View(); !strings.Contains(v, "╭") || len(linesOf(v)) > 50 {
		t.Errorf("150x50 should show cards within height:\n%s", v)
	}
}
