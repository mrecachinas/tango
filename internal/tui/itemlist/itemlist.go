// Package itemlist provides a body-only list. The root should call Handles
// before Update: a nil command can still mean that a navigation key was consumed.
package itemlist

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
)

type Item struct {
	Title, Detail string
}

type SelectMsg struct{ Index int }
type BackMsg struct{}

type KeyMap struct {
	Up, Down, First, Last, HalfUp, HalfDown, PageUp, PageDown, Select, Back key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("k", "up")),
		Down:     key.NewBinding(key.WithKeys("j", "down")),
		First:    key.NewBinding(key.WithKeys("g", "home")),
		Last:     key.NewBinding(key.WithKeys("G", "end")),
		HalfUp:   key.NewBinding(key.WithKeys("ctrl+u")),
		HalfDown: key.NewBinding(key.WithKeys("ctrl+d")),
		PageUp:   key.NewBinding(key.WithKeys("pgup")),
		PageDown: key.NewBinding(key.WithKeys("pgdown")),
		Select:   key.NewBinding(key.WithKeys("enter")),
		Back:     key.NewBinding(key.WithKeys("esc")),
	}
}

type Model struct {
	Keys                          KeyMap
	items                         []Item
	cursor, offset, width, height int
	emptyText                     string
}

func New() *Model { return &Model{Keys: DefaultKeyMap(), emptyText: "No items"} }

func (m *Model) SetItems(items []Item) {
	m.items = append([]Item(nil), items...)
	m.SetCursor(m.cursor)
}

func (m *Model) SetSize(w, h int) {
	m.width, m.height = max(0, w), max(0, h)
	m.ensureVisible()
}

func (m *Model) SetEmptyText(text string) { m.emptyText = text }
func (m *Model) Cursor() int              { return m.cursor }

func (m *Model) SetCursor(index int) {
	m.cursor = min(max(0, index), max(0, len(m.items)-1))
	m.ensureVisible()
}

func (m *Model) Handles(msg tea.KeyPressMsg) bool {
	k := m.Keys
	return key.Matches(msg, k.Up, k.Down, k.First, k.Last, k.HalfUp, k.HalfDown,
		k.PageUp, k.PageDown, k.Select, k.Back)
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch keys := m.Keys; {
	case key.Matches(k, keys.Up):
		m.SetCursor(m.cursor - 1)
	case key.Matches(k, keys.Down):
		m.SetCursor(m.cursor + 1)
	case key.Matches(k, keys.First):
		m.SetCursor(0)
	case key.Matches(k, keys.Last):
		m.SetCursor(len(m.items) - 1)
	case key.Matches(k, keys.HalfUp):
		m.SetCursor(m.cursor - max(1, m.height/2))
	case key.Matches(k, keys.HalfDown):
		m.SetCursor(m.cursor + max(1, m.height/2))
	case key.Matches(k, keys.PageUp):
		m.SetCursor(m.cursor - max(1, m.height))
	case key.Matches(k, keys.PageDown):
		m.SetCursor(m.cursor + max(1, m.height))
	case key.Matches(k, keys.Select):
		if len(m.items) > 0 {
			index := m.cursor
			return func() tea.Msg { return SelectMsg{Index: index} }
		}
	case key.Matches(k, keys.Back):
		return func() tea.Msg { return BackMsg{} }
	}
	return nil
}

func (m *Model) ensureVisible() {
	if m.height <= 0 {
		m.offset = m.cursor
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.height {
		m.offset = m.cursor - m.height + 1
	}
	m.offset = min(max(0, m.offset), max(0, len(m.items)-m.height))
}

func (m *Model) View() string {
	if m.height <= 0 {
		return ""
	}
	t := theme.Current()
	lines := make([]string, m.height)
	for y := range lines {
		i := m.offset + y
		line := ""
		if i < len(m.items) {
			selected := i == m.cursor
			text, muted := rowStyle(t.Text, selected), rowStyle(t.Muted, selected)
			item := m.items[i]
			title := cleanLine(item.Title)
			line = text.Render(ansi.Truncate(title, m.width, "…"))
			if detail := cleanLine(item.Detail); detail != "" {
				// Preserve both fields in a narrow list rather than letting a long title hide detail.
				titleWidth := ansi.StringWidth(title)
				if titleWidth+2+ansi.StringWidth(detail) > m.width {
					titleWidth = min(titleWidth, max(1, m.width/2))
				}
				detailWidth := min(ansi.StringWidth(detail), max(0, m.width-titleWidth-2))
				if detailWidth > 0 && titleWidth > 0 {
					line = text.Render(ansi.Truncate(title, titleWidth, "…")) +
						text.Render("  ") + muted.Render(ansi.Truncate(detail, detailWidth, "…"))
				}
			}
			line = fitWidth(line, m.width)
			if selected {
				line = t.CursorRow.Render(line)
			}
		} else if len(m.items) == 0 && y == 0 {
			line = t.Faint.Render(cleanLine(m.emptyText))
		}
		lines[y] = fitWidth(line, m.width)
	}
	return strings.Join(lines, "\n")
}

func rowStyle(style lipgloss.Style, selected bool) lipgloss.Style {
	if selected {
		return style.Inherit(theme.Current().CursorRow)
	}
	return style
}

func cleanLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, ansi.Strip(s))
}

func fitWidth(s string, w int) string {
	s = theme.FitWidth(s, w)
	// Truncation at a double-width rune can leave one unused cell.
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}
