// Package prompt provides the transient query, command, and document-search input.
package prompt

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
)

type Kind string

const (
	Query   Kind = "query"
	Command Kind = "command"
	Search  Kind = "search"
)

type SubmitMsg struct {
	Kind Kind
	Text string
}

type CancelMsg struct{ Kind Kind }

type ChangedMsg struct {
	Kind Kind
	Text string
}

type Model struct {
	kind           Kind
	input          textinput.Model
	width, maxRows int
	active         bool
	showStrip      bool
	complete       func(string) []string
	mapping        func(string) string
	candidates     []string
	selected       int
	cycling        bool
	history        []string
	historyIndex   int
	draft          string
}

// New leaves the prompt inactive. Prefix is presentation only, not part of Text.
// Completion callbacks receive raw Text; adapt command prefixes in the caller.
func New(kind Kind, prefix, placeholder string) *Model {
	m := &Model{
		kind: kind, input: textinput.New(), maxRows: 2, showStrip: true,
		selected: -1, historyIndex: -1,
	}
	m.input.Prompt = singleLine(prefix)
	m.input.Placeholder = singleLine(placeholder)
	m.input.ShowSuggestions = true
	m.input.KeyMap.NextSuggestion.SetEnabled(false)
	m.input.KeyMap.PrevSuggestion.SetEnabled(false)
	m.input.KeyMap.AcceptSuggestion.SetEnabled(false)
	m.style()
	return m
}

func (m *Model) SetSize(w, h int) {
	m.width, m.maxRows = max(0, w), max(0, h)
	m.input.SetWidth(max(1, m.width-ansi.StringWidth(m.input.Prompt)-1))
}

func (m *Model) Focus() tea.Cmd {
	m.active = true
	m.style()
	return m.input.Focus()
}

func (m *Model) Blur() {
	m.active = false
	m.input.Blur()
}

func (m *Model) Active() bool                   { return m.active }
func (m *Model) Kind() Kind                     { return m.kind }
func (m *Model) Text() string                   { return m.input.Value() }
func (m *Model) Handles(_ tea.KeyPressMsg) bool { return m.active }

func (m *Model) SetKind(kind Kind) { m.kind = kind }

func (m *Model) SetPrefix(prefix string) {
	m.input.Prompt = singleLine(prefix)
	m.SetSize(m.width, m.maxRows)
}

func (m *Model) SetPlaceholder(placeholder string) {
	m.input.Placeholder = singleLine(placeholder)
}

// SetCompletion maps entries immediately, before another callback can replace a
// SuggestionText-style lookup table. Nil mapping treats entries as raw text.
func (m *Model) SetCompletion(fn func(string) []string, mapping ...func(string) string) {
	m.complete, m.mapping = fn, nil
	if len(mapping) > 0 {
		m.mapping = mapping[0]
	}
	m.refresh()
}

// SetHistory accepts oldest-to-newest entries; browsing restores the draft.
func (m *Model) SetHistory(history []string) {
	m.history = append([]string(nil), history...)
	m.historyIndex = -1
}

// SetText updates programmatically without emitting ChangedMsg.
func (m *Model) SetText(text string) {
	m.input.SetValue(singleLine(text))
	m.input.CursorEnd()
	m.historyIndex = -1
	m.refresh()
}

func (m *Model) Candidates() []string        { return append([]string(nil), m.candidates...) }
func (m *Model) SetCandidateStrip(show bool) { m.showStrip = show }

// Height is zero when inactive, otherwise one (input) or two (strip above input).
func (m *Model) Height() int {
	if !m.active || m.maxRows == 0 {
		return 0
	}
	if m.showStrip && len(m.candidates) > 0 && m.maxRows > 1 {
		return 2
	}
	return 1
}

func (m *Model) refresh() {
	m.selected, m.cycling = -1, false
	m.candidates = nil
	if m.complete != nil {
		seen := make(map[string]bool)
		for _, candidate := range m.complete(m.Text()) {
			if m.mapping != nil {
				candidate = m.mapping(candidate)
			}
			candidate = singleLine(candidate)
			if candidate != "" && !seen[candidate] {
				m.candidates = append(m.candidates, candidate)
				seen[candidate] = true
			}
		}
	}
	m.ghost()
}

func (m *Model) ghost() {
	if len(m.candidates) == 0 {
		m.input.SetSuggestions(nil)
		return
	}
	m.input.SetSuggestions([]string{m.candidates[max(0, m.selected)]})
}

func (m *Model) cycle(backward bool) {
	if len(m.candidates) == 0 {
		return
	}
	if !m.cycling {
		m.selected = 0
		if backward {
			m.selected = len(m.candidates) - 1
		}
	} else if backward {
		m.selected = (m.selected + len(m.candidates) - 1) % len(m.candidates)
	} else {
		m.selected = (m.selected + 1) % len(m.candidates)
	}
	m.cycling = true
	m.historyIndex = -1
	m.input.SetValue(m.candidates[m.selected])
	m.input.CursorEnd()
	m.ghost()
}

func (m *Model) browse(backward bool) {
	if len(m.history) == 0 {
		return
	}
	if backward {
		if m.historyIndex < 0 {
			m.draft = m.Text()
			m.historyIndex = len(m.history) - 1
		} else {
			m.historyIndex = max(0, m.historyIndex-1)
		}
	} else {
		if m.historyIndex < 0 {
			return
		}
		m.historyIndex++
		if m.historyIndex == len(m.history) {
			m.historyIndex = -1
		}
	}
	value := m.draft
	if m.historyIndex >= 0 {
		value = m.history[m.historyIndex]
	}
	m.input.SetValue(singleLine(value))
	m.input.CursorEnd()
	m.refresh()
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if !m.active {
		return nil
	}
	before := m.Text()
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "enter":
			event := SubmitMsg{Kind: m.kind, Text: m.Text()}
			m.Blur()
			return func() tea.Msg { return event }
		case "esc":
			event := CancelMsg{Kind: m.kind}
			m.Blur()
			return func() tea.Msg { return event }
		case "tab", "shift+tab":
			m.cycle(key.String() == "shift+tab")
			return m.changed(before, nil)
		case "up", "down":
			m.browse(key.String() == "up")
			return m.changed(before, nil)
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.Text() != before {
		m.historyIndex = -1
		m.refresh()
	}
	return m.changed(before, cmd)
}

func (m *Model) changed(before string, cmd tea.Cmd) tea.Cmd {
	if before == m.Text() {
		return cmd
	}
	event := ChangedMsg{Kind: m.kind, Text: m.Text()}
	return tea.Batch(cmd, func() tea.Msg { return event })
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

// View is exactly Height() lines, each width cells. The optional candidate strip
// is above the input, so the editable line stays immediately above the footer.
func (m *Model) View() string {
	h := m.Height()
	if h == 0 {
		return ""
	}
	m.style()
	view := m.input.View()
	if m.Text() == "" && len(m.candidates) > 0 {
		// Bubbles intentionally does not suggest for an empty value. Render a
		// copy as ghost text without committing anything to the editable input.
		ghost := m.input
		ghost.Placeholder = ""
		ghost.SetValue(m.candidates[max(0, m.selected)])
		ghost.SetCursor(0)
		ghost.SetSuggestions(nil)
		styles := ghost.Styles()
		styles.Focused.Text = theme.Current().Suggestion
		styles.Blurred.Text = theme.Current().Suggestion
		ghost.SetStyles(styles)
		view = ghost.View()
	}
	input := fitWidth(view, m.width)
	if h == 1 {
		return input
	}
	first := max(0, m.selected)
	var entries []string
	t := theme.Current()
	for i := first; i < min(first+6, len(m.candidates)); i++ {
		style := t.Suggestion
		if i == max(0, m.selected) {
			style = t.Accent
		}
		entries = append(entries, style.Render(m.candidates[i]))
	}
	strip := fitWidth(strings.Join(entries, t.Faint.Render(" · ")), m.width)
	return strip + "\n" + input
}

func singleLine(text string) string {
	text = ansi.Strip(text)
	return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(text)
}

func fitWidth(text string, w int) string {
	// Truncating a wide grapheme may leave a spare cell; pad that cell as well.
	return theme.FitWidth(theme.FitWidth(text, w), w)
}
