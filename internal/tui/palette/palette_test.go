package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
)

func key(code rune, text string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: text} }

func items() []Item {
	return []Item{
		{Kind: Command, Title: "limit", Hint: "Page size", Value: ":limit"},
		{Kind: Command, Title: "sort", Hint: "Sort fields", Value: ":sort"},
		{Kind: Command, Title: "use", Hint: "Database", Value: ":use"},
		{Kind: Command, Title: "killop", Hint: "Kill operation", Value: ":killop"},
		{Kind: Database, Title: "admin", Hint: "database", Value: "admin"},
		{Kind: Collection, Title: "orders", Hint: "500 docs", Value: "orders"},
		{Kind: Action, Title: "Refresh all", Hint: "reload", Value: ":refresh!"},
	}
}

func choose(t *testing.T, m *Model) ChooseMsg {
	t.Helper()
	cmd := m.Update(key(tea.KeyEnter, ""))
	if cmd == nil {
		t.Fatal("missing ChooseMsg")
	}
	msg, ok := cmd().(ChooseMsg)
	if !ok {
		t.Fatal("wrong message")
	}
	return msg
}

func TestFuzzyRankingAndRecentChoices(t *testing.T) {
	m := New(items())
	m.SetText("rfa")
	filtered := m.FilteredItems()
	if len(filtered) != 1 || filtered[0].Title != "Refresh all" {
		t.Fatal(filtered)
	}
	m.SetText("ord")
	if filtered = m.FilteredItems(); len(filtered) != 1 || filtered[0].Title != "orders" {
		t.Fatal(filtered)
	}
	m.SetText("ORDERs")
	if chosen := choose(t, m); chosen.Item == nil || chosen.Item.Title != "orders" {
		t.Fatal(chosen)
	}
	m.SetText("")
	if m.FilteredItems()[0].Title != "orders" {
		t.Fatal("recent choice not first")
	}
	m.Remember(items()[4])
	if m.FilteredItems()[0].Title != "admin" {
		t.Fatal("newest choice not first")
	}
	m.SetItems(append(items(), Item{Kind: Command, Title: "Limit documents", Value: ":other"}))
	m.SetText("limit")
	if m.FilteredItems()[0].Title != "limit" {
		t.Fatal("exact item was not ranked above fuzzy match")
	}
}

func TestCommandValuesAndRawCommandPassthrough(t *testing.T) {
	for _, text := range []string{"limit 20", "sort -ts", "use admin", "killop 123", ":limit 20", "unknown command"} {
		m := New(items())
		m.SetText(text)
		msg := choose(t, m)
		if msg.Item != nil || msg.Input != text {
			t.Fatalf("input %q: %#v", text, msg)
		}
	}
	m := New(append(items(), Item{Kind: Command, Title: "limit 20", Value: ":limit 20"}))
	m.SetText("limit 20")
	if chosen := choose(t, m); chosen.Item == nil || chosen.Item.Title != "limit 20" {
		t.Fatal("exact full-command item should win")
	}
	m.SetItems([]Item{{Kind: Command, Title: "Page size", Value: ":limit"}})
	m.SetText("limit")
	if chosen := choose(t, m); chosen.Item == nil || chosen.Item.Title != "Page size" {
		t.Fatal("command value was not searched")
	}
	m.SetItems(nil)
	m.SetText("")
	if m.Update(key(tea.KeyEnter, "")) != nil {
		t.Fatal("empty input must not execute")
	}
}

func TestMovementAndClose(t *testing.T) {
	m := New(items())
	m.Update(key(tea.KeyDown, ""))
	m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.SelectedItem().Title != "use" {
		t.Fatal("down navigation failed")
	}
	m.Update(key(tea.KeyUp, ""))
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.SelectedItem().Title != "limit" {
		t.Fatal("up navigation failed")
	}
	cmd := m.Update(key(tea.KeyEscape, ""))
	if _, ok := cmd().(CloseMsg); !ok {
		t.Fatal("escape did not close")
	}
	m.Update(key('s', "s"))
	if m.Text() != "s" || m.SelectedItem().Title != "sort" {
		t.Fatal("typing did not filter")
	}
}

func TestCompleteBoxDimensions(t *testing.T) {
	data := items()
	data = append(data, Item{Kind: Collection, Title: "界👩‍💻界orders", Hint: "界界界many documents", Value: "界orders"})
	m := New(data)
	for _, w := range []int{0, 1, 4, 5, 8, 9, 10, 12, 20, 40, 80, 200} {
		for _, h := range []int{0, 2, 3, 5, 6, 10, 30} {
			m.SetText("界👩‍💻")
			m.SetSize(w, h)
			bw, bh := m.Dimensions()
			if bw > min(72, max(0, w-4)) || bh > 14 {
				t.Fatal("box exceeds limits")
			}
			view := m.View()
			if bh == 0 {
				if view != "" {
					t.Fatal(view)
				}
				continue
			}
			rows := strings.Split(view, "\n")
			if len(rows) != bh {
				t.Fatalf("screen %dx%d: height=%d want=%d", w, h, len(rows), bh)
			}
			for _, row := range rows {
				if got := ansi.StringWidth(row); got != bw {
					t.Fatalf("screen %dx%d: width=%d want=%d: %q", w, h, got, bw, row)
				}
			}
		}
	}
}

func TestUnicodeMatchesAndCursorStyling(t *testing.T) {
	m := New([]Item{{Kind: Collection, Title: "界orders", Hint: "many", Value: "orders"}})
	m.SetText("or")
	res := m.results[0]
	if len(res.matched) != 2 || res.matched[0] != len("界") {
		t.Fatal("fuzzy uses byte indexes; test fixture changed")
	}
	row := m.row(res, true, 30)
	tm := theme.Current()
	if !strings.Contains(row, tm.AccentBold.Inherit(tm.CursorRow).Render("o")) {
		t.Fatalf("matched unicode byte position not highlighted: %q", row)
	}
	if !strings.Contains(row, tm.Muted.Inherit(tm.CursorRow).Render("many")) {
		t.Fatalf("hint punched a hole in cursor fill: %q", row)
	}
	if ansi.StringWidth(row) != 30 {
		t.Fatal("cursor row not fully padded")
	}
}

func TestScrollKeepsCursorVisible(t *testing.T) {
	m := New(items())
	m.SetSize(50, 8)
	for range 6 {
		m.Update(key(tea.KeyDown, ""))
	}
	if m.top == 0 || !strings.Contains(ansi.Strip(m.View()), "Refresh all") {
		t.Fatal("cursor scrolled off screen")
	}
}
