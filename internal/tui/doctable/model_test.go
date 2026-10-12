package doctable

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/mjson"
	"tango/internal/theme"
)

func keyMsg(key string) tea.KeyPressMsg {
	code := map[string]rune{
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"enter": tea.KeyEnter, "space": tea.KeySpace, "esc": tea.KeyEscape,
	}
	if r, ok := code[key]; ok {
		return tea.KeyPressMsg{Code: r}
	}
	if strings.HasPrefix(key, "ctrl+") {
		return tea.KeyPressMsg{Code: []rune(strings.TrimPrefix(key, "ctrl+"))[0], Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(key)[0], Text: key}
}

func testDocs(n int) []bson.D {
	docs := make([]bson.D, n)
	for i := range docs {
		docs[i] = bson.D{
			{Key: "_id", Value: primitive.NewObjectID()},
			{Key: "name", Value: fmt.Sprintf("row-%02d", i)},
			{Key: "metadata", Value: bson.D{{Key: "browser", Value: "Chrome"}, {Key: "os", Value: "日本 👩‍💻"}}},
			{Key: "count", Value: int64(i)},
			{Key: "timestamp", Value: time.Date(2025, 1, 23, 0, 0, 0, 0, time.UTC)},
		}
	}
	return docs
}

func assertDimensions(t *testing.T, m *Model) {
	t.Helper()
	view := m.View()
	if m.height == 0 {
		if view != "" {
			t.Fatalf("zero height: got %q", view)
		}
		return
	}
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("height = %d, want %d; view %q", len(lines), m.height, view)
	}
	for i, line := range lines {
		if width := ansi.StringWidth(line); width != m.width {
			t.Fatalf("line %d width = %d, want %d: %q", i, width, m.width, line)
		}
	}
}

func TestViewDimensions(t *testing.T) {
	docs := append(testDocs(8), bson.D{
		{Key: "key\nwith\x1bcontrols", Value: "a\nb\t\x1b"},
		{Key: "long", Value: strings.Repeat("日本 🇦🇷 👩‍💻 é", 20)},
		{Key: "regex", Value: primitive.Regex{Pattern: "x\x1by"}},
	})
	for _, size := range [][2]int{{0, 0}, {0, 3}, {1, 1}, {1, 4}, {2, 3}, {3, 4}, {4, 2}, {5, 3}, {10, 3}, {20, 6}, {45, 8}, {80, 20}, {180, 14}, {-2, -1}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := New()
			m.SetDocs(docs)
			m.SetSize(size[0], size[1])
			m.SetSelected(map[int]bool{0: true, 2: true})
			for mode := 0; mode < 2; mode++ {
				for _, key := range []string{"0", "l", "right", "$", "h", "left", "G", "k", "g"} {
					m.Update(keyMsg(key))
					assertDimensions(t, m)
				}
				m.ToggleMode()
			}
			m.SetDocs(nil)
			m.SetEmptyText("Loading…")
			assertDimensions(t, m)
		})
	}
}

func TestEmptyState(t *testing.T) {
	m := New()
	m.SetSize(20, 5)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if lines[2] != "    No documents    " {
		t.Fatalf("placeholder = %q", lines[2])
	}
	m.SetEmptyText("Loading…")
	if lines := strings.Split(ansi.Strip(m.View()), "\n"); lines[2] != "      Loading…      " {
		t.Fatalf("loading placeholder = %q", lines[2])
	}
	m.SetEmptyText("界界界界界界界界界界")
	m.SetSize(9, 3)
	assertDimensions(t, m)
	if !strings.Contains(ansi.Strip(m.View()), "…") {
		t.Fatal("long empty placeholder was not truncated")
	}
	m.SetEmptyText("a\nb\x1b")
	assertDimensions(t, m)
	if strings.Contains(ansi.Strip(m.View()), "a\nb") {
		t.Fatal("empty text contains an unescaped newline")
	}
}

func TestWidthFitting(t *testing.T) {
	m := New()
	m.SetDocs([]bson.D{{
		{Key: "a", Value: "xxx"},
		{Key: "b", Value: 12},
		{Key: "c", Value: "z"},
	}})
	m.SetSize(16, 3)
	want := []visibleColumn{{index: 0, width: 3}, {index: 1, width: 2}, {index: 2, width: 5}}
	if got := m.visibleColumns(); !reflect.DeepEqual(got, want) {
		t.Fatalf("widths = %+v, want %+v", got, want)
	}
	if m.ColumnInfo() != "" {
		t.Fatalf("all columns fit: %q", m.ColumnInfo())
	}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if lines[1] != "▎ xxx  12  z    " {
		t.Fatalf("row = %q", lines[1])
	}
	m.SetSize(9, 3)
	if got := m.ColumnInfo(); got != "cols 1–2 of 3" {
		t.Fatalf("range = %q", got)
	}
	m.Update(keyMsg("l"))
	if got := m.ColumnInfo(); got != "cols 2–3 of 3" {
		t.Fatalf("scrolled range = %q", got)
	}
	m.SetSize(3, 3)
	if got := m.visibleColumns(); len(got) != 1 || got[0].width != 1 {
		t.Fatalf("tiny widths = %+v", got)
	}
	assertDimensions(t, m)
}

func TestColumnWidthsAndCopies(t *testing.T) {
	longHeader := strings.Repeat("h", 40)
	m := New()
	m.SetDocs([]bson.D{{
		{Key: "str", Value: strings.Repeat("s", 200)},
		{Key: longHeader, Value: 1},
		{Key: "_id", Value: primitive.NewObjectID()},
	}})
	if want := []int{8, 32, 40}; !reflect.DeepEqual(m.widths, want) {
		t.Fatalf("natural widths = %v, want %v", m.widths, want)
	}
	copy := m.Columns()
	copy[0] = "changed"
	if m.Columns()[0] != "_id" {
		t.Fatal("Columns leaked internal storage")
	}
	m.SetDocs([]bson.D{{{Key: "str", Value: "short"}}})
	if m.widths[0] != 5 {
		t.Fatalf("widths not recomputed for new page: %v", m.widths)
	}
}

func TestVerticalNavigation(t *testing.T) {
	tests := []struct {
		key   string
		start int
		want  int
	}{
		{"j", 0, 1}, {"down", 1, 2}, {"k", 1, 0}, {"up", 2, 1},
		{"g", 15, 0}, {"G", 0, 19}, {"home", 15, 0}, {"end", 0, 19},
		{"ctrl+d", 3, 5}, {"ctrl+u", 8, 6},
		{"pgdown", 3, 8}, {"pgup", 8, 3},
		{"j", 19, 19}, {"k", 0, 0}, {"pgdown", 18, 19}, {"pgup", 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.key+fmt.Sprint(tt.start), func(t *testing.T) {
			m := New()
			m.SetSize(30, 6)
			m.SetDocs(testDocs(20))
			m.SetCursor(tt.start)
			if !m.Handles(keyMsg(tt.key)) {
				t.Fatalf("key %q not handled", tt.key)
			}
			if cmd := m.Update(keyMsg(tt.key)); cmd != nil {
				t.Fatal("navigation should be synchronous")
			}
			if m.Cursor() != tt.want {
				t.Fatalf("cursor = %d, want %d", m.Cursor(), tt.want)
			}
			if m.cursor < m.top || m.cursor >= m.top+m.rowCount() {
				t.Fatalf("cursor %d outside viewport starting at %d", m.cursor, m.top)
			}
			if got := strings.Count(ansi.Strip(m.View()), "▎"); got != 1 {
				t.Fatalf("visible cursor markers = %d", got)
			}
		})
	}
}

func TestCursorClampAndViewportResize(t *testing.T) {
	m := New()
	m.SetSize(40, 6)
	m.SetDocs(testDocs(20))
	m.SetCursor(19)
	if m.top != 15 {
		t.Fatalf("end viewport starts at %d, want 15", m.top)
	}
	m.SetSize(40, 3)
	if m.top != 18 {
		t.Fatalf("smaller viewport starts at %d, want 18", m.top)
	}
	m.ToggleMode()
	if m.top != 17 {
		t.Fatalf("JSON viewport starts at %d, want 17", m.top)
	}
	m.SetDocs(testDocs(2))
	if m.Cursor() != 1 || m.top != 0 {
		t.Fatalf("shorter page cursor %d top %d", m.Cursor(), m.top)
	}
	m.SetCursor(-10)
	if m.Cursor() != 0 {
		t.Fatal("negative cursor did not clamp")
	}
	m.SetCursor(999)
	if m.Cursor() != 1 {
		t.Fatal("oversized cursor did not clamp")
	}
	m.SetDocs(nil)
	if m.Cursor() != 0 || m.top != 0 {
		t.Fatalf("empty cursor %d top %d", m.Cursor(), m.top)
	}
	for _, key := range []string{"j", "G", "ctrl+d", "pgup", "h", "$"} {
		m.Update(keyMsg(key))
		assertDimensions(t, m)
	}
}

func TestHorizontalNavigationAndPageRefresh(t *testing.T) {
	m := New()
	m.SetDocs(testDocs(10))
	m.SetSize(20, 4)
	for _, key := range []string{"l", "right"} {
		m.Update(keyMsg(key))
	}
	if m.firstColumn != 2 {
		t.Fatalf("first column = %d", m.firstColumn)
	}
	m.SetDocs(testDocs(2))
	if m.firstColumn != 2 {
		t.Fatal("page refresh reset horizontal position")
	}
	m.Update(keyMsg("$"))
	if m.firstColumn != len(m.Columns())-1 {
		t.Fatal("$ did not jump to last column")
	}
	m.Update(keyMsg("l"))
	if m.firstColumn != len(m.Columns())-1 {
		t.Fatal("right did not clamp")
	}
	m.Update(keyMsg("h"))
	m.Update(keyMsg("left"))
	if m.firstColumn != len(m.Columns())-3 {
		t.Fatal("left navigation did not move by columns")
	}
	m.SetDocs([]bson.D{{{Key: "a", Value: 1}, {Key: "b", Value: 2}}})
	if m.firstColumn != 1 {
		t.Fatal("new page did not clamp horizontal offset")
	}
	m.Update(keyMsg("0"))
	m.Update(keyMsg("h"))
	if m.firstColumn != 0 {
		t.Fatal("left did not clamp at first column")
	}
}

func TestUnhandledKeysAndMessages(t *testing.T) {
	m := New()
	m.SetDocs(testDocs(10))
	m.SetSize(30, 5)
	m.SetCursor(4)
	for _, key := range []string{"enter", "space", "esc", "x", "ctrl+c", "/", ":", "?"} {
		if m.Handles(keyMsg(key)) {
			t.Fatalf("root action %q consumed", key)
		}
		if m.Update(keyMsg(key)) != nil {
			t.Fatalf("root action %q produced a command", key)
		}
	}
	if m.Update(tea.WindowSizeMsg{Width: 90, Height: 30}) != nil {
		t.Fatal("non-key message produced a command")
	}
	if m.Cursor() != 4 || m.width != 30 || m.height != 5 {
		t.Fatal("unhandled messages changed state")
	}
}

func TestCursorAndSelectionGutter(t *testing.T) {
	m := New()
	m.SetSize(20, 5)
	m.SetDocs([]bson.D{{{Key: "name", Value: "one"}}, {{Key: "name", Value: "two"}}})
	selected := map[int]bool{0: true, 1: true, 99: true}
	m.SetSelected(selected)
	selected[0] = false
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.HasPrefix(lines[1], "▎●") || !strings.HasPrefix(lines[2], " ●") {
		t.Fatalf("gutters = %q, %q", lines[1], lines[2])
	}
	m.SetCursor(1)
	lines = strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.HasPrefix(lines[1], " ●") || !strings.HasPrefix(lines[2], "▎●") {
		t.Fatalf("moved gutters = %q, %q", lines[1], lines[2])
	}
	m.SetSelected(nil)
	lines = strings.Split(ansi.Strip(m.View()), "\n")
	if strings.Contains(strings.Join(lines, "\n"), "●") {
		t.Fatal("selection was not cleared")
	}
}

func TestCursorRowStylesCoverEverySpan(t *testing.T) {
	old := theme.Current()
	t.Cleanup(func() { theme.Set(old) })
	for _, th := range []*theme.Theme{theme.New(theme.Dark()), theme.New(theme.Light()), theme.New(theme.Mono())} {
		theme.Set(th)
		m := New()
		m.SetSize(18, 3)
		m.SetDocs([]bson.D{{
			{Key: "a", Value: "one"},
			{Key: "b", Value: 12},
			{Key: "c", Value: nil},
		}})
		m.SetSelected(map[int]bool{0: true})
		expected := th.Accent.Inherit(th.CursorRow).Render("▎") +
			th.SelectMark.Inherit(th.CursorRow).Render("●") +
			th.Text.Inherit(th.CursorRow).Render("one") +
			th.Text.Inherit(th.CursorRow).Render("  ") +
			th.JSONNumber.Inherit(th.CursorRow).Render("12") +
			th.Text.Inherit(th.CursorRow).Render("  ") +
			th.JSONNull.Inherit(th.CursorRow).Render("null   ")
		lines := strings.Split(m.View(), "\n")
		if lines[1] != expected {
			t.Fatalf("cursor row lost a span background: got %q, want %q", lines[1], expected)
		}
		if lines[0] != th.Header.Render("  a    b   c      ") {
			t.Fatalf("header style = %q", lines[0])
		}
	}
}

func TestJSONMode(t *testing.T) {
	m := New()
	if m.Mode() != ModeTable {
		t.Fatal("default mode is not table")
	}
	docs := []bson.D{{
		{Key: "_id", Value: primitive.NewObjectID()},
		{Key: "name", Value: "hello\nworld"},
		{Key: "count", Value: int64(100)},
		{Key: "when", Value: primitive.NewDateTimeFromTime(time.Date(2025, 1, 23, 15, 4, 0, 0, time.UTC))},
		{Key: "ok", Value: true},
		{Key: "missing", Value: nil},
	}}
	m.SetDocs(docs)
	m.SetSize(300, 3)
	m.ToggleMode()
	if m.Mode() != ModeJSON || m.ColumnInfo() != "" {
		t.Fatal("JSON mode or column info incorrect")
	}
	expected := "▎ " + mjson.Compact(docs[0])
	line := strings.Split(ansi.Strip(m.View()), "\n")[0]
	if strings.TrimRight(line, " ") != expected {
		t.Fatalf("JSON = %q, want %q", line, expected)
	}
	if !strings.Contains(line, "ObjectId(") || !strings.Contains(line, "NumberLong(100)") || !strings.Contains(line, "ISODate(") {
		t.Fatal("JSON mode lost shell helpers")
	}
	assertDimensions(t, m)
	m.SetSize(24, 3)
	line = strings.Split(ansi.Strip(m.View()), "\n")[0]
	compact := mjson.Compact(docs[0])
	if got, want := line, "▎ "+ansi.Truncate(compact, 22, "…"); got != want {
		t.Fatalf("truncated JSON = %q, want %q", got, want)
	}
	m.Update(keyMsg("l"))
	m.Update(keyMsg("right"))
	if m.jsonOffset != 2 {
		t.Fatalf("JSON offset = %d, want 2 characters", m.jsonOffset)
	}
	line = strings.Split(ansi.Strip(m.View()), "\n")[0]
	if got, want := line, "▎ "+ansi.Truncate(compact[2:], 22, "…"); got != want {
		t.Fatalf("scrolled JSON = %q, want %q", got, want)
	}
	m.Update(keyMsg("$"))
	line = strings.Split(ansi.Strip(m.View()), "\n")[0]
	if got, want := line, "▎ "+compact[len(compact)-22:]; got != want {
		t.Fatalf("JSON end = %q, want %q", got, want)
	}
	if !strings.HasSuffix(line, "}") {
		t.Fatal("JSON end does not show the closing brace")
	}
	m.Update(keyMsg("0"))
	m.Update(keyMsg("h"))
	if m.jsonOffset != 0 {
		t.Fatal("JSON offset did not clamp")
	}
	m.Update(keyMsg("l"))
	m.ToggleMode()
	m.Update(keyMsg("l"))
	m.ToggleMode()
	if m.firstColumn != 1 || m.jsonOffset != 1 {
		t.Fatal("mode switch did not preserve independent positions")
	}
	m.SetDocs([]bson.D{{{Key: "a", Value: 1}}})
	if m.jsonOffset != 0 {
		t.Fatal("JSON offset did not clamp on shorter page")
	}
	assertDimensions(t, m)
}

func TestJSONStylesAndFullWidthCursorFill(t *testing.T) {
	m := New()
	m.SetSize(80, 2)
	m.SetDocs([]bson.D{{{Key: "a", Value: "x"}, {Key: "b", Value: int64(2)}, {Key: "c", Value: nil}}})
	m.ToggleMode()
	th := theme.Current()
	var expected strings.Builder
	expected.WriteString(th.Accent.Inherit(th.CursorRow).Render("▎"))
	expected.WriteString(th.SelectMark.Inherit(th.CursorRow).Render(" "))
	for _, token := range mjson.Tokens(m.docs[0], false) {
		expected.WriteString(tokenStyle(th, token.Kind).Inherit(th.CursorRow).Render(token.Text))
	}
	expected.WriteString(th.Text.Inherit(th.CursorRow).Render(strings.Repeat(" ", 78-m.jsonWidth)))
	if got := strings.Split(m.View(), "\n")[0]; got != expected.String() {
		t.Fatalf("JSON cursor span styles: got %q, want %q", got, expected.String())
	}
}

func TestJSONUnicodeClipping(t *testing.T) {
	m := New()
	m.SetDocs([]bson.D{{{Key: "unicode", Value: "日日本 👩‍💻 é 🇦🇷"}}})
	m.ToggleMode()
	for width := 1; width <= 30; width++ {
		m.SetSize(width, 3)
		m.Update(keyMsg("0"))
		for range m.jsonWidth + 2 {
			assertDimensions(t, m)
			m.Update(keyMsg("l"))
		}
	}
}

func TestJSONScrollingMovesWholeCharacters(t *testing.T) {
	m := New()
	m.SetDocs([]bson.D{{{Key: "u", Value: "日👩‍💻é"}}})
	m.SetSize(8, 2)
	m.ToggleMode()
	for range 7 {
		m.Update(keyMsg("l"))
	}
	if got := strings.Split(ansi.Strip(m.View()), "\n")[0]; got != "▎ 日👩‍💻é…" {
		t.Fatalf("Unicode starting position = %q", got)
	}
	m.Update(keyMsg("l"))
	if got := strings.Split(ansi.Strip(m.View()), "\n")[0]; got != "▎ 👩‍💻é\"} " {
		t.Fatalf("Unicode next character = %q", got)
	}
	m.Update(keyMsg("h"))
	if got := strings.Split(ansi.Strip(m.View()), "\n")[0]; got != "▎ 日👩‍💻é…" {
		t.Fatalf("Unicode previous character = %q", got)
	}
	assertDimensions(t, m)
}

func BenchmarkView500x30(b *testing.B) {
	docs := benchmarkDocs()
	for _, mode := range []Mode{ModeTable, ModeJSON} {
		b.Run(fmt.Sprint(mode), func(b *testing.B) {
			m := New()
			m.SetDocs(docs)
			m.SetSize(120, 25)
			if mode == ModeJSON {
				m.ToggleMode()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				m.View()
			}
		})
	}
}

func BenchmarkSetDocs500x30(b *testing.B) {
	docs := benchmarkDocs()
	m := New()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.SetDocs(docs)
	}
}

func benchmarkDocs() []bson.D {
	docs := make([]bson.D, 500)
	for i := range docs {
		docs[i] = make(bson.D, 30)
		docs[i][0] = bson.E{Key: "_id", Value: primitive.NewObjectID()}
		for j := 1; j < 30; j++ {
			docs[i][j] = bson.E{Key: fmt.Sprintf("field_%02d", j), Value: fmt.Sprintf("row_%03d_value_%02d", i, j)}
		}
	}
	return docs
}
