package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/state"
	"tango/internal/theme"
)

// Server stats is a viewport, not a selectable list: :killop takes an explicit
// operation id, so refresh metadata and section headings never become cursors.
type statsModel struct {
	width, height, offset int
	updated, fallback     string
	sections              []state.StatsSection
	lines                 []string
}

func newStats() *statsModel { return &statsModel{} }

func (m *statsModel) SetSize(w, h int) {
	if w != m.width {
		m.width = max(0, w)
		m.rebuild()
	}
	m.height = max(0, h)
	m.clamp()
}

func (m *statsModel) SetData(updated string, sections []state.StatsSection, fallback string) {
	m.updated, m.sections, m.fallback = updated, sections, fallback
	m.rebuild()
}

func (m *statsModel) clamp() {
	m.offset = min(max(0, m.offset), max(0, len(m.lines)-m.height))
}

func (m *statsModel) rebuild() {
	t := theme.Current()
	m.lines = nil
	if len(m.sections) == 0 {
		for _, line := range strings.Split(m.fallback, "\n") {
			m.lines = append(m.lines, t.Faint.Render(lineText(line)))
		}
	} else {
		m.lines = append(m.lines, t.Faint.Render(lineText(m.updated)))
		for _, section := range m.sections {
			title := lineText(section.Title)
			if title != "" {
				title = strings.ToUpper(title[:1]) + strings.ToLower(title[1:])
			}
			heading := t.Text.Bold(true).Render(title) + " " + t.Muted.Render(lineText(section.Note))
			m.lines = append(m.lines, "", heading)
			if section.Empty != "" {
				for _, line := range strings.Split(ansi.Wrap(lineText(section.Empty), max(1, m.width), ""), "\n") {
					m.lines = append(m.lines, t.Faint.Render(line))
				}
				continue
			}
			widths := statsWidths(section.Columns, m.width)
			headers := make([]string, len(section.Columns))
			for i, column := range section.Columns {
				headers[i] = column.Name
			}
			m.lines = append(m.lines, t.Muted.Render(statsRow(headers, section.Columns, widths)))
			for _, row := range section.Rows {
				m.lines = append(m.lines, t.Text.Render(statsRow(row, section.Columns, widths)))
			}
		}
	}
	m.clamp()
}

func statsWidths(columns []state.StatsColumn, width int) []int {
	widths := make([]int, len(columns))
	room := max(0, width-2*max(0, len(columns)-1))
	flex := -1
	for i, column := range columns {
		widths[i] = max(ansi.StringWidth(column.Name), column.Width)
		if column.Width == 0 {
			flex = i
		}
	}
	if flex >= 0 {
		widths[flex] = max(widths[flex], min(24, width/3))
	}
	total := func() int {
		n := 0
		for _, w := range widths {
			n += w
		}
		return n
	}
	for total() > room {
		largest := -1
		for i, w := range widths {
			if w > ansi.StringWidth(columns[i].Name) && (largest < 0 || w > widths[largest]) {
				largest = i
			}
		}
		if largest < 0 {
			break
		}
		widths[largest]--
	}
	if flex >= 0 {
		widths[flex] += max(0, room-total())
	}
	return widths
}

func statsRow(row []string, columns []state.StatsColumn, widths []int) string {
	parts := make([]string, len(columns))
	for i, column := range columns {
		text := ""
		if i < len(row) {
			text = ansi.Truncate(lineText(row[i]), widths[i], "…")
		}
		if column.Right {
			text = strings.Repeat(" ", max(0, widths[i]-ansi.StringWidth(text))) + text
		}
		parts[i] = theme.FitWidth(text, widths[i])
	}
	return strings.Join(parts, "  ")
}

func (m *statsModel) Handles(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "j", "k", "up", "down", "g", "G", "home", "end", "ctrl+d", "ctrl+u", "pgdown", "pgup":
		return true
	}
	return false
}

func (m *statsModel) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "j", "down":
		m.offset++
	case "k", "up":
		m.offset--
	case "g", "home":
		m.offset = 0
	case "G", "end":
		m.offset = len(m.lines)
	case "ctrl+d":
		m.offset += max(1, m.height/2)
	case "ctrl+u":
		m.offset -= max(1, m.height/2)
	case "pgdown":
		m.offset += max(1, m.height)
	case "pgup":
		m.offset -= max(1, m.height)
	}
	m.clamp()
	return nil
}

func (m *statsModel) View() string {
	return fitScreen(strings.Join(m.lines[m.offset:], "\n"), m.width, m.height)
}
