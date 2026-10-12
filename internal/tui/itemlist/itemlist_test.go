package itemlist

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
)

func press(s string) tea.KeyPressMsg {
	codes := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "up": tea.KeyUp, "down": tea.KeyDown,
		"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
	}
	if code, ok := codes[s]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	if strings.HasPrefix(s, "ctrl+") {
		return tea.KeyPressMsg{Code: []rune(s)[5], Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func TestNavigation(t *testing.T) {
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
			items := make([]Item, 20)
			for i := range items {
				items[i].Title = fmt.Sprintf("index-%02d", i)
			}
			m.SetItems(items)
			m.SetSize(20, 4)
			m.SetCursor(tc.start)
			if !m.Handles(press(tc.key)) {
				t.Fatal("must handle navigation binding")
			}
			m.Update(press(tc.key))
			if m.Cursor() != tc.want || m.cursor < m.offset || m.cursor >= m.offset+m.height {
				t.Fatalf("cursor %d, offset %d, want %d visible", m.Cursor(), m.offset, tc.want)
			}
		})
	}
}

func TestEventsClampingAndEmpty(t *testing.T) {
	m := New()
	if m.Update(press("enter")) != nil {
		t.Fatal("empty list must not emit selection")
	}
	m.SetItems([]Item{{Title: "one"}, {Title: "two"}})
	m.SetCursor(100)
	cmd := m.Update(press("enter"))
	if cmd == nil || cmd() != (SelectMsg{Index: 1}) {
		t.Fatal("enter must emit selected index")
	}
	m.SetCursor(-100)
	if m.Cursor() != 0 {
		t.Fatal("cursor should clamp at zero")
	}
	m.SetCursor(1)
	m.SetItems([]Item{{Title: "one"}})
	if m.Cursor() != 0 {
		t.Fatal("item refresh should clamp cursor")
	}
	m.SetItems(nil)
	m.SetSize(20, 3)
	m.SetEmptyText("No indexes")
	if !strings.Contains(ansi.Strip(m.View()), "No indexes") {
		t.Fatal("empty text must be configurable")
	}
	cmd = m.Update(press("esc"))
	if cmd == nil || cmd() != (BackMsg{}) {
		t.Fatal("escape must emit BackMsg")
	}
}

func TestRenderingCopyAndKeyRouting(t *testing.T) {
	m := New()
	items := []Item{{Title: "index", Detail: "unique: true"}}
	m.SetItems(items)
	items[0].Title = "mutated"
	m.SetSize(30, 2)
	if got := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.HasPrefix(got, "index  unique: true") {
		t.Fatalf("title/detail must be on same line and input copied: %q", got)
	}
	m.SetItems([]Item{{Title: "very-long-index-name", Detail: "very-long-index-detail"}})
	m.SetSize(12, 2)
	if got := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.Contains(got, "…") {
		t.Fatalf("long fields should truncate: %q", got)
	}
	if m.Handles(press("q")) || m.Update("not a key") != nil {
		t.Fatal("unbound keys and other messages belong to root")
	}
	m.Keys.Back = key.NewBinding(key.WithKeys("q"))
	if !m.Handles(press("q")) || m.Update(press("q")) == nil {
		t.Fatal("custom bindings must govern routing and update")
	}
}

func TestDimensionsAndCursorBackground(t *testing.T) {
	old := theme.Current()
	t.Cleanup(func() { theme.Set(old) })
	for _, palette := range []theme.Palette{theme.Dark(), theme.Light(), theme.Mono()} {
		theme.Set(theme.New(palette))
		for _, items := range [][]Item{
			nil,
			{{Title: "文🚀e\u0301index", Detail: "🔑 unique"}, {Title: "👩‍💻🇺🇸e\u0301", Detail: "emoji"}},
			{{Title: "line\nbreak\x1b[31m", Detail: "tab\tdetail"}, {Title: "second"}},
		} {
			m := New()
			m.SetItems(items)
			for w := 0; w <= 30; w++ {
				for h := 0; h <= 6; h++ {
					m.SetSize(w, h)
					view := m.View()
					if h == 0 {
						if view != "" {
							t.Fatal("zero height must render nothing")
						}
						continue
					}
					lines := strings.Split(view, "\n")
					if len(lines) != h {
						t.Fatalf("size %dx%d got %d lines", w, h, len(lines))
					}
					for _, line := range lines {
						if ansi.StringWidth(line) != w || ansi.StringWidth(ansi.Strip(line)) != w {
							t.Fatalf("size %dx%d got line %q", w, h, line)
						}
					}
				}
			}
		}
		if rowStyle(theme.Current().Muted, true).GetBackground() != theme.Current().CursorRow.GetBackground() {
			t.Fatal("styled detail must carry full cursor background")
		}
	}
	m := New()
	m.SetSize(-2, -3)
	if m.View() != "" {
		t.Fatal("negative dimensions must clamp")
	}
}
