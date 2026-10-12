package jsonview

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
	"tango/internal/theme"
)

func key(code rune, text string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: text} }

func dimensions(t *testing.T, view string, w, h int) {
	t.Helper()
	if h == 0 {
		if view != "" {
			t.Fatalf("zero-height view = %q", view)
		}
		return
	}
	rows := strings.Split(view, "\n")
	if len(rows) != h {
		t.Fatalf("height = %d, want %d: %q", len(rows), h, view)
	}
	for i, row := range rows {
		if got := ansi.StringWidth(row); got != w {
			t.Fatalf("row %d width = %d, want %d: %q", i, got, w, row)
		}
	}
}

func TestFindMatches(t *testing.T) {
	tests := []struct {
		hay, term string
		want      []Match
	}{
		{"Name name NAME", "name", []Match{{0, 4}, {5, 9}, {10, 14}}},
		{"aaaaa", "aa", []Match{{0, 2}, {2, 4}}},
		{"Äa äA", "äa", []Match{{0, 2}, {3, 5}}},
		{"Σςσ", "σ", []Match{{0, 1}, {2, 3}}},
		{"a\nB", "A\nb", []Match{{0, 3}}},
		{"x", "", nil},
		{"", "x", nil},
		{"x", "xx", nil},
	}
	for _, tt := range tests {
		t.Run(tt.hay+"/"+tt.term, func(t *testing.T) {
			// Splitting tokens must not split the search space.
			var toks []mjson.Token
			for _, r := range tt.hay {
				toks = append(toks, mjson.Token{Kind: mjson.String, Text: string(r)})
			}
			got := FindMatches(toks, tt.term)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("matches = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRenderDrawsExactDocument(t *testing.T) {
	doc := bson.D{
		{Key: "label", Value: "[red]hi[-] [::b]bold [\"region\"] [yellow[]"},
		{Key: "tags", Value: bson.A{"x", "[y]", bson.A{}}},
		{Key: "[k]", Value: bson.D{{Key: "a", Value: int32(1)}}},
		{Key: "brackets", Value: "[[[]]] [] [-:-] [#ff0000]"},
	}
	for _, pretty := range []bool{true, false} {
		toks := mjson.Tokens(doc, pretty)
		plain := mjson.Join(toks)
		for _, term := range []string{"", "y", "[", "hi[-]", "\"x\""} {
			m := New(toks)
			h := strings.Count(plain, "\n") + 1
			m.SetSize(300, h)
			m.SetSearch(term)
			var rows []string
			for _, row := range strings.Split(ansi.Strip(m.View()), "\n") {
				rows = append(rows, strings.TrimRight(row, " "))
			}
			if got := strings.Join(rows, "\n"); got != plain {
				t.Fatalf("pretty=%v term=%q\n got: %q\nwant: %q", pretty, term, got, plain)
			}
			dimensions(t, m.View(), 300, h)
		}
	}
}

func TestMatchHighlightAndCurrent(t *testing.T) {
	m := New([]mjson.Token{{Kind: mjson.Key, Text: "Name"}, {Kind: mjson.String, Text: " name NAME"}})
	m.SetSize(30, 1)
	m.SetSearch("name")
	if got := m.MatchInfo(); got != "1/3" {
		t.Fatal(got)
	}
	current := theme.Current().MatchHL.Bold(true).Underline(true).Render("Name")
	ordinary := theme.Current().MatchHL.Render("name")
	if !strings.Contains(m.View(), current) || !strings.Contains(m.View(), ordinary) {
		t.Fatalf("missing distinct highlight: %q", m.View())
	}
	m.NextMatch()
	if m.MatchInfo() != "2/3" {
		t.Fatal(m.MatchInfo())
	}
	m.PrevMatch()
	m.PrevMatch()
	if m.MatchInfo() != "3/3" {
		t.Fatal(m.MatchInfo())
	}
	m.SetSearch("missing")
	if m.MatchInfo() != "no matches" {
		t.Fatal(m.MatchInfo())
	}
	if strings.Contains(m.View(), theme.Current().MatchHL.Render("Name")) {
		t.Fatal("stale highlight")
	}
}

func TestWrapDimensionsAndGutter(t *testing.T) {
	m := New()
	m.SetText("👩‍💻 界 abcdefg\ne\u0301中\t123\nlast")
	for _, gutter := range []bool{false, true} {
		m.SetLineNumbers(gutter)
		for _, w := range []int{0, 1, 2, 3, 4, 5, 7, 9, 20, 40} {
			for _, h := range []int{0, 1, 2, 9} {
				m.SetSize(w, h)
				dimensions(t, m.View(), w, h)
			}
		}
	}
	m.SetSize(10, 6)
	if got := ansi.Strip(m.View()); !strings.HasPrefix(got, "1 ") || !strings.Contains(got, "\n2 ") {
		t.Fatalf("missing line numbers: %q", got)
	}
}

func TestNavigationAndSearchReveal(t *testing.T) {
	m := New()
	m.SetText("first\nsecond\nneedle\nfourth\nfifth\nNEEDLE\nlast")
	m.SetSize(12, 2)
	m.SetSearch("needle")
	if m.Offset() != 1 {
		t.Fatalf("first match not revealed: %d", m.Offset())
	}
	m.Update(key('n', "n"))
	if m.MatchInfo() != "2/2" || !strings.Contains(ansi.Strip(m.View()), "NEEDLE") {
		t.Fatalf("next match not revealed: %q", m.View())
	}
	m.Update(key('N', "N"))
	if m.MatchInfo() != "1/2" || !strings.Contains(ansi.Strip(m.View()), "needle") {
		t.Fatalf("previous match not revealed: %q", m.View())
	}
	m.Update(key('G', "G"))
	if m.ScrollPercent() != 1 {
		t.Fatal(m.ScrollPercent())
	}
	m.Update(key('g', "g"))
	if m.ScrollPercent() != 0 {
		t.Fatal(m.ScrollPercent())
	}
	for _, k := range []tea.KeyPressMsg{
		key('j', "j"), key(tea.KeyDown, ""), {Code: 'd', Mod: tea.ModCtrl}, key(tea.KeyPgDown, ""),
	} {
		if !m.Handles(k) {
			t.Fatal("navigation not handled")
		}
		m.Update(k)
	}
	if m.ScrollPercent() != 1 {
		t.Fatal("scroll did not reach bottom")
	}
	for _, k := range []tea.KeyPressMsg{
		key('k', "k"), key(tea.KeyUp, ""), {Code: 'u', Mod: tea.ModCtrl}, key(tea.KeyPgUp, ""),
	} {
		m.Update(k)
	}
	if m.ScrollPercent() != 0 || m.Handles(key('x', "x")) {
		t.Fatal("unexpected scroll/key handling")
	}
}

func TestCacheAndTokenReplacement(t *testing.T) {
	m := New()
	m.SetText(strings.Repeat("long document string\n", 400))
	m.SetSize(25, 8)
	cache := &m.lines[0]
	m.SetTokens(append([]mjson.Token(nil), m.tokens...))
	m.SetSize(25, 10)
	m.SetSearch("document")
	m.NextMatch()
	m.View()
	if cache != &m.lines[0] {
		t.Fatal("height, search, or rendering unnecessarily rebuilt wrapped lines")
	}
	m.SetTokens([]mjson.Token{{Text: "error", Kind: mjson.Error}})
	if m.LineCount() != 1 || m.Offset() != 0 || m.MatchInfo() != "no matches" {
		t.Fatal("token replacement retained old state")
	}
	if !strings.Contains(m.View(), theme.Current().JSONError.Render("error")) {
		t.Fatal("error styling missing")
	}
}

func TestEveryTokenStyle(t *testing.T) {
	tm := theme.Current()
	tests := []struct {
		kind mjson.Kind
		want string
	}{
		{mjson.Punct, tm.JSONPunct.Render("sample")},
		{mjson.Key, tm.JSONKey.Render("sample")},
		{mjson.String, tm.JSONString.Render("sample")},
		{mjson.Number, tm.JSONNumber.Render("sample")},
		{mjson.Bool, tm.JSONBool.Render("sample")},
		{mjson.Null, tm.JSONNull.Render("sample")},
		{mjson.Helper, tm.JSONHelper.Render("sample")},
		{mjson.Text, tm.Text.Render("sample")},
		{mjson.Error, tm.JSONError.Render("sample")},
	}
	for _, tt := range tests {
		m := New([]mjson.Token{{Kind: tt.kind, Text: "sample"}})
		m.SetSize(20, 1)
		if !strings.Contains(m.View(), tt.want) {
			t.Fatalf("kind %v did not use theme style: %q", tt.kind, m.View())
		}
	}
}

func TestTerminalControlsAreLiteral(t *testing.T) {
	m := New()
	m.SetText("\x1b[31mnot red\x1b[0m\r\x00")
	m.SetSize(100, 1)
	if got := strings.TrimSpace(ansi.Strip(m.View())); got != "\\x1b[31mnot red\\x1b[0m\\x0d\\x00" {
		t.Fatalf("unsafe message: %q", got)
	}
}

func BenchmarkCachedView(b *testing.B) {
	m := New()
	m.SetText(strings.Repeat("{ \"message\": \"a long document with matches\" }\n", 2000))
	m.SetSize(80, 30)
	m.SetSearch("document")
	b.ResetTimer()
	for b.Loop() {
		m.View()
	}
}
