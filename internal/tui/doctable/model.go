// Package doctable renders a MongoDB document page as a table or compact,
// syntax-coloured shell JSON. It renders pane bodies only; the root owns focus,
// selection actions and pane framing.
package doctable

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/clipperhouse/uax29/v2/graphemes"
	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
	"tango/internal/theme"
)

// Mode determines the document representation.
type Mode uint8

const (
	ModeTable Mode = iota
	ModeJSON
)

const (
	defaultMaxWidth = 32
	gutterWidth     = 2
	columnGap       = 2
)

// Model owns page-local navigation and cached, unstyled document values.
// Call SetDocs again after mutating the supplied documents.
type Model struct {
	width, height int
	docs          []bson.D
	selected      map[int]bool
	columns       []string
	widths        []int
	cells         [][]cell
	json          [][]mjson.Token
	jsonWidths    []int
	jsonOffsets   [][]int
	jsonWidth     int
	jsonMaxOffset int
	cursor, top   int
	firstColumn   int
	jsonOffset    int
	mode          Mode
	emptyText     string
}

// New creates an empty table with a "No documents" placeholder.
func New() *Model { return &Model{emptyText: "No documents"} }

// SetSize sets the body dimensions; negative dimensions become zero.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = max(0, w), max(0, h)
	m.keepVisible()
	m.updateJSONLimit()
	m.clampJSONOffset()
}

// SetDocs replaces the page and derives columns and widths once. The cursor and
// horizontal offsets are preserved when still valid. Selection is independent.
func (m *Model) SetDocs(docs []bson.D) {
	m.docs = docs
	p := derivePage(docs)
	m.columns = p.columns
	m.widths = make([]int, len(p.columns))
	m.cells = make([][]cell, len(docs))
	midnight := make([]bool, len(p.columns))
	for j, column := range p.columns {
		midnight[j] = dateOnly(p.rows, column)
		m.widths[j] = ansi.StringWidth(escapeControls(column))
	}
	for i, row := range p.rows {
		m.cells[i] = make([]cell, len(p.columns))
		for j, column := range p.columns {
			value, present := row[column]
			c := formatCell(value, present, midnight[j])
			m.cells[i][j] = c
			header := ansi.StringWidth(escapeControls(column))
			limit := max(defaultMaxWidth, header)
			m.widths[j] = max(m.widths[j], min(limit, ansi.StringWidth(c.text)))
		}
	}
	m.json, m.jsonWidths, m.jsonOffsets = nil, nil, nil
	m.jsonWidth, m.jsonMaxOffset = 0, 0
	if m.mode == ModeJSON {
		m.cacheJSON()
	}
	m.firstColumn = min(m.firstColumn, max(0, len(m.columns)-1))
	m.SetCursor(m.cursor)
}

// SetSelected sets page-local multi-selection. A copy prevents external edits
// from changing the rendered state without another call.
func (m *Model) SetSelected(selected map[int]bool) {
	m.selected = make(map[int]bool, len(selected))
	for i, selected := range selected {
		m.selected[i] = selected
	}
}

// SetEmptyText changes the centered placeholder used for empty pages.
func (m *Model) SetEmptyText(s string) { m.emptyText = escapeControls(s) }

// Cursor returns the page-local cursor index, or zero for an empty page.
func (m *Model) Cursor() int { return m.cursor }

// SetCursor clamps the page-local cursor and scrolls it into view.
func (m *Model) SetCursor(i int) {
	m.cursor = max(0, min(i, len(m.docs)-1))
	m.keepVisible()
}

// Mode returns the active representation.
func (m *Model) Mode() Mode { return m.mode }

// ToggleMode switches representations, preserving their independent horizontal
// positions. Table mode reserves one header line; JSON uses every body line.
// JSON horizontal navigation moves by grapheme (one user-perceived character).
func (m *Model) ToggleMode() {
	if m.mode == ModeTable {
		m.mode = ModeJSON
		m.cacheJSON()
	} else {
		m.mode = ModeTable
	}
	m.keepVisible()
	m.updateJSONLimit()
	m.clampJSONOffset()
}

// Columns returns a copy of the derived column names, in display order.
func (m *Model) Columns() []string { return append([]string(nil), m.columns...) }

type visibleColumn struct {
	index int
	width int
}

func (m *Model) visibleColumns() []visibleColumn {
	remaining := max(0, m.width-gutterWidth)
	if remaining == 0 {
		return nil
	}
	var visible []visibleColumn
	for i := m.firstColumn; i < len(m.columns); i++ {
		width := m.widths[i]
		if len(visible) == 0 {
			width = min(width, remaining)
		} else {
			if columnGap+width > remaining {
				break
			}
			remaining -= columnGap
		}
		visible = append(visible, visibleColumn{index: i, width: width})
		remaining -= width
	}
	if len(visible) > 0 {
		visible[len(visible)-1].width += remaining
	}
	return visible
}

// ColumnInfo reports the currently visible column range for pane chrome.
// It is empty when every column fits or in JSON mode.
func (m *Model) ColumnInfo() string {
	if m.mode != ModeTable || len(m.columns) == 0 {
		return ""
	}
	visible := m.visibleColumns()
	if len(visible) == len(m.columns) {
		return ""
	}
	if len(visible) == 0 {
		return fmt.Sprintf("cols 0 of %d", len(m.columns))
	}
	return fmt.Sprintf("cols %d–%d of %d", m.firstColumn+1, m.firstColumn+len(visible), len(m.columns))
}

func (m *Model) rowCount() int {
	if m.mode == ModeTable {
		return max(0, m.height-1)
	}
	return m.height
}

func (m *Model) keepVisible() {
	rows := max(1, m.rowCount())
	m.top = max(0, min(m.top, len(m.docs)-rows))
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+rows {
		m.top = m.cursor - rows + 1
	}
}

// Handles reports whether Update consumes a key. The root must check this
// before routing keys elsewhere: navigation is synchronous and returns no Cmd.
// Enter, space and mode-toggle bindings belong to the root, not this component.
func (m *Model) Handles(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "j", "k", "up", "down", "g", "G", "home", "end",
		"ctrl+d", "ctrl+u", "pgup", "pgdown", "h", "l", "left", "right", "0", "$":
		return true
	default:
		return false
	}
}

// Update handles only page-local navigation. It emits no domain messages.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.Handles(key) {
		return nil
	}
	rows := max(1, m.rowCount())
	switch key.String() {
	case "j", "down":
		m.SetCursor(m.cursor + 1)
	case "k", "up":
		m.SetCursor(m.cursor - 1)
	case "g", "home":
		m.SetCursor(0)
	case "G", "end":
		m.SetCursor(len(m.docs) - 1)
	case "ctrl+d":
		m.SetCursor(m.cursor + max(1, rows/2))
	case "ctrl+u":
		m.SetCursor(m.cursor - max(1, rows/2))
	case "pgdown":
		m.SetCursor(m.cursor + rows)
	case "pgup":
		m.SetCursor(m.cursor - rows)
	default:
		m.scrollHorizontal(key.String())
	}
	return nil
}

func (m *Model) scrollHorizontal(key string) {
	if m.mode == ModeJSON {
		switch key {
		case "h", "left":
			m.jsonOffset--
		case "l", "right":
			m.jsonOffset++
		case "0":
			m.jsonOffset = 0
		case "$":
			m.jsonOffset = m.jsonMaxOffset
		}
		m.clampJSONOffset()
		return
	}
	switch key {
	case "h", "left":
		m.firstColumn--
	case "l", "right":
		m.firstColumn++
	case "0":
		m.firstColumn = 0
	case "$":
		m.firstColumn = len(m.columns) - 1
	}
	m.firstColumn = max(0, min(m.firstColumn, len(m.columns)-1))
}

func (m *Model) cacheJSON() {
	if m.json != nil {
		return
	}
	m.json = make([][]mjson.Token, len(m.docs))
	m.jsonWidths = make([]int, len(m.docs))
	m.jsonOffsets = make([][]int, len(m.docs))
	for i, doc := range m.docs {
		tokens := mjson.Tokens(doc, false)
		width := 0
		for j := range tokens {
			tokens[j].Text = escapeControls(tokens[j].Text)
			width += ansi.StringWidth(tokens[j].Text)
		}
		m.json[i] = tokens
		m.jsonWidths[i] = width
		text := mjson.Join(tokens)
		if !isASCII(text) {
			offsets := []int{0}
			iterator := graphemes.FromString(text)
			for iterator.Next() {
				offsets = append(offsets, offsets[len(offsets)-1]+ansi.StringWidth(iterator.Value()))
			}
			m.jsonOffsets[i] = offsets
		}
		m.jsonWidth = max(m.jsonWidth, width)
	}
	m.updateJSONLimit()
	m.clampJSONOffset()
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func (m *Model) updateJSONLimit() {
	m.jsonMaxOffset = 0
	width := max(1, m.width-gutterWidth)
	for i, total := range m.jsonWidths {
		offset := max(0, total-width)
		if offsets := m.jsonOffsets[i]; len(offsets) > 0 {
			offset = sort.SearchInts(offsets, offset)
		}
		m.jsonMaxOffset = max(m.jsonMaxOffset, offset)
	}
}

func (m *Model) clampJSONOffset() {
	if m.mode == ModeJSON {
		m.jsonOffset = max(0, min(m.jsonOffset, m.jsonMaxOffset))
	}
}

// View renders exactly height lines of width terminal cells. A zero-height body
// is empty. No framing is included. Only visible rows receive ANSI styling.
func (m *Model) View() string {
	if m.height == 0 {
		return ""
	}
	t := theme.Current()
	lines := make([]string, m.height)
	blank := fit("", m.width)
	for i := range lines {
		lines[i] = blank
	}
	if len(m.docs) == 0 {
		text := fit(m.emptyText, m.width)
		left := max(0, (m.width-ansi.StringWidth(ansi.Truncate(m.emptyText, m.width, "…")))/2)
		lines[(m.height-1)/2] = t.Faint.Render(fit(strings.Repeat(" ", left)+strings.TrimRight(text, " "), m.width))
		return strings.Join(lines, "\n")
	}
	columns := m.visibleColumns()
	start := 0
	if m.mode == ModeTable {
		var header strings.Builder
		header.WriteString(fit("", min(gutterWidth, m.width)))
		for i, column := range columns {
			if i > 0 {
				header.WriteString(strings.Repeat(" ", columnGap))
			}
			header.WriteString(fit(escapeControls(m.columns[column.index]), column.width))
		}
		lines[0] = t.Header.Render(fit(header.String(), m.width))
		start = 1
	}
	for y, index := start, m.top; y < m.height && index < len(m.docs); y, index = y+1, index+1 {
		cursor := index == m.cursor
		gutter := m.renderGutter(index, cursor, t)
		if m.mode == ModeJSON {
			lines[y] = gutter + m.renderJSON(index, max(0, m.width-gutterWidth), cursor, t)
			continue
		}
		var row strings.Builder
		row.WriteString(gutter)
		for i, column := range columns {
			if i > 0 {
				row.WriteString(rowStyle(t.Text, cursor, t).Render(strings.Repeat(" ", columnGap)))
			}
			row.WriteString(renderCell(m.cells[index][column.index], column.width, cursor, t))
		}
		padding := max(0, m.width-ansi.StringWidth(row.String()))
		if padding > 0 {
			row.WriteString(rowStyle(t.Text, cursor, t).Render(strings.Repeat(" ", padding)))
		}
		lines[y] = row.String()
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderGutter(index int, cursor bool, t *theme.Theme) string {
	var b strings.Builder
	if m.width > 0 {
		mark := " "
		if cursor {
			mark = "▎"
		}
		b.WriteString(rowStyle(t.Accent, cursor, t).Render(mark))
	}
	if m.width > 1 {
		mark := " "
		if m.selected[index] {
			mark = "●"
		}
		b.WriteString(rowStyle(t.SelectMark, cursor, t).Render(mark))
	}
	return b.String()
}

func (m *Model) renderJSON(index, width int, cursor bool, t *theme.Theme) string {
	if width <= 0 {
		return ""
	}
	tokens := m.json[index]
	total := m.jsonWidths[index]
	offset := min(m.jsonOffset, total)
	if offsets := m.jsonOffsets[index]; len(offsets) > 0 {
		offset = offsets[min(m.jsonOffset, len(offsets)-1)]
	}
	truncated := total-offset > width
	limit := width
	if truncated {
		limit--
	}
	position, used := 0, 0
	end := offset + limit
	var b strings.Builder
	for _, token := range tokens {
		size := ansi.StringWidth(token.Text)
		next := position + size
		if next <= offset {
			position = next
			continue
		}
		left, right := max(0, offset-position), min(size, end-position)
		segment := ansi.Cut(token.Text, left, right)
		// Cut can retain a wide grapheme that straddles the left boundary.
		segment = ansi.Truncate(segment, min(limit-used, right-left), "")
		b.WriteString(rowStyle(tokenStyle(t, token.Kind), cursor, t).Render(segment))
		used += ansi.StringWidth(segment)
		position = next
		if position >= end {
			break
		}
	}
	if truncated {
		b.WriteString(rowStyle(t.JSONPunct, cursor, t).Render("…"))
		used++
	}
	b.WriteString(rowStyle(t.Text, cursor, t).Render(strings.Repeat(" ", max(0, width-used))))
	return b.String()
}
