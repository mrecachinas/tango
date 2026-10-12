// Package palette provides the fuzzy command and resource chooser overlay.
package palette

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"github.com/sahilm/fuzzy"

	"tango/internal/theme"
)

type Kind string

const (
	Command    Kind = "command"
	Database   Kind = "database"
	Collection Kind = "collection"
	Action     Kind = "action"
)

type Item struct {
	Kind  Kind
	Title string
	Hint  string
	Value string
}

type ChooseMsg struct {
	Item  *Item
	Input string
}

type CloseMsg struct{}

type result struct {
	index   int
	matched []int
}

type Model struct {
	input         textinput.Model
	items         []Item
	results       []result
	cursor, top   int
	width, height int
	recent        map[string]int
	clock         int
}

func New(items []Item) *Model {
	m := &Model{input: textinput.New(), recent: make(map[string]int)}
	m.input.Prompt = ": "
	m.input.Placeholder = "Command, database, collection…"
	m.style()
	m.input.Focus()
	m.SetSize(80, 24)
	m.SetItems(items)
	return m
}

// SetSize receives available screen dimensions. The complete box is at most
// min(72, w-4) cells wide and min(14, h-2) rows tall.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = min(72, max(0, w-4)), min(14, max(0, h-2))
	m.input.SetWidth(max(1, m.width-7))
	m.reveal()
}

func (m *Model) Dimensions() (int, int)         { return m.width, m.height }
func (m *Model) Focus() tea.Cmd                 { return m.input.Focus() }
func (m *Model) Blur()                          { m.input.Blur() }
func (m *Model) Text() string                   { return m.input.Value() }
func (m *Model) Handles(_ tea.KeyPressMsg) bool { return true }

func (m *Model) SetItems(items []Item) {
	m.items = append([]Item(nil), items...)
	m.filter()
}

func (m *Model) SetText(text string) {
	m.input.SetValue(text)
	m.input.CursorEnd()
	m.filter()
}

func itemID(item Item) string { return string(item.Kind) + "\x00" + item.Title + "\x00" + item.Value }

// Remember makes a successful choice rank first the next time the input is empty.
func (m *Model) Remember(item Item) {
	m.clock++
	m.recent[itemID(item)] = m.clock
	if strings.TrimSpace(m.Text()) == "" {
		m.filter()
	}
}

func (m *Model) FilteredItems() []Item {
	items := make([]Item, len(m.results))
	for i, res := range m.results {
		items[i] = m.items[res.index]
	}
	return items
}

func (m *Model) SelectedItem() *Item {
	if m.cursor < 0 || m.cursor >= len(m.results) {
		return nil
	}
	item := m.items[m.results[m.cursor].index]
	return &item
}

func normalized(text string) string { return strings.TrimPrefix(strings.TrimSpace(text), ":") }

func (m *Model) filter() {
	m.results = nil
	m.cursor, m.top = 0, 0
	text := normalized(m.Text())
	if text == "" {
		for i := range m.items {
			m.results = append(m.results, result{index: i})
		}
		sort.SliceStable(m.results, func(i, j int) bool {
			return m.recent[itemID(m.items[m.results[i].index])] >
				m.recent[itemID(m.items[m.results[j].index])]
		})
		return
	}
	titles := make([]string, len(m.items))
	for i, item := range m.items {
		titles[i] = safe(item.Title)
	}
	for _, match := range fuzzy.Find(text, titles) {
		m.results = append(m.results, result{index: match.Index, matched: match.MatchedIndexes})
	}
	// Human-readable titles may differ from executable values (e.g. "Page size"
	// versus ":limit"). Include value-only matches after title matches.
	seen := make(map[int]bool)
	for _, res := range m.results {
		seen[res.index] = true
	}
	values := make([]string, len(m.items))
	for i, item := range m.items {
		values[i] = normalized(item.Value)
	}
	for _, match := range fuzzy.Find(text, values) {
		if !seen[match.Index] {
			m.results = append(m.results, result{index: match.Index})
		}
	}
	sort.SliceStable(m.results, func(i, j int) bool {
		return exact(m.items[m.results[i].index], text) && !exact(m.items[m.results[j].index], text)
	})
}

func exact(item Item, input string) bool {
	return strings.EqualFold(normalized(item.Title), input) ||
		(item.Value != "" && strings.EqualFold(normalized(item.Value), input))
}

func (m *Model) rawCommand() bool {
	fields := strings.Fields(normalized(m.Text()))
	if len(fields) < 2 {
		return false
	}
	for _, item := range m.items {
		if item.Kind != Command {
			continue
		}
		for _, text := range []string{item.Title, item.Value} {
			parts := strings.Fields(normalized(text))
			if len(parts) > 0 && strings.EqualFold(parts[0], fields[0]) {
				return true
			}
		}
	}
	return false
}

func (m *Model) visibleRows() int { return max(0, m.height-4) }

func (m *Model) reveal() {
	m.cursor = min(max(0, m.cursor), max(0, len(m.results)-1))
	if m.cursor < m.top {
		m.top = m.cursor
	} else if m.cursor >= m.top+m.visibleRows() {
		m.top = max(0, m.cursor-m.visibleRows()+1)
	}
	m.top = min(m.top, max(0, len(m.results)-m.visibleRows()))
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			return func() tea.Msg { return CloseMsg{} }
		case "up", "ctrl+p":
			m.cursor--
			m.reveal()
			return nil
		case "down", "ctrl+n":
			m.cursor++
			m.reveal()
			return nil
		case "enter":
			input := m.Text()
			item := m.SelectedItem()
			if m.rawCommand() && (item == nil || !exact(*item, normalized(input))) {
				item = nil
			}
			if item == nil && strings.TrimSpace(input) == "" {
				return nil
			}
			if item != nil {
				m.Remember(*item)
			}
			event := ChooseMsg{Item: item, Input: input}
			return func() tea.Msg { return event }
		}
	}
	before := m.Text()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.Text() != before {
		m.filter()
	}
	return cmd
}

func (m *Model) style() {
	t := theme.Current()
	state := textinput.StyleState{
		Text: t.Text, Prompt: t.PromptPrefix,
		Placeholder: t.Faint, Suggestion: t.Suggestion,
	}
	m.input.SetStyles(textinput.Styles{
		Focused: state, Blurred: state,
		Cursor: textinput.CursorStyle{Color: t.P.Accent, Shape: tea.CursorBar, Blink: true},
	})
}

func safe(text string) string {
	return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(ansi.Strip(text))
}

func (m *Model) row(res result, selected bool, w int) string {
	item := m.items[res.index]
	t := theme.Current()
	decorate := func(style lipgloss.Style, text string) string {
		if selected {
			style = style.Inherit(t.CursorRow)
		}
		return style.Render(text)
	}
	label := map[Kind]string{Command: "cmd", Database: "db", Collection: "col", Action: "act"}[item.Kind]
	prefix := decorate(t.Faint, theme.FitWidth(label, min(3, w))+" ")
	if w <= 4 {
		prefix = decorate(t.Faint, "")
	}
	remaining := max(0, w-ansi.StringWidth(prefix))
	hint := safe(item.Hint)
	hintW := min(ansi.StringWidth(hint), remaining/3)
	titleW := remaining
	if hintW > 0 {
		titleW -= hintW + 1
	}
	title := safe(item.Title)
	title = ansi.Truncate(title, max(0, titleW), "…")
	matched := make(map[int]bool, len(res.matched))
	for _, i := range res.matched {
		matched[i] = true
	}
	var b strings.Builder
	b.WriteString(prefix)
	graphemes := uniseg.NewGraphemes(title)
	for graphemes.Next() {
		style := t.Text
		start, end := graphemes.Positions()
		for i := start; i < end; i++ {
			if matched[i] {
				style = t.AccentBold
				break
			}
		}
		b.WriteString(decorate(style, graphemes.Str()))
	}
	if hintW > 0 {
		b.WriteString(strings.Repeat(" ", max(1, remaining-ansi.StringWidth(title)-hintW)))
		b.WriteString(decorate(t.Muted, theme.FitWidth(hint, hintW)))
	}
	row := fitWidth(b.String(), w)
	if selected {
		return t.CursorRow.Render(row)
	}
	return row
}

// View renders the complete, already framed overlay; do not wrap it in Pane.
func (m *Model) View() string {
	if m.height == 0 {
		return ""
	}
	if m.width < 4 || m.height < 4 {
		rows := make([]string, m.height)
		for i := range rows {
			rows[i] = theme.FitWidth("", m.width)
		}
		return strings.Join(rows, "\n")
	}
	m.style()
	t := theme.Current()
	inner := m.width - 4
	rows := make([]string, m.height-2)
	rows[0] = fitWidth(m.input.View(), inner)
	for i := 0; i < m.visibleRows(); i++ {
		index := m.top + i
		text := ""
		if index < len(m.results) {
			text = m.row(m.results[index], index == m.cursor, inner)
		} else if i == 0 && len(m.results) == 0 {
			text = t.Muted.Render("No matches · enter runs typed command")
		}
		rows[i+1] = fitWidth(text, inner)
	}
	rows[len(rows)-1] = fitWidth(t.Key.Render("↑↓")+" "+t.KeyDesc.Render("choose")+"  "+
		t.Key.Render("enter")+" "+t.KeyDesc.Render("run")+"  "+t.Key.Render("esc")+" "+t.KeyDesc.Render("close"), inner)
	return t.Overlay.Render(strings.Join(rows, "\n"))
}

func fitWidth(text string, w int) string {
	return theme.FitWidth(theme.FitWidth(text, w), w)
}
