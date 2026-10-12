// Package overlay contains key help, destructive confirmations, and compositing.
package overlay

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
)

type Binding struct{ Keys, Desc string }

type Section struct {
	Title    string
	Bindings []Binding
}

type CloseMsg struct{}
type ConfirmedMsg struct{ ID string }
type CancelledMsg struct{ ID string }

type Help struct {
	sections      []Section
	input         textinput.Model
	width, height int
	lines         []string
	offset        int
	filtering     bool
	cachedTheme   *theme.Theme
}

func NewHelp(sections []Section) *Help {
	m := &Help{input: newInput("/ filter ", "keys or descriptions…")}
	m.input.Focus()
	m.SetSize(100, 24)
	m.SetSections(sections)
	return m
}

// SetSize receives available screen dimensions. Help is a complete box with a
// four-column/two-row margin, capped at 110 cells; Dimensions reports the box.
func (m *Help) SetSize(w, h int) {
	width := min(110, max(0, w-8))
	m.height = max(0, h-4)
	if width != m.width {
		m.width = width
		m.rebuild()
	}
	m.input.SetWidth(max(1, m.width-14))
	m.clamp()
}

func (m *Help) Dimensions() (int, int)         { return m.width, m.height }
func (m *Help) Focus() tea.Cmd                 { return m.input.Focus() }
func (m *Help) Filter() string                 { return m.input.Value() }
func (m *Help) Filtering() bool                { return m.filtering }
func (m *Help) Offset() int                    { return m.offset }
func (m *Help) Handles(_ tea.KeyPressMsg) bool { return true }

func (m *Help) SetSections(sections []Section) {
	m.sections = make([]Section, len(sections))
	for i, section := range sections {
		m.sections[i] = Section{Title: section.Title, Bindings: append([]Binding(nil), section.Bindings...)}
	}
	m.offset = 0
	m.rebuild()
}

func (m *Help) SetFilter(filter string) {
	m.input.SetValue(filter)
	m.input.CursorEnd()
	m.offset = 0
	m.rebuild()
}

func (m *Help) rows() int { return max(0, m.height-4) }
func (m *Help) clamp()    { m.offset = min(max(0, m.offset), max(0, len(m.lines)-m.rows())) }

func (m *Help) filtered() []Section {
	term := strings.ToLower(strings.TrimSpace(m.Filter()))
	var result []Section
	for _, section := range m.sections {
		titleMatch := strings.Contains(strings.ToLower(section.Title), term)
		out := Section{Title: section.Title}
		for _, binding := range section.Bindings {
			if titleMatch || strings.Contains(strings.ToLower(binding.Keys+" "+binding.Desc), term) {
				out.Bindings = append(out.Bindings, binding)
			}
		}
		if len(out.Bindings) > 0 {
			result = append(result, out)
		}
	}
	return result
}

func sectionRows(sections []Section, w int) []string {
	t := theme.Current()
	var rows []string
	keyWidth := 0
	for _, section := range sections {
		for _, binding := range section.Bindings {
			keyWidth = max(keyWidth, ansi.StringWidth(safe(binding.Keys)))
		}
	}
	keyWidth = min(keyWidth, 24, max(1, w-16))
	descWidth := max(1, w-keyWidth-2)
	for s, section := range sections {
		if s > 0 {
			rows = append(rows, theme.FitWidth("", w))
		}
		rows = append(rows, fitWidth(t.AccentBold.Render(safe(section.Title)), w))
		for _, binding := range section.Bindings {
			key := safe(binding.Keys)
			if ansi.StringWidth(key) > keyWidth {
				// Long command syntax gets its own line, never an ellipsis.
				for _, part := range strings.Split(ansi.Wrap(key, max(1, w), ""), "\n") {
					rows = append(rows, fitWidth(t.Key.Render(part), w))
				}
				key = ""
			}
			desc := strings.Split(ansi.Wrap(safe(binding.Desc), descWidth, ""), "\n")
			for i, text := range desc {
				label := ""
				if i == 0 {
					label = key
				}
				rows = append(rows, fitWidth(t.Key.Render(fitWidth(label, keyWidth))+
					"  "+t.KeyDesc.Render(text), w))
			}
		}
	}
	return rows
}

func (m *Help) rebuild() {
	m.cachedTheme = theme.Current()
	inner := max(0, m.width-4)
	sections := m.filtered()
	m.lines = nil
	if len(sections) == 0 {
		m.lines = []string{theme.Current().Muted.Render("No matching keys")}
	} else if inner >= 98 && len(sections) > 1 {
		colWidth := (inner - 3) / 2
		// Keep sections intact and balance by content rather than section count.
		total := 0
		for _, section := range sections {
			total += len(sectionRows([]Section{section}, colWidth)) + 1
		}
		split, used := 1, len(sectionRows(sections[:1], colWidth))+1
		for split < len(sections)-1 && used < total/2 {
			used += len(sectionRows(sections[split:split+1], colWidth)) + 1
			split++
		}
		left := sectionRows(sections[:split], colWidth)
		right := sectionRows(sections[split:], inner-colWidth-3)
		for i := 0; i < max(len(left), len(right)); i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			m.lines = append(m.lines, theme.FitWidth(l, colWidth)+"   "+r)
		}
	} else {
		m.lines = sectionRows(sections, inner)
	}
	m.clamp()
}

// Navigation uses arrows, g/G, j/k (when not filtering), and page/half-page keys.
// Other typing starts filtering; / enters filter mode explicitly, enter leaves
// it, and esc clears a filter before closing the overlay on the next esc.
func (m *Help) Update(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			if m.filtering || m.Filter() != "" {
				m.filtering = false
				m.SetFilter("")
				return nil
			}
			return func() tea.Msg { return CloseMsg{} }
		case "enter":
			m.filtering = false
			return nil
		case "/":
			m.filtering = true
			return m.input.Focus()
		case "?":
			if !m.filtering {
				return func() tea.Msg { return CloseMsg{} }
			}
		case "down", "ctrl+n":
			m.offset++
			m.clamp()
			return nil
		case "up", "ctrl+p":
			m.offset--
			m.clamp()
			return nil
		case "pgdown":
			m.offset += max(1, m.rows())
			m.clamp()
			return nil
		case "pgup":
			m.offset -= max(1, m.rows())
			m.clamp()
			return nil
		case "ctrl+d":
			m.offset += max(1, m.rows()/2)
			m.clamp()
			return nil
		case "ctrl+u":
			m.offset -= max(1, m.rows()/2)
			m.clamp()
			return nil
		case "j", "k", "g", "G", "home", "end":
			if !m.filtering {
				switch key.String() {
				case "j":
					m.offset++
				case "k":
					m.offset--
				case "g", "home":
					m.offset = 0
				case "G", "end":
					m.offset = len(m.lines)
				}
				m.clamp()
				return nil
			}
		}
	}
	before := m.Filter()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.Filter() != before {
		m.filtering = true
		m.offset = 0
		m.rebuild()
	}
	return cmd
}

func (m *Help) View() string {
	if m.height == 0 {
		return ""
	}
	if m.width < 4 || m.height < 4 {
		return fit("", m.width, m.height)
	}
	t := theme.Current()
	if m.cachedTheme != t {
		m.rebuild()
	}
	styleInput(&m.input)
	inner := m.width - 4
	rows := make([]string, m.height-2)
	rows[0] = fitWidth(inputView(&m.input), inner)
	for i := 0; i < m.rows(); i++ {
		text := ""
		if index := m.offset + i; index < len(m.lines) {
			text = m.lines[index]
		}
		rows[i+1] = fitWidth(text, inner)
	}
	rows[len(rows)-1] = fitWidth(t.Key.Render("↑↓")+" "+t.KeyDesc.Render("scroll")+"  "+
		t.Key.Render("/")+" "+t.KeyDesc.Render("filter")+"  "+t.Key.Render("esc")+" "+t.KeyDesc.Render("close"), inner)
	return t.Overlay.Render(strings.Join(rows, "\n"))
}

type Confirm struct {
	id, question  string
	width, height int
	done          bool
}

func NewConfirm(id, question string) *Confirm {
	m := &Confirm{id: id, question: question}
	m.SetSize(80, 24)
	return m
}
func (m *Confirm) SetSize(w, h int) {
	m.width, m.height = min(64, max(0, w-8)), max(0, h-4)
}
func (m *Confirm) Dimensions() (int, int)         { return dialogSize(m.View()) }
func (m *Confirm) Handles(_ tea.KeyPressMsg) bool { return !m.done }

// Only a lowercase y confirms. Any other key cancels; non-key messages do nothing.
func (m *Confirm) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || m.done {
		return nil
	}
	m.done = true
	id := m.id
	if key.String() == "y" {
		return func() tea.Msg { return ConfirmedMsg{ID: id} }
	}
	return func() tea.Msg { return CancelledMsg{ID: id} }
}

func (m *Confirm) View() string {
	t := theme.Current()
	title, action := confirmationTitle(m.id, m.question)
	rows := []string{t.Err.Bold(true).Render(title), ""}
	rows = append(rows, strings.Split(ansi.Wrap(emphasize(m.question, ""), max(1, m.width-4), ""), "\n")...)
	rows = append(rows, "", t.Key.Render("y")+" "+t.KeyDesc.Render(action)+" · "+
		t.Key.Render("n/esc")+" "+t.KeyDesc.Render("cancel"))
	return dialogBox(rows, m.width, m.height)
}

type TypeConfirm struct {
	id, question, expected string
	input                  textinput.Model
	width, height          int
	done, invalid          bool
}

// NewTypeConfirm receives the full question and an exact, case-sensitive target.
// For example: ("drop", "Type orders to drop sample_app.orders", "orders").
func NewTypeConfirm(id, question, expected string) *TypeConfirm {
	m := &TypeConfirm{id: id, question: question, expected: expected, input: newInput("", expected)}
	m.input.Focus()
	m.SetSize(80, 24)
	return m
}

func (m *TypeConfirm) Focus() tea.Cmd                 { return m.input.Focus() }
func (m *TypeConfirm) Text() string                   { return m.input.Value() }
func (m *TypeConfirm) Handles(_ tea.KeyPressMsg) bool { return !m.done }
func (m *TypeConfirm) SetText(text string) {
	m.input.SetValue(text)
	m.input.CursorEnd()
	m.invalid = false
}
func (m *TypeConfirm) SetSize(w, h int) {
	m.width, m.height = min(64, max(0, w-8)), max(0, h-4)
	m.input.SetWidth(max(1, m.width-8))
}
func (m *TypeConfirm) Dimensions() (int, int) { return dialogSize(m.View()) }

func (m *TypeConfirm) Update(msg tea.Msg) tea.Cmd {
	if m.done {
		return nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.done = true
			id := m.id
			return func() tea.Msg { return CancelledMsg{ID: id} }
		case "enter":
			if m.Text() != m.expected {
				m.invalid = true
				return nil
			}
			m.done = true
			id := m.id
			return func() tea.Msg { return ConfirmedMsg{ID: id} }
		}
	}
	before := m.Text()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if before != m.Text() {
		m.invalid = false
	}
	return cmd
}

func (m *TypeConfirm) View() string {
	t := theme.Current()
	styleInput(&m.input)
	title, action := confirmationTitle(m.id, m.question)
	rows := []string{t.Err.Bold(true).Render(title), ""}
	rows = append(rows, strings.Split(ansi.Wrap(emphasize(m.question, m.expected), max(1, m.width-4), ""), "\n")...)
	label := t.Muted.Render("Type ") + t.Text.Bold(true).Render(safe(m.expected)) + t.Muted.Render(" to confirm")
	if m.Text() == m.expected {
		label += " " + t.OK.Render("✓")
	}
	rows = append(rows, "", label)
	inputBorder := t.Pane
	if m.invalid {
		inputBorder = inputBorder.BorderForeground(t.P.Err)
	}
	box := inputBorder.Render(fitWidth(inputView(&m.input), max(0, m.width-6)))
	rows = append(rows, strings.Split(box, "\n")...)
	if m.invalid {
		rows = append(rows, t.Err.Render("Must match exactly"))
	} else {
		rows = append(rows, "")
	}
	rows = append(rows, t.Key.Render("enter")+" "+t.KeyDesc.Render(action)+" · "+
		t.Key.Render("esc")+" "+t.KeyDesc.Render("cancel"))
	return dialogBox(rows, m.width, m.height)
}

func confirmationTitle(id, question string) (title, action string) {
	text := strings.ToLower(id + " " + question)
	switch {
	case strings.Contains(text, "drop"):
		switch {
		case strings.Contains(text, "database"):
			return "Drop database", "drop"
		case strings.Contains(text, "index"):
			return "Drop index", "drop"
		default:
			return "Drop collection", "drop"
		}
	case strings.Contains(text, "delete"):
		if strings.Contains(text, "documents") {
			return "Delete documents", "delete"
		}
		return "Delete document", "delete"
	default:
		return "Confirm action", "confirm"
	}
}

func emphasize(question, expected string) string {
	t := theme.Current()
	words := strings.Fields(safe(question))
	id := false
	for i, word := range words {
		id = id || strings.HasPrefix(word, "_id:")
		if id || strings.Contains(word, ".") || strings.ContainsAny(word, "'\"") ||
			(expected != "" && strings.Contains(word, safe(expected))) {
			words[i] = t.Text.Bold(true).Render(word)
		} else {
			words[i] = t.Text.Render(word)
		}
	}
	return strings.Join(words, " ")
}

func dialogBox(rows []string, width, height int) string {
	if width == 0 || height == 0 {
		return ""
	}
	if width < 4 || height < 4 {
		return fit(theme.Current().Err.Render("Confirm"), width, min(1, height))
	}
	limit := height - 2
	if len(rows) > limit {
		footer := rows[len(rows)-1]
		rows = append(rows[:limit-1:limit-1], footer)
	}
	for i := range rows {
		rows[i] = fitWidth(rows[i], width-4)
	}
	return theme.Current().Overlay.BorderForeground(theme.Current().P.Err).
		Render(strings.Join(rows, "\n"))
}

func dialogSize(view string) (int, int) {
	if view == "" {
		return 0, 0
	}
	return lipgloss.Size(view)
}

func inputView(input *textinput.Model) string {
	if input.Value() != "" {
		return input.View()
	}
	t := theme.Current()
	return t.PromptPrefix.Render(input.Prompt) + t.Accent.Render("▏") +
		t.Faint.Render(safe(input.Placeholder))
}

func newInput(prefix, placeholder string) textinput.Model {
	input := textinput.New()
	input.Prompt, input.Placeholder = prefix, placeholder
	styleInput(&input)
	return input
}

func styleInput(input *textinput.Model) {
	t := theme.Current()
	state := textinput.StyleState{
		Text: t.Text, Prompt: t.PromptPrefix, Placeholder: t.Faint, Suggestion: t.Suggestion,
	}
	input.SetStyles(textinput.Styles{
		Focused: state, Blurred: state,
		Cursor: textinput.CursorStyle{Color: t.P.Accent, Shape: tea.CursorBar, Blink: true},
	})
}

func safe(text string) string {
	return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(ansi.Strip(text))
}

func fitWidth(text string, w int) string {
	return theme.FitWidth(theme.FitWidth(text, w), w)
}

func fit(text string, w, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	rows := make([]string, h)
	for i := range rows {
		if i < len(lines) {
			rows[i] = fitWidth(lines[i], w)
		} else {
			rows[i] = theme.FitWidth("", w)
		}
	}
	return strings.Join(rows, "\n")
}

// Place centers a rectangular box over the base without dimming other cells.
// Canvas trims trailing spaces, so normalize its output back to exactly w×h.
func Place(base, box string, w, h int) string {
	w, h = max(0, w), max(0, h)
	if w == 0 || h == 0 {
		return fit("", w, h)
	}
	canvas := lipgloss.NewCanvas(w, h)
	layers := []*lipgloss.Layer{lipgloss.NewLayer(fit(base, w, h))}
	if box != "" {
		boxLines := strings.Split(box, "\n")
		boxW := 0
		for _, line := range boxLines {
			boxW = max(boxW, ansi.StringWidth(line))
		}
		availableW, availableH := w, h
		if w >= 12 {
			availableW -= 8
		}
		if h >= 6 {
			availableH -= 4
		}
		boxW, boxH := min(availableW, boxW), min(availableH, len(boxLines))
		layers = append(layers, lipgloss.NewLayer(fit(box, boxW, boxH)).X((w-boxW)/2).Y((h-boxH)/2))
	}
	// Layer.Draw itself ignores X/Y; the compositor applies layer bounds.
	canvas.Compose(lipgloss.NewCompositor(layers...))
	return fit(canvas.Render(), w, h)
}
