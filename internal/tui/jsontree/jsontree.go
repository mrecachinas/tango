// Package jsontree renders a collapsible BSON document body. The root should
// check Handles before global shortcuts; nil Update commands still consume
// navigation keys. Escape is deliberately unbound so the root can leave the view.
package jsontree

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
	"tango/internal/theme"
)

type KeyMap struct {
	Up, Down, First, Last, HalfUp, HalfDown, PageUp, PageDown key.Binding
	Toggle, Expand, Collapse, ExpandAll, CollapseAll          key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:          key.NewBinding(key.WithKeys("k", "up")),
		Down:        key.NewBinding(key.WithKeys("j", "down")),
		First:       key.NewBinding(key.WithKeys("g", "home")),
		Last:        key.NewBinding(key.WithKeys("G", "end")),
		HalfUp:      key.NewBinding(key.WithKeys("ctrl+u")),
		HalfDown:    key.NewBinding(key.WithKeys("ctrl+d")),
		PageUp:      key.NewBinding(key.WithKeys("pgup")),
		PageDown:    key.NewBinding(key.WithKeys("pgdown")),
		Toggle:      key.NewBinding(key.WithKeys("enter", "space", " ")),
		Expand:      key.NewBinding(key.WithKeys("l", "right")),
		Collapse:    key.NewBinding(key.WithKeys("h", "left")),
		ExpandAll:   key.NewBinding(key.WithKeys("E")),
		CollapseAll: key.NewBinding(key.WithKeys("C")),
	}
}

type node struct {
	key, path                  string
	tokens                     []mjson.Token
	parent                     *node
	children                   []*node
	container, array, expanded bool
}

type row struct {
	node   *node
	guides string
}

type Model struct {
	Keys                          KeyMap
	root                          *node
	rows                          []row
	cursor, offset, width, height int
}

func New() *Model { return &Model{Keys: DefaultKeyMap()} }

func (m *Model) SetSize(w, h int) {
	m.width, m.height = max(0, w), max(0, h)
	m.ensureVisible()
}

func (m *Model) SetDoc(doc bson.D) {
	m.root = buildNode("", "", doc, nil)
	m.root.expanded = true
	for _, child := range m.root.children {
		child.expanded = child.container && len(child.children) <= 20
	}
	m.cursor, m.offset = 0, 0
	m.rebuild()
}

// Path uses dotted object keys and bracketed array indexes; the document root
// has an empty path (e.g. metadata.browser or events[0].timestamp).
func (m *Model) Path() string {
	if m.cursor < len(m.rows) {
		return m.rows[m.cursor].node.path
	}
	return ""
}

func (m *Model) Handles(msg tea.KeyPressMsg) bool {
	k := m.Keys
	return key.Matches(msg, k.Up, k.Down, k.First, k.Last, k.HalfUp, k.HalfDown,
		k.PageUp, k.PageDown, k.Toggle, k.Expand, k.Collapse, k.ExpandAll, k.CollapseAll)
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok || len(m.rows) == 0 {
		return nil
	}
	n := m.rows[m.cursor].node
	switch keys := m.Keys; {
	case key.Matches(k, keys.Up):
		m.cursor--
	case key.Matches(k, keys.Down):
		m.cursor++
	case key.Matches(k, keys.First):
		m.cursor = 0
	case key.Matches(k, keys.Last):
		m.cursor = len(m.rows) - 1
	case key.Matches(k, keys.HalfUp):
		m.cursor -= max(1, m.height/2)
	case key.Matches(k, keys.HalfDown):
		m.cursor += max(1, m.height/2)
	case key.Matches(k, keys.PageUp):
		m.cursor -= max(1, m.height)
	case key.Matches(k, keys.PageDown):
		m.cursor += max(1, m.height)
	case key.Matches(k, keys.Toggle):
		if n.container {
			n.expanded = !n.expanded
			m.rebuild()
		}
	case key.Matches(k, keys.Expand):
		if n.container && !n.expanded {
			n.expanded = true
			m.rebuild()
		} else if len(n.children) > 0 {
			m.cursor++
		}
	case key.Matches(k, keys.Collapse):
		if n.container && n.expanded {
			n.expanded = false
			m.rebuild()
		} else if n.parent != nil {
			m.selectNode(n.parent)
		}
	case key.Matches(k, keys.ExpandAll):
		setExpanded(m.root, true)
		m.rebuild()
	case key.Matches(k, keys.CollapseAll):
		setExpanded(m.root, false)
		// Keep the document root open so collapse-all leaves its fields navigable.
		m.root.expanded = true
		m.rebuild()
	}
	m.cursor = min(max(0, m.cursor), len(m.rows)-1)
	m.ensureVisible()
	return nil
}

func buildNode(name, path string, value interface{}, parent *node) *node {
	n := &node{key: name, path: path, parent: parent}
	var object bson.D
	var array []interface{}
	switch value := value.(type) {
	case bson.D:
		object = value
		n.container = true
	case bson.M:
		object = orderedMap(value)
		n.container = true
	case map[string]interface{}:
		object = orderedMap(value)
		n.container = true
	case bson.A:
		array = value
		n.container, n.array = true, true
	case []interface{}:
		array = value
		n.container, n.array = true, true
	case bson.Raw:
		if err := bson.Unmarshal(value, &object); err == nil {
			n.container = true
		} else {
			object = nil
		}
	}
	for _, e := range object {
		path := e.Key
		if n.path != "" {
			path = n.path + "." + e.Key
		}
		n.children = append(n.children, buildNode(e.Key, path, e.Value, n))
	}
	for i, item := range array {
		name := "[" + strconv.Itoa(i) + "]"
		n.children = append(n.children, buildNode(name, n.path+name, item, n))
	}
	if !n.container {
		n.tokens = mjson.Tokens(value, false)
	}
	return n
}

func orderedMap(value map[string]interface{}) bson.D {
	keys := make([]string, 0, len(value))
	for k := range value {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(bson.D, 0, len(keys))
	for _, k := range keys {
		out = append(out, bson.E{Key: k, Value: value[k]})
	}
	return out
}

func setExpanded(n *node, expanded bool) {
	n.expanded = n.container && expanded
	for _, child := range n.children {
		setExpanded(child, expanded)
	}
}

func (m *Model) rebuild() {
	var selected *node
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		selected = m.rows[m.cursor].node
	}
	m.rows = nil
	if m.root != nil {
		m.rows = append(m.rows, row{node: m.root})
		m.flatten(m.root, "")
	}
	// A hidden selected descendant falls back to the nearest visible ancestor.
	for selected != nil {
		if m.selectNode(selected) {
			m.ensureVisible()
			return
		}
		selected = selected.parent
	}
	m.cursor = min(max(0, m.cursor), max(0, len(m.rows)-1))
	m.ensureVisible()
}

func (m *Model) flatten(n *node, prefix string) {
	if !n.expanded {
		return
	}
	for i, child := range n.children {
		branch, next := "├─ ", "│  "
		if i == len(n.children)-1 {
			branch, next = "└─ ", "   "
		}
		m.rows = append(m.rows, row{node: child, guides: prefix + branch})
		m.flatten(child, prefix+next)
	}
}

func (m *Model) selectNode(n *node) bool {
	for i, row := range m.rows {
		if row.node == n {
			m.cursor = i
			return true
		}
	}
	return false
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
	m.offset = min(max(0, m.offset), max(0, len(m.rows)-m.height))
}

func (m *Model) View() string {
	if m.height <= 0 {
		return ""
	}
	lines := make([]string, m.height)
	for y := range lines {
		line := ""
		if i := m.offset + y; i < len(m.rows) {
			line = m.renderRow(m.rows[i], i == m.cursor)
		} else if m.root == nil && y == 0 {
			line = theme.Current().Faint.Render("No document")
		}
		lines[y] = fitWidth(line, m.width)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderRow(row row, selected bool) string {
	t := theme.Current()
	styled := func(style lipgloss.Style, text string) string {
		if selected {
			style = style.Inherit(t.CursorRow)
		}
		return style.Render(text)
	}
	n := row.node
	var b strings.Builder
	b.WriteString(styled(t.Faint, row.guides))
	marker := "  "
	if n.container {
		marker = "▸ "
		if n.expanded {
			marker = "▾ "
		}
	}
	b.WriteString(styled(t.Muted, marker))
	if n.parent != nil {
		b.WriteString(styled(t.JSONKey, cleanLine(n.key)))
		b.WriteString(styled(t.JSONPunct, ": "))
	}
	if n.container {
		count := fmt.Sprintf("{%d}", len(n.children))
		if n.array {
			count = fmt.Sprintf("[%d]", len(n.children))
		}
		b.WriteString(styled(t.Muted, count))
	} else {
		for _, token := range n.tokens {
			style := t.Text
			switch token.Kind {
			case mjson.Key:
				style = t.JSONKey
			case mjson.String:
				style = t.JSONString
			case mjson.Number:
				style = t.JSONNumber
			case mjson.Bool:
				style = t.JSONBool
			case mjson.Null:
				style = t.JSONNull
			case mjson.Helper:
				style = t.JSONHelper
			case mjson.Punct:
				style = t.JSONPunct
			case mjson.Error:
				style = t.JSONError
			}
			b.WriteString(styled(style, cleanLine(token.Text)))
		}
	}
	line := fitWidth(b.String(), m.width)
	if selected {
		line = t.CursorRow.Render(line)
	}
	return line
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
