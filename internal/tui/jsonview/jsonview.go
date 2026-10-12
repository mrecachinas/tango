// Package jsonview renders tokenized documents in a scrollable, soft-wrapped body.
package jsonview

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"tango/internal/mjson"
	"tango/internal/theme"
)

// Match contains rune offsets into the joined token text; End is exclusive.
type Match struct{ Start, End int }

type glyph struct {
	text       string
	start, end int
	kind       mjson.Kind
}

type line struct {
	glyphs []glyph
	number int // Zero for a continuation of a soft-wrapped source line.
}

type Model struct {
	tokens        []mjson.Token
	text          string
	kinds         []mjson.Kind
	lower         []rune
	width, height int
	offset        int
	lineNumbers   bool
	gutter        int
	lines         []line
	runeLines     []int
	term          string
	matches       []Match
	matchIDs      []int
	current       int
}

// New optionally accepts an initial token stream. View renders a body, not a pane.
func New(tokens ...[]mjson.Token) *Model {
	m := &Model{current: -1}
	if len(tokens) > 0 {
		m.SetTokens(tokens[0])
	}
	return m
}

func (m *Model) SetSize(w, h int) {
	w, h = max(0, w), max(0, h)
	rewrap := m.width != w
	m.width, m.height = w, h
	if rewrap {
		m.wrap()
	}
	m.clamp()
}

func (m *Model) SetTokens(tokens []mjson.Token) {
	if slices.Equal(tokens, m.tokens) {
		return
	}
	m.tokens = make([]mjson.Token, len(tokens))
	m.kinds = nil
	for i, tok := range tokens {
		tok.Text = displayText(tok.Text)
		m.tokens[i] = tok
		for range []rune(tok.Text) {
			m.kinds = append(m.kinds, tok.Kind)
		}
	}
	m.text = mjson.Join(m.tokens)
	m.lower = []rune(m.text)
	for i, r := range m.lower {
		m.lower[i] = unicode.ToLower(r)
	}
	m.offset = 0
	m.wrap()
	m.find()
}

func (m *Model) SetText(text string) {
	m.SetTokens([]mjson.Token{{Kind: mjson.Text, Text: text}})
}

func (m *Model) SetLineNumbers(show bool) {
	if m.lineNumbers != show {
		m.lineNumbers = show
		m.wrap()
		m.clamp()
	}
}

func (m *Model) ToggleLineNumbers() { m.SetLineNumbers(!m.lineNumbers) }

func (m *Model) SetSearch(term string) {
	if term == m.term {
		return
	}
	m.term = term
	m.find()
}

func (m *Model) find() {
	m.matches = findMatches(m.lower, m.term)
	m.matchIDs = nil
	m.current = -1
	if len(m.matches) == 0 {
		return
	}
	m.matchIDs = make([]int, len(m.lower))
	for i := range m.matchIDs {
		m.matchIDs[i] = -1
	}
	for id, match := range m.matches {
		for i := match.Start; i < match.End; i++ {
			m.matchIDs[i] = id
		}
	}
	m.current = 0
	m.reveal()
}

// FindMatches finds non-overlapping, case-insensitive matches across token boundaries.
func FindMatches(tokens []mjson.Token, term string) []Match {
	hay := []rune(mjson.Join(tokens))
	for i, r := range hay {
		hay[i] = unicode.ToLower(r)
	}
	return findMatches(hay, term)
}

func findMatches(hay []rune, term string) []Match {
	needle := []rune(term)
	if len(needle) == 0 {
		return nil
	}
	for i, r := range needle {
		needle[i] = unicode.ToLower(r)
	}
	// KMP keeps incremental searches linear even for long repeated strings.
	failure := make([]int, len(needle))
	for i, j := 1, 0; i < len(needle); i++ {
		for j > 0 && needle[i] != needle[j] {
			j = failure[j-1]
		}
		if needle[i] == needle[j] {
			j++
		}
		failure[i] = j
	}
	var matches []Match
	for i, j := 0, 0; i < len(hay); i++ {
		for j > 0 && hay[i] != needle[j] {
			j = failure[j-1]
		}
		if hay[i] == needle[j] {
			j++
		}
		if j == len(needle) {
			matches = append(matches, Match{Start: i + 1 - j, End: i + 1})
			j = 0 // Match state/render.go's non-overlapping semantics.
		}
	}
	return matches
}

func (m *Model) NextMatch() {
	if len(m.matches) > 0 {
		m.current = (m.current + 1) % len(m.matches)
		m.reveal()
	}
}

func (m *Model) PrevMatch() {
	if len(m.matches) > 0 {
		m.current = (m.current + len(m.matches) - 1) % len(m.matches)
		m.reveal()
	}
}

func (m *Model) MatchInfo() string {
	if len(m.matches) == 0 {
		return "no matches"
	}
	return fmt.Sprintf("%d/%d", m.current+1, len(m.matches))
}

// ScrollPercent is in [0, 1]. A document that fits reports 0.
func (m *Model) ScrollPercent() float64 {
	bottom := len(m.lines) - m.height
	if bottom <= 0 {
		return 0
	}
	return float64(m.offset) / float64(bottom)
}

func (m *Model) LineCount() int { return len(m.lines) }
func (m *Model) Offset() int    { return m.offset }

// Handles distinguishes a consumed navigation key from an unhandled nil command.
func (m *Model) Handles(key tea.KeyPressMsg) bool {
	switch key.String() {
	case "j", "k", "up", "down", "g", "G", "home", "end",
		"ctrl+d", "ctrl+u", "pgdown", "pgup", "n", "N":
		return true
	default:
		return false
	}
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
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
	case "n":
		m.NextMatch()
	case "N":
		m.PrevMatch()
	}
	m.clamp()
	return nil
}

func (m *Model) clamp() { m.offset = min(max(0, m.offset), max(0, len(m.lines)-m.height)) }

func (m *Model) reveal() {
	if m.current < 0 || len(m.runeLines) == 0 {
		return
	}
	row := m.runeLines[m.matches[m.current].Start]
	if row < m.offset {
		m.offset = row
	} else if row >= m.offset+m.height {
		m.offset = row - max(1, m.height) + 1
	}
	m.clamp()
}

func (m *Model) wrap() {
	m.lines = nil
	m.runeLines = make([]int, len(m.lower))
	m.gutter = 0
	if m.width <= 0 {
		return
	}
	if m.lineNumbers {
		m.gutter = len(strconv.Itoa(strings.Count(m.text, "\n")+1)) + 1
		if m.gutter >= m.width {
			m.gutter = 0
		}
	}
	w := m.width - m.gutter
	m.lines = append(m.lines, line{number: 1})
	graphemes := uniseg.NewGraphemes(m.text)
	pos, col, number := 0, 0, 1
	for graphemes.Next() {
		text := graphemes.Str()
		end := pos + utf8.RuneCountInString(text)
		if text == "\n" {
			m.runeLines[pos] = len(m.lines) - 1
			number++
			m.lines = append(m.lines, line{number: number})
			col = 0
			pos = end
			continue
		}
		if text == "\t" {
			text = strings.Repeat(" ", min(4-col%4, w))
		}
		cells := ansi.StringWidth(text)
		if cells > w {
			text, cells = "…", 1
		}
		if col+cells > w {
			m.lines = append(m.lines, line{})
			col = 0
		}
		last := len(m.lines) - 1
		m.lines[last].glyphs = append(m.lines[last].glyphs, glyph{
			text: text, start: pos, end: end, kind: m.kinds[pos],
		})
		for i := pos; i < end; i++ {
			m.runeLines[i] = last
		}
		col += cells
		pos = end
	}
}

func (m *Model) View() string {
	if m.height <= 0 {
		return ""
	}
	rows := make([]string, m.height)
	t := theme.Current()
	for i := range rows {
		if row := m.offset + i; row < len(m.lines) {
			l := m.lines[row]
			var b strings.Builder
			if m.gutter > 0 {
				n := ""
				if l.number > 0 {
					n = strconv.Itoa(l.number)
				}
				b.WriteString(t.Faint.Render(fmt.Sprintf("%*s ", m.gutter-1, n)))
			}
			// Group runs before styling instead of emitting an ANSI reset per rune.
			var run strings.Builder
			kind, match := mjson.Kind(255), -2
			flush := func() {
				if run.Len() == 0 {
					return
				}
				style := tokenStyle(t, kind)
				if match >= 0 {
					style = t.MatchHL
					if match == m.current {
						style = style.Bold(true).Underline(true)
					}
				}
				b.WriteString(style.Render(run.String()))
				run.Reset()
			}
			for _, g := range l.glyphs {
				id := -1
				for p := g.start; p < g.end && m.matchIDs != nil; p++ {
					if m.matchIDs[p] >= 0 {
						id = m.matchIDs[p]
						break
					}
				}
				if g.kind != kind || id != match {
					flush()
					kind, match = g.kind, id
				}
				run.WriteString(g.text)
			}
			flush()
			rows[i] = b.String()
		}
		rows[i] = theme.FitWidth(rows[i], m.width)
	}
	return strings.Join(rows, "\n")
}

func tokenStyle(t *theme.Theme, kind mjson.Kind) lipgloss.Style {
	switch kind {
	case mjson.Punct:
		return t.JSONPunct
	case mjson.Key:
		return t.JSONKey
	case mjson.String:
		return t.JSONString
	case mjson.Number:
		return t.JSONNumber
	case mjson.Bool:
		return t.JSONBool
	case mjson.Null:
		return t.JSONNull
	case mjson.Helper:
		return t.JSONHelper
	case mjson.Error:
		return t.JSONError
	default:
		return t.Text
	}
}

// displayText prevents plain message/error text from injecting terminal controls.
func displayText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if (r < ' ' && r != '\n' && r != '\t') || r == 0x7f {
			fmt.Fprintf(&b, "\\x%02x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
