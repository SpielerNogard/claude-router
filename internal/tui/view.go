package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/SpielerNogard/claude-router/internal/store"
)

var (
	styleBold   = lipgloss.NewStyle().Bold(true)
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleActive = lipgloss.NewStyle().Bold(true).Reverse(true)
	styleLevel  = map[string]lipgloss.Style{
		"ok":   lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"warn": lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		"bad":  lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	}
	styleBad = styleLevel["bad"]
)

func (m model) View() string {
	lines := []string{m.header(), ""}
	if m.tab == tabOverview {
		lines = append(lines, m.overview()...)
	} else {
		lines = append(lines, m.logLines()...)
	}
	// bubbletea drops the top lines of a frame taller than the terminal, which would hide the tabs.
	if h := m.bodyHeight(); h >= 0 && len(lines)-2 > h {
		lines = lines[:2+h]
	}
	lines = append(lines, "", m.statusLine())
	if m.width > 0 {
		clip := lipgloss.NewStyle().MaxWidth(m.width)
		for i, l := range lines {
			lines[i] = clip.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) header() string {
	tabs := []string{" Overview ", " Logs "}
	tabs[m.tab] = styleActive.Render(tabs[m.tab])
	return fmt.Sprintf("%s  %s   %s %s   Range: %s",
		styleBold.Render("claude-router"), m.listen, tabs[0], tabs[1], ranges[m.rng].key)
}

// statusLine is the latest message above the always-visible key help.
func (m model) statusLine() string {
	info := styleDim.Render(m.logs.Last())
	switch {
	case m.confirm:
		info = styleBold.Render("Stop router? (y/n)")
	case m.err != nil:
		info = styleBad.Render("error: " + m.err.Error())
	}
	return info + "\n" + m.help.View(m.keys)
}

// bodyHeight is the room between header and status lines, -1 before the first WindowSizeMsg.
func (m model) bodyHeight() int {
	if m.height == 0 {
		return -1
	}
	return max(m.height-3-lipgloss.Height(m.statusLine()), 1)
}

func (m model) overview() []string {
	if m.width >= minCardWidth {
		if c := m.cards(); m.height == 0 || len(c) <= m.bodyHeight() {
			return c
		}
	}
	var t store.Totals
	if m.stats != nil {
		t = m.stats.Totals
	}
	kpi := fmt.Sprintf("Requests %d   Tokens in %s  out %s   Cache r %s / w %s   Hit %s   Cost %s",
		t.Requests, compact(t.InputTokens), compact(t.OutputTokens),
		compact(t.CacheRead), compact(t.CacheWrite), hitRate(t), usd(t.CostUSD))

	sorted := m.providerNames()

	now := m.now()
	var rows [][]string
	for _, n := range sorted {
		var pt store.Totals
		if m.stats != nil {
			pt = m.stats.ByProvider[n]
		}
		v := m.limits[n]
		window := func(name string) string {
			if w, ok := v.Windows[name]; ok && level(w, now) != "stale" {
				return fmt.Sprintf("%.0f%%", w.Utilization*100)
			}
			return dash
		}
		rows = append(rows, []string{n, providerStatus(v, m.blocked[n], now), window("5h"), window("7d"),
			fmt.Sprint(pt.Requests), compact(pt.InputTokens), compact(pt.OutputTokens), hitRate(pt), usd(pt.CostUSD)})
	}
	header := []string{"Provider", "Status", "5h", "7d", "Req", "In", "Out", "Hit", "Cost"}
	statusStyle := func(row []string, col int) *lipgloss.Style {
		if col != 1 {
			return nil
		}
		s, ok := styleLevel[row[1]]
		if !ok {
			s = styleBad
		}
		return &s
	}
	return append([]string{kpi, ""}, table(header, rows, m.width, statusStyle)...)
}

// providerNames lists configured providers plus any that only appear in the stats, sorted.
func (m model) providerNames() []string {
	names := map[string]bool{}
	for n := range m.limits {
		names[n] = true
	}
	if m.stats != nil {
		for n := range m.stats.ByProvider {
			names[n] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	return sorted
}

func (m model) logLines() []string {
	visible := 20
	if h := m.bodyHeight(); h >= 0 {
		chrome := 1 // table header
		if m.width >= minCardWidth {
			chrome = 3 // plus box border
		}
		visible = max(h-chrome, 1)
	}
	start := min(m.scroll, len(m.records))
	end := min(start+visible, len(m.records))
	var rows [][]string
	for _, r := range m.records[start:end] {
		t := store.Totals{InputTokens: r.InputTokens, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite}
		status := r.Status
		if r.ErrorCode != "" {
			status += " " + r.ErrorCode
		}
		client, _, _ := strings.Cut(r.Client, " ")
		rows = append(rows, []string{r.TS.Local().Format("15:04:05"), client, r.Provider, r.UpstreamModel,
			compact(r.InputTokens), compact(r.OutputTokens),
			compact(r.CacheRead) + "/" + compact(r.CacheWrite), hitRate(t), usd(r.CostUSD),
			fmt.Sprintf("%.1fs", float64(r.DurationMS)/1000), status})
	}
	header := []string{"Time", "Client", "Provider", "Model", "In", "Out", "Cache r/w", "Hit", "Cost", "Duration", "Status"}
	errStyle := func(row []string, col int) *lipgloss.Style {
		if col == len(row)-1 && row[col] != "ok" {
			return &styleBad
		}
		return nil
	}
	if len(rows) == 0 {
		return []string{styleDim.Render("no requests yet")}
	}
	if m.width < minCardWidth {
		return table(header, rows, m.width, errStyle)
	}
	box := styleCard.Width(m.width - 2).Render(strings.Join(table(header, rows, m.width-4, errStyle), "\n"))
	return strings.Split(box, "\n")
}

// table pads columns and drops them from the right until a row fits width (0 = unlimited), so narrow terminals never wrap.
func table(header []string, rows [][]string, width int, style func(row []string, col int) *lipgloss.Style) []string {
	widths := make([]int, len(header))
	for c, h := range header {
		widths[c] = lipgloss.Width(h)
		for _, r := range rows {
			widths[c] = max(widths[c], lipgloss.Width(r[c]))
		}
	}
	cols := len(header)
	total := func(n int) int {
		sum := 0
		for _, w := range widths[:n] {
			sum += w + 2
		}
		return sum
	}
	for width > 0 && cols > 1 && total(cols) > width {
		cols--
	}
	render := func(cells []string, bold bool) string {
		var b strings.Builder
		for c := 0; c < cols; c++ {
			cell := cells[c] + strings.Repeat(" ", widths[c]-lipgloss.Width(cells[c])+2)
			switch {
			case bold:
				cell = styleBold.Render(cell)
			case style != nil:
				if s := style(cells, c); s != nil {
					cell = s.Render(cell)
				}
			}
			b.WriteString(cell)
		}
		return strings.TrimRight(b.String(), " ")
	}
	out := []string{render(header, true)}
	for _, r := range rows {
		out = append(out, render(r, false))
	}
	return out
}
