package sidebar

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
	"tango/internal/theme/themetest"
)

func press(s string) tea.KeyPressMsg {
	codes := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "backspace": tea.KeyBackspace,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"tab": tea.KeyTab,
	}
	if code, ok := codes[s]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	if strings.HasPrefix(s, "ctrl+") {
		return tea.KeyPressMsg{Code: []rune(strings.TrimPrefix(s, "ctrl+"))[0], Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func fixture() *Model {
	m := New()
	m.SetSize(22, 8)
	m.SetDatabases([]Entry{{Name: "sample_app", Info: "3"}, {Name: "inventory", Info: "2"}})
	m.SetCollections("sample_app", []Entry{{Name: "events", Info: "500"}, {Name: "users", Info: "120"}, {Name: "sessions", Info: "10"}})
	m.SetCollections("inventory", []Entry{{Name: "items", Info: "20"}, {Name: "warehouses", Info: "5"}})
	return m
}

func selected(t *testing.T, m *Model, db, coll string) {
	t.Helper()
	gotDB, gotColl := m.Selected()
	if gotDB != db || gotColl != coll {
		t.Fatalf("Selected() = (%q, %q), want (%q, %q)", gotDB, gotColl, db, coll)
	}
}

func TestTreeNavigationAndEvents(t *testing.T) {
	m := fixture()
	if len(m.Rows()) != 2 {
		t.Fatal("databases should start collapsed")
	}
	cmd := m.Update(press("enter"))
	if cmd == nil || cmd() != (SelectDBMsg{DB: "sample_app"}) {
		t.Fatal("enter database must select it")
	}
	if len(m.Rows()) != 5 || !m.Rows()[0].Expanded {
		t.Fatal("selecting database must expand its children")
	}
	m.Update(press("j"))
	selected(t, m, "sample_app", "events")
	cmd = m.Update(press("right"))
	if cmd == nil || cmd() != (OpenCollMsg{DB: "sample_app", Coll: "events"}) {
		t.Fatal("right on collection must open it")
	}
	m.Update(press("left"))
	selected(t, m, "sample_app", "")
	m.Update(press("h"))
	if len(m.Rows()) != 2 {
		t.Fatal("h on expanded database must collapse it")
	}
	cmd = m.Update(press("esc"))
	if cmd == nil || cmd() != (BackMsg{}) {
		t.Fatal("esc without filter must go back")
	}
	m.SelectPath("inventory", "warehouses")
	selected(t, m, "inventory", "warehouses")
	if !m.Rows()[1].Expanded {
		t.Fatal("SelectPath collection must expand its parent")
	}
	m.Collapse("inventory")
	selected(t, m, "inventory", "")
}

func TestLoadingAndRefreshSelection(t *testing.T) {
	m := fixture()
	m.Expand("sample_app")
	m.SetLoading("sample_app", true)
	rows := m.Rows()
	if !rows[4].Loading || rows[4].Name != "loading…" {
		t.Fatalf("loading placeholder missing: %+v", rows)
	}
	m.SelectPath("sample_app", "sessions")
	m.Update(press("down"))
	selected(t, m, "inventory", "")
	m.Update(press("up"))
	selected(t, m, "sample_app", "sessions")
	m.SetCollections("sample_app", []Entry{{Name: "new", Info: "1"}, {Name: "sessions", Info: "99"}})
	selected(t, m, "sample_app", "sessions")
	m.SetCollections("sample_app", []Entry{{Name: "new", Info: "1"}})
	selected(t, m, "sample_app", "")
	m.SetLoading("sample_app", false)
	for _, row := range m.Rows() {
		if row.Loading {
			t.Fatal("loading placeholder must disappear")
		}
	}
	m.SetDatabases(nil)
	selected(t, m, "", "")
	if m.Update(press("enter")) != nil {
		t.Fatal("empty list must not emit selection")
	}
}

func TestFilterTypingOwnsPrintableKeys(t *testing.T) {
	m := fixture()
	m.Update(press("/"))
	if !m.Filtering() || m.FilterText() != "" {
		t.Fatal("slash must start explicit empty filter")
	}
	for _, text := range []string{"q", "W", "j", "G", "l", "h", "/", " ", "é", "🚀"} {
		msg := press(text)
		if !m.Handles(msg) {
			t.Fatalf("filter must own %q", text)
		}
		if cmd := m.Update(msg); cmd != nil {
			t.Fatalf("filter text %q must not emit an event", text)
		}
	}
	if got := m.FilterText(); got != "qWjGlh/ é🚀" {
		t.Fatalf("FilterText() = %q", got)
	}
	if !m.Handles(press("tab")) {
		t.Fatal("typing mode must prevent root stealing keys")
	}
	m.Update(press("enter"))
	if m.Filtering() || m.FilterText() != "qWjGlh/ é🚀" {
		t.Fatal("enter must confirm without clearing text")
	}
	if cmd := m.Update(press("esc")); cmd != nil || m.Filtering() || m.FilterText() != "" {
		t.Fatal("first esc must clear confirmed filter, not go back")
	}
	if cmd := m.Update(press("esc")); cmd == nil {
		t.Fatal("second esc must go back")
	}
}

func TestImplicitFilteringAndBackspace(t *testing.T) {
	m := fixture()
	if m.Handles(press(" ")) {
		t.Fatal("space must not implicitly start filtering")
	}
	m.Update(press(" "))
	if m.Filtering() {
		t.Fatal("space outside filtering must be ignored")
	}
	m.Update(press("é"))
	if !m.Filtering() || m.FilterText() != "é" {
		t.Fatal("non-ASCII printable key must implicitly start filtering")
	}
	m.Update(press("🚀"))
	m.Update(press("backspace"))
	if m.FilterText() != "é" {
		t.Fatal("backspace must delete one rune")
	}
	m.Update(press("backspace"))
	if m.FilterText() != "" || !m.Filtering() {
		t.Fatal("deleting the last rune should retain typing mode, matching existing semantics")
	}
	m.Update(press("backspace"))
	if m.Filtering() {
		t.Fatal("backspace on empty filter should exit")
	}
	m.Update(press("ctrl+d"))
	if m.Filtering() {
		t.Fatal("control shortcuts are not printable")
	}
	m.Update(press("q"))
	if !m.Filtering() || m.FilterText() != "q" {
		t.Fatal("q must become filter text outside typing mode")
	}
}

func TestFuzzyFilterBothLevelsAndUnicodeIndexes(t *testing.T) {
	m := fixture()
	m.Update(press("/"))
	m.Update(press("ev"))
	rows := m.Rows()
	if len(rows) != 2 || rows[0].DB != "sample_app" || !rows[0].Expanded ||
		rows[1].Coll != "events" || !reflect.DeepEqual(rows[1].MatchedIndexes, []int{0, 1}) {
		t.Fatalf("collection fuzzy match should include its parent: %+v", rows)
	}
	m.Update(press("enter"))
	m.Update(press("j"))
	selected(t, m, "sample_app", "events")
	if cmd := m.Update(press("enter")); cmd == nil {
		t.Fatal("confirmed filter must permit collection opening")
	}
	m.Update(press("esc"))
	m.Update(press("/"))
	m.Update(press("sap"))
	rows = m.Rows()
	if len(rows) != 1 || rows[0].Name != "sample_app" || len(rows[0].MatchedIndexes) != 3 {
		t.Fatalf("database fuzzy match failed: %+v", rows)
	}
	m.Update(press("esc"))
	m.SetDatabases([]Entry{{Name: "é文🚀base", Info: "2"}})
	m.Update(press("/"))
	m.Update(press("文b"))
	rows = m.Rows()
	if len(rows) != 1 || !reflect.DeepEqual(rows[0].MatchedIndexes, []int{1, 3}) {
		t.Fatalf("match indexes must be rune offsets: %+v", rows)
	}
	m.Update(press("z"))
	if len(m.Rows()) != 0 || !strings.Contains(ansi.Strip(m.View()), "No matches") {
		t.Fatal("unmatched filter must render empty state")
	}
}

func TestNavigationKeysAndScrolling(t *testing.T) {
	cases := []struct {
		key         string
		start, want int
	}{
		{"j", 5, 6}, {"down", 5, 6}, {"k", 5, 4}, {"up", 5, 4},
		{"g", 5, 0}, {"home", 5, 0}, {"G", 5, 19}, {"end", 5, 19},
		{"ctrl+d", 5, 7}, {"ctrl+u", 5, 3}, {"pgdown", 5, 9}, {"pgup", 5, 1},
		{"k", 0, 0}, {"j", 19, 19},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%d", tc.key, tc.start), func(t *testing.T) {
			m := New()
			var entries []Entry
			for i := 0; i < 20; i++ {
				entries = append(entries, Entry{Name: fmt.Sprintf("database-%02d", i)})
			}
			m.SetDatabases(entries)
			m.SetSize(20, 4)
			m.SelectPath(entries[tc.start].Name, "")
			if !m.Handles(press(tc.key)) {
				t.Fatal("navigation key must be consumed")
			}
			m.Update(press(tc.key))
			selected(t, m, entries[tc.want].Name, "")
			if m.cursor < m.offset || m.cursor >= m.offset+m.height {
				t.Fatal("selected row must stay visible")
			}
		})
	}
}

func TestRowsAndInputAreCopied(t *testing.T) {
	m := New()
	entries := []Entry{{Name: "alpha"}}
	m.SetDatabases(entries)
	entries[0].Name = "changed"
	m.Update(press("/"))
	m.Update(press("a"))
	rows := m.Rows()
	rows[0].Name = "changed"
	rows[0].MatchedIndexes[0] = 99
	if got := m.Rows()[0]; got.Name != "alpha" || got.MatchedIndexes[0] == 99 {
		t.Fatal("data and Rows snapshots must not alias internal state")
	}
}

func TestSidebarRender(t *testing.T) {
	m := fixture()
	m.Expand("sample_app")
	m.SetSize(18, 6)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.HasPrefix(lines[0], "▾ sample_app") || !strings.HasSuffix(lines[0], "3") ||
		!strings.HasPrefix(lines[1], "    events") || !strings.HasSuffix(lines[1], "500") {
		t.Fatalf("unexpected tree rendering: %q", lines)
	}
	m.SetDatabases([]Entry{{Name: "very-long-database-name", Info: "12345"}})
	m.SetSize(12, 3)
	line := strings.Split(ansi.Strip(m.View()), "\n")[0]
	if !strings.Contains(line, "…") || !strings.HasSuffix(line, "12345") {
		t.Fatalf("count must be retained while name truncates: %q", line)
	}
	m.Update(press("/"))
	m.Update(press("very-long-database-name"))
	last := strings.Split(ansi.Strip(m.View()), "\n")[2]
	if !strings.HasPrefix(last, "/ ") || !strings.HasSuffix(last, "▏") {
		t.Fatalf("filter prompt and insertion cursor must remain visible: %q", last)
	}
}

func TestHandlesAndCustomKeyMap(t *testing.T) {
	m := fixture()
	if m.Handles(press("ctrl+x")) || m.Handles(press("tab")) ||
		m.Handles(tea.KeyPressMsg{Code: 'x', Mod: tea.ModAlt}) {
		t.Fatal("unbound non-printable/modifier keys belong to root")
	}
	m.Keys.Down = key.NewBinding(key.WithKeys("ctrl+x"))
	if !m.Handles(press("ctrl+x")) {
		t.Fatal("custom binding must be exposed to root")
	}
	m.Update(press("ctrl+x"))
	selected(t, m, "inventory", "")
	if cmd := m.Update(tea.WindowSizeMsg{Width: 20, Height: 10}); cmd != nil {
		t.Fatal("non-key messages must be ignored")
	}
}

func TestDimensionsAcrossThemesAndTinySizes(t *testing.T) {
	old := theme.Current()
	t.Cleanup(func() { theme.Set(old) })
	for _, palette := range []theme.Palette{theme.Dark(), theme.Light(), theme.Mono()} {
		theme.Set(theme.New(palette))
		for state := 0; state < 4; state++ {
			m := fixture()
			m.SetDatabases([]Entry{{Name: "文🚀é長い名前", Info: "123456789"}, {Name: "bad\nname\x1b[31m", Info: "1\t2"}, {Name: "👩‍💻🇺🇸e\u0301", Info: "2"}})
			m.SetCollections("文🚀é長い名前", []Entry{{Name: "文🚀e\u0301", Info: "500"}})
			m.Expand("文🚀é長い名前")
			m.SetLoading("文🚀é長い名前", true)
			m.SetActive("👩‍💻🇺🇸e\u0301", "")
			switch state {
			case 1:
				m.Update(press("/"))
				m.Update(press("文🚀"))
			case 2:
				m.SetDatabases(nil)
			case 3:
				m.Update(press("/"))
				m.Update(press("not-found"))
				m.Update(press("enter"))
			}
			for w := 0; w <= 30; w++ {
				for h := 0; h <= 8; h++ {
					m.SetSize(w, h)
					assertDimensions(t, m.View(), w, h)
				}
			}
		}
	}
	m := New()
	m.SetSize(-1, -1)
	if m.View() != "" {
		t.Fatal("negative dimensions should be clamped")
	}
}

func assertDimensions(t *testing.T, view string, w, h int) {
	t.Helper()
	if h == 0 {
		if view != "" {
			t.Fatalf("zero height: got %q", view)
		}
		return
	}
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Fatalf("size %dx%d: %d lines", w, h, len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != w {
			t.Fatalf("size %dx%d row %d: width %d (%q)", w, h, i, got, line)
		}
		if got := ansi.StringWidth(ansi.Strip(line)); got != w {
			t.Fatalf("size %dx%d row %d: stripped width %d (%q)", w, h, i, got, line)
		}
	}
}

func TestCursorAndActiveStyles(t *testing.T) {
	old := theme.Current()
	t.Cleanup(func() { theme.Set(old) })
	theme.Set(theme.New(theme.Dark()))
	m := fixture()
	m.SetFocused(true)
	m.SetActive("sample_app", "events")
	m.Expand("sample_app")
	m.SelectPath("sample_app", "events")
	tokens := strings.Split(m.View(), "\n")
	if !strings.Contains(tokens[1], "e") || !strings.Contains(tokens[1], "\x1b[48;2;43;48;56m") {
		t.Fatal("active collection characters must retain accent/bold and cursor background")
	}
	if rowStyle(theme.Current().Muted, true).GetBackground() != theme.Current().CursorRow.GetBackground() {
		t.Fatal("muted count must carry cursor background")
	}
	themetest.Cursor(t, tokens[1], m.width)
}

func TestUnfocusedActiveAndDimEntries(t *testing.T) {
	m := New()
	m.SetSize(24, 8)
	m.SetDatabases([]Entry{{Name: "admin", Info: "2"}, {Name: "config"}, {Name: "local"},
		{Name: "custom", Dim: true}, {Name: "sample_app", Info: "1"}})
	m.SetCollections("sample_app", []Entry{{Name: "events", Info: "500"}})
	m.SetActive("sample_app", "events")
	m.SelectPath("sample_app", "events")
	for _, row := range m.Rows()[:4] {
		if !row.Dim {
			t.Fatalf("database %q must dim", row.Name)
		}
	}
	lines := strings.Split(m.View(), "\n")
	fill := theme.Current().CursorRow.Render(" ")
	prefix := fill[:strings.IndexByte(fill, ' ')]
	if strings.Contains(m.View(), prefix) {
		t.Fatal("unfocused sidebar must not paint cursor background")
	}
	active := lines[5]
	if !strings.Contains(active, theme.Current().AccentBold.Render("e")) ||
		!strings.Contains(active, theme.Current().Muted.Render("500")) {
		t.Fatal("active name must accent, count must remain muted")
	}
	m.SetFocused(true)
	themetest.Cursor(t, strings.Split(m.View(), "\n")[5], m.width)
}
