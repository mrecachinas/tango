package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/state"
	"tango/internal/theme"
)

func TestStatsViewportStylesAlignmentAndTruncation(t *testing.T) {
	theme.Set(theme.New(theme.Dark()))
	m := newStats()
	m.SetSize(80, 12)
	meta := "Updated 12:34:56 · auto-refresh every 2s · r refresh"
	sections := []state.StatsSection{{
		Title: "Active Operations", Note: "(2)",
		Columns: []state.StatsColumn{
			{Name: "OPID", Width: 8, Right: true}, {Name: "SECS", Width: 8, Right: true}, {Name: "COMMAND"},
		},
		Rows: [][]string{{"1", "2", strings.Repeat("long command ", 20)}, {"234", "123", "short command"}},
	}, {
		Title: "Top Collections", Note: "(0)", Empty: "No collection activity",
	}, {
		Title: "Slow Queries", Note: "(0)", Empty: "No profiler data",
	}}
	m.SetData(meta, sections, "")
	body := m.View()
	for _, styled := range []string{
		theme.Current().Faint.Render(meta), theme.Current().Text.Bold(true).Render("Active operations"),
		theme.Current().Muted.Render("(2)"), theme.Current().Text.Bold(true).Render("Top collections"),
		theme.Current().Text.Bold(true).Render("Slow queries"),
	} {
		if !strings.Contains(body, styled) {
			t.Errorf("missing stats styling %q", ansi.Strip(styled))
		}
	}
	if strings.Contains(body, "48;") || strings.Contains(body, "48:") {
		t.Fatal("non-interactive stats viewport painted a cursor background")
	}
	lines := strings.Split(ansi.Strip(body), "\n")
	if !strings.HasPrefix(lines[4], "       1         2  ") || !strings.HasSuffix(strings.TrimRight(lines[4], " "), "…") {
		t.Fatalf("numeric alignment or command truncation: %q", lines[4])
	}
	for _, size := range [][2]int{{118, 32}, {60, 12}, {38, 6}, {1, 1}, {0, 0}} {
		m.SetSize(size[0], size[1])
		if size[1] == 0 {
			if m.View() != "" {
				t.Fatal("zero-height stats body")
			}
			continue
		}
		lines := strings.Split(m.View(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("stats height %d want %d", len(lines), size[1])
		}
		for _, line := range lines {
			if ansi.StringWidth(line) != size[0] {
				t.Fatalf("stats width %d want %d", ansi.StringWidth(line), size[0])
			}
		}
	}
	m.SetSize(80, 4)
	m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if m.offset != len(m.lines)-4 || !strings.Contains(ansi.Strip(m.View()), "Slow queries") {
		t.Fatal("stats viewport cannot scroll to bottom")
	}
	m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if m.offset != 0 {
		t.Fatal("stats viewport cannot scroll to top")
	}
}
