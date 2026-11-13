package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"

	"github.com/SpielerNogard/claude-router/internal/store"
)

const (
	minCardWidth    = 60 // below this the overview falls back to the compact table
	kpiCardMin      = 22
	providerCardMin = 44
)

var (
	styleCard  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8")).Padding(0, 1)
	styleSpark = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	levelColor = map[string]string{"ok": "2", "warn": "3", "bad": "1"}
)

func thousands(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func resetLabel(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return "resets " + t.Format("15:04")
	}
	return "resets " + t.Format("Mon 15:04")
}

func (m model) cards() []string {
	var t store.Totals
	var series store.StatsSeries
	if m.stats != nil {
		t, series = m.stats.Totals, m.stats.Series
	}
	perRow, outer := layout(m.width, kpiCardMin, 5)
	inner := outer - 4
	spark := func(vals []float64) string { return styleSpark.Render(sparkline(vals, inner)) }
	kpis := []string{
		card(outer, styleDim.Render("Requests"), styleBold.Render(thousands(t.Requests)), "", ""),
		card(outer, styleDim.Render("Tokens in"), styleBold.Render(thousands(t.InputTokens)),
			styleDim.Render(fmt.Sprintf("+%s r · +%s w", compact(t.CacheRead), compact(t.CacheWrite))), spark(ints(series.InputTokens))),
		card(outer, styleDim.Render("Tokens out"), styleBold.Render(thousands(t.OutputTokens)), "", spark(ints(series.OutputTokens))),
		card(outer, styleDim.Render("Cache hit rate"), styleBold.Render(hitRate(t)), "",
			spark(hitSeries(series.InputTokens, series.CacheRead, series.CacheWrite, inner))),
		card(outer, styleDim.Render("Cost"), styleBold.Render(usd(t.CostUSD)), "", spark(series.CostUSD)),
	}
	lines := grid(kpis, perRow)

	names := m.providerNames()
	pRow, pOuter := layout(m.width, providerCardMin, len(names))
	var provs []string
	for _, n := range names {
		provs = append(provs, m.providerCard(n, pOuter))
	}
	return append(lines, grid(provs, pRow)...)
}

// layout fits as many cards of at least min columns as the width allows, at most n.
func layout(width, min, n int) (perRow, outer int) {
	perRow = max(1, minInt(n, width/min))
	return perRow, width / perRow
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// card renders lines into a bordered box exactly outer columns wide, clipping instead of wrapping.
func card(outer int, lines ...string) string {
	clip := lipgloss.NewStyle().MaxWidth(outer - 4)
	for i, l := range lines {
		lines[i] = clip.Render(l)
	}
	return styleCard.Width(outer - 2).Render(strings.Join(lines, "\n"))
}

func grid(cards []string, perRow int) []string {
	var out []string
	for i := 0; i < len(cards); i += perRow {
		row := lipgloss.JoinHorizontal(lipgloss.Top, cards[i:minInt(i+perRow, len(cards))]...)
		out = append(out, strings.Split(row, "\n")...)
	}
	return out
}

func (m model) providerCard(name string, outer int) string {
	now := m.now()
	v := m.limits[name]
	title := styleBold.Render(name)
	switch status := providerStatus(v, m.blocked[name], now); {
	case strings.HasPrefix(status, "blocked"):
		title += " " + styleBad.Render(status)
	case v.Kind == "unlimited":
		title += " " + styleDim.Render("∞ no window limit")
	case len(v.Windows) == 0:
		title += " " + styleDim.Render("– no data yet")
	}
	inner := outer - 4
	bar := func(win string) string {
		w, ok := v.Windows[win]
		if v.Kind == "unlimited" {
			return ""
		}
		if !ok || level(w, now) == "stale" {
			return win + "  –"
		}
		tail := fmt.Sprintf(" %3.0f%%  %-16s", w.Utilization*100, resetLabel(w.Reset, now))
		p := progress.New(progress.WithSolidFill(levelColor[level(w, now)]), progress.WithoutPercentage(),
			progress.WithWidth(max(inner-len(win)-1-lipgloss.Width(tail), 5)))
		return win + " " + p.ViewAs(minFloat(w.Utilization, 1)) + tail
	}
	var pt store.Totals
	if m.stats != nil {
		pt = m.stats.ByProvider[name]
	}
	stats := fmt.Sprintf("Req %s  In %s  Out %s  Hit %s  %s",
		thousands(pt.Requests), compact(pt.InputTokens), compact(pt.OutputTokens), hitRate(pt), usd(pt.CostUSD))
	return card(outer, title, bar("5h"), bar("7d"), stats)
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
