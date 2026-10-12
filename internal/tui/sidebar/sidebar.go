// Package sidebar implements database/collection navigation. Route focused keys
// through Handles before global shortcuts. While Filtering is true, all key
// presses belong to the sidebar; a nil Update command still consumes the key.
package sidebar

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"tango/internal/theme"
)

type Entry struct{ Name, Info string }
type SelectDBMsg struct{ DB string }
type OpenCollMsg struct{ DB, Coll string }
type BackMsg struct{}

// Row is a visible tree row. Loading rows are not selectable. MatchedIndexes
// are rune indexes into Name, not byte offsets.
type Row struct {
	DB, Coll, Name, Info string
	Expanded, Loading    bool
	MatchedIndexes       []int
}

type KeyMap struct {
	Up, Down, First, Last, HalfUp, HalfDown, PageUp, PageDown key.Binding
	Open, Parent, Back, Filter, Delete                        key.Binding
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
		Open:     key.NewBinding(key.WithKeys("enter", "l", "right")),
		Parent:   key.NewBinding(key.WithKeys("h", "left")),
		Back:     key.NewBinding(key.WithKeys("esc")),
		Filter:   key.NewBinding(key.WithKeys("/")),
		Delete:   key.NewBinding(key.WithKeys("backspace", "ctrl+h")),
	}
}

type Model struct {
	Keys                          KeyMap
	databases                     []Entry
	collections                   map[string][]Entry
	expanded, loading             map[string]bool
	activeDB, activeColl          string
	rows                          []Row
	cursor, offset, width, height int
	filter                        string
	filtering                     bool
}

func New() *Model {
	return &Model{
		Keys: DefaultKeyMap(), collections: make(map[string][]Entry),
		expanded: make(map[string]bool), loading: make(map[string]bool),
	}
}

func (m *Model) SetSize(w, h int) {
	m.width, m.height = max(0, w), max(0, h)
	m.ensureVisible()
}

func (m *Model) SetDatabases(entries []Entry) {
	m.databases = append([]Entry(nil), entries...)
	m.rebuild()
}

func (m *Model) SetCollections(db string, entries []Entry) {
	m.collections[db] = append([]Entry(nil), entries...)
	m.rebuild()
}

func (m *Model) SetLoading(db string, loading bool) {
	m.loading[db] = loading
	m.rebuild()
}

func (m *Model) SetActive(db, coll string) { m.activeDB, m.activeColl = db, coll }
func (m *Model) Expand(db string)          { m.expanded[db] = true; m.rebuild() }
func (m *Model) Collapse(db string)        { m.expanded[db] = false; m.rebuild() }
func (m *Model) Filtering() bool           { return m.filtering }
func (m *Model) FilterText() string        { return m.filter }

func (m *Model) Selected() (db, coll string) {
	if m.cursor >= 0 && m.cursor < len(m.rows) && !m.rows[m.cursor].Loading {
		r := m.rows[m.cursor]
		return r.DB, r.Coll
	}
	return "", ""
}

// Rows returns a snapshot, so tests and callers cannot mutate component state.
func (m *Model) Rows() []Row {
	rows := append([]Row(nil), m.rows...)
	for i := range rows {
		rows[i].MatchedIndexes = append([]int(nil), rows[i].MatchedIndexes...)
	}
	return rows
}

// SelectPath expands the parent when selecting a collection. A path hidden by
// the current filter leaves the selection unchanged.
func (m *Model) SelectPath(db, coll string) {
	if coll != "" {
		m.Expand(db)
	}
	for i, row := range m.rows {
		if row.DB == db && row.Coll == coll && !row.Loading {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
}

func (m *Model) Handles(msg tea.KeyPressMsg) bool {
	if m.filtering {
		return true
	}
	k := m.Keys
	return key.Matches(msg, k.Up, k.Down, k.First, k.Last, k.HalfUp, k.HalfDown,
		k.PageUp, k.PageDown, k.Open, k.Parent, k.Back, k.Filter) ||
		(key.Matches(msg, k.Delete) && m.filter != "") || nonSpace(printable(msg))
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	keys := m.Keys
	switch {
	case key.Matches(k, keys.Back):
		if m.filtering || m.filter != "" {
			m.filter, m.filtering = "", false
			m.rebuild()
			return nil
		}
		return func() tea.Msg { return BackMsg{} }
	case key.Matches(k, keys.Delete):
		if m.filter != "" {
			runes := []rune(m.filter)
			m.filter = string(runes[:len(runes)-1])
			m.rebuild()
		} else if m.filtering {
			m.filtering = false
			m.ensureVisible()
		}
		return nil
	}
	if m.filtering {
		if k.String() == "enter" {
			m.filtering = false
			m.ensureVisible()
		} else if text := printable(k); text != "" {
			m.filter += text
			m.rebuild()
		}
		return nil
	}
	switch {
	case key.Matches(k, keys.Filter):
		m.filtering = true
		m.ensureVisible()
	case key.Matches(k, keys.Up):
		m.move(-1)
	case key.Matches(k, keys.Down):
		m.move(1)
	case key.Matches(k, keys.First):
		m.cursor = 0
	case key.Matches(k, keys.Last):
		m.cursor = max(0, len(m.rows)-1)
		if len(m.rows) > 0 && m.rows[m.cursor].Loading {
			m.move(-1)
		}
	case key.Matches(k, keys.HalfUp):
		m.move(-max(1, m.bodyHeight()/2))
	case key.Matches(k, keys.HalfDown):
		m.move(max(1, m.bodyHeight()/2))
	case key.Matches(k, keys.PageUp):
		m.move(-max(1, m.bodyHeight()))
	case key.Matches(k, keys.PageDown):
		m.move(max(1, m.bodyHeight()))
	case key.Matches(k, keys.Parent):
		db, coll := m.Selected()
		if coll != "" {
			m.SelectPath(db, "")
		} else if db != "" {
			m.Collapse(db)
		}
	case key.Matches(k, keys.Open):
		db, coll := m.Selected()
		if db == "" {
			return nil
		}
		if coll != "" {
			return func() tea.Msg { return OpenCollMsg{DB: db, Coll: coll} }
		}
		m.Expand(db)
		return func() tea.Msg { return SelectDBMsg{DB: db} }
	default:
		if text := printable(k); nonSpace(text) {
			m.filter += text
			m.filtering = true
			m.rebuild()
		}
	}
	m.ensureVisible()
	return nil
}

func (m *Model) rebuild() {
	db, coll := m.Selected()
	rows := make([]Row, 0, len(m.databases))
	for _, entry := range m.databases {
		dbMatch, dbIndexes := matches(m.filter, entry.Name)
		children := make([]Row, 0, len(m.collections[entry.Name]))
		for _, child := range m.collections[entry.Name] {
			if matched, indexes := matches(m.filter, child.Name); matched {
				children = append(children, Row{
					DB: entry.Name, Coll: child.Name, Name: child.Name,
					Info: child.Info, MatchedIndexes: indexes,
				})
			}
		}
		if !dbMatch && len(children) == 0 {
			continue
		}
		expanded := m.expanded[entry.Name] || (m.filter != "" && len(children) > 0)
		rows = append(rows, Row{
			DB: entry.Name, Name: entry.Name, Info: entry.Info,
			Expanded: expanded, MatchedIndexes: dbIndexes,
		})
		if expanded {
			rows = append(rows, children...)
			if m.loading[entry.Name] {
				rows = append(rows, Row{DB: entry.Name, Name: "loading…", Loading: true})
			}
		}
	}
	m.rows = rows
	m.cursor = min(m.cursor, max(0, len(rows)-1))
	parent := -1
	for i, row := range rows {
		if row.DB == db && !row.Loading {
			if row.Coll == "" {
				parent = i
			}
			if row.Coll == coll {
				m.cursor = i
				m.ensureVisible()
				return
			}
		}
	}
	if parent >= 0 {
		m.cursor = parent
	} else if len(rows) > 0 && rows[m.cursor].Loading {
		m.move(-1)
	}
	m.ensureVisible()
}

func matches(filter, name string) (bool, []int) {
	if filter == "" {
		return true, nil
	}
	matches := fuzzy.Find(filter, []string{name})
	if len(matches) == 0 {
		return false, nil
	}
	// sahilm/fuzzy reports byte offsets; rendering iterates runes.
	indexes := make([]int, len(matches[0].MatchedIndexes))
	for i, index := range matches[0].MatchedIndexes {
		indexes[i] = utf8.RuneCountInString(name[:index])
	}
	return true, indexes
}

func (m *Model) move(delta int) {
	direction := 1
	if delta < 0 {
		direction = -1
		delta = -delta
	}
	for ; delta > 0; delta-- {
		next := m.cursor + direction
		for next >= 0 && next < len(m.rows) && m.rows[next].Loading {
			next += direction
		}
		if next < 0 || next >= len(m.rows) {
			break
		}
		m.cursor = next
	}
}

func (m *Model) bodyHeight() int {
	if m.filtering || m.filter != "" {
		return max(0, m.height-1)
	}
	return m.height
}

func (m *Model) ensureVisible() {
	h := m.bodyHeight()
	if h <= 0 {
		m.offset = m.cursor
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = min(max(0, m.offset), max(0, len(m.rows)-h))
}

func (m *Model) View() string {
	if m.height <= 0 {
		return ""
	}
	t := theme.Current()
	lines := make([]string, m.height)
	body := m.bodyHeight()
	for y := range lines {
		line := ""
		if y < body {
			if i := m.offset + y; i < len(m.rows) {
				line = m.renderRow(m.rows[i], i == m.cursor)
			} else if len(m.rows) == 0 && y == 0 {
				text := "No databases"
				if m.filter != "" {
					text = "No matches"
				}
				line = t.Faint.Render(text)
			}
		} else {
			// Keep the insertion cursor visible even after a long filter.
			text := cleanLine(m.filter)
			if w := max(0, m.width-3); ansi.StringWidth(text) > w {
				text = ansi.TruncateLeft(text, ansi.StringWidth(text)-w, "")
				text = ansi.Truncate(text, w, "")
			}
			line = t.PromptPrefix.Render("/ ") + t.Text.Render(text)
			if m.filtering {
				line += t.Accent.Render("▏")
			}
		}
		lines[y] = fitWidth(line, m.width)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderRow(row Row, selected bool) string {
	t := theme.Current()
	text := rowStyle(t.Text, selected)
	muted := rowStyle(t.Muted, selected)
	faint := rowStyle(t.Faint, selected)
	accent := rowStyle(t.Accent, selected)
	nameStyle := text
	if row.DB == m.activeDB && (row.Coll == "" || row.Coll == m.activeColl) {
		nameStyle = rowStyle(t.AccentBold, selected)
	}
	prefix := "    "
	if row.Coll == "" && !row.Loading {
		prefix = "▸ "
		if row.Expanded {
			prefix = "▾ "
		}
	}
	name := cleanLine(row.Name)
	if row.Loading {
		nameStyle = faint
	}
	matched := make(map[int]bool, len(row.MatchedIndexes))
	for _, index := range row.MatchedIndexes {
		matched[index] = true
	}
	var label strings.Builder
	for i := 0; name != ""; {
		// ANSI boundaries inside an emoji/combining cluster change its cell width.
		cluster, _ := ansi.FirstGraphemeCluster(name, ansi.GraphemeWidth)
		runes := utf8.RuneCountInString(cluster)
		style := nameStyle
		for j := i; j < i+runes; j++ {
			if matched[j] {
				style = accent
				if row.DB == m.activeDB {
					style = style.Bold(true)
				}
				break
			}
		}
		label.WriteString(style.Render(cluster))
		i += runes
		name = name[len(cluster):]
	}
	info := cleanLine(row.Info)
	infoWidth := min(ansi.StringWidth(info), m.width)
	leftWidth := m.width
	if infoWidth > 0 {
		leftWidth = max(0, m.width-infoWidth-1)
	}
	left := text.Render(prefix) + label.String()
	left = fitWidth(left, leftWidth)
	line := left
	if infoWidth > 0 {
		gap := m.width - leftWidth - infoWidth
		line += text.Render(strings.Repeat(" ", max(0, gap))) +
			muted.Render(ansi.Truncate(info, infoWidth, "…"))
	}
	line = fitWidth(line, m.width)
	if selected {
		line = t.CursorRow.Render(line)
	}
	return line
}

func rowStyle(style lipgloss.Style, selected bool) lipgloss.Style {
	if selected {
		return style.Inherit(theme.Current().CursorRow)
	}
	return style
}

func printable(msg tea.KeyPressMsg) string {
	if msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper) != 0 {
		return ""
	}
	text := msg.Text
	if text == "" && unicode.IsPrint(msg.Code) {
		text = string(msg.Code)
	}
	for _, r := range text {
		if !unicode.IsPrint(r) {
			return ""
		}
	}
	return text
}

func nonSpace(s string) bool { return strings.TrimSpace(s) != "" }

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
