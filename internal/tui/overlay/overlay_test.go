package overlay

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
)

func key(code rune, text string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: text} }

func dimensions(t *testing.T, view string, w, h int) {
	t.Helper()
	if h == 0 {
		if view != "" {
			t.Fatal(view)
		}
		return
	}
	rows := strings.Split(view, "\n")
	if len(rows) != h {
		t.Fatalf("height=%d want=%d: %q", len(rows), h, view)
	}
	for _, row := range rows {
		if got := ansi.StringWidth(row); got != w {
			t.Fatalf("width=%d want=%d: %q", got, w, row)
		}
	}
}

func TestConfirmSemantics(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{
		key('y', "y"), key('n', "n"), key('Y', "Y"), key('x', "x"),
		key(tea.KeyEscape, ""), key(tea.KeyEnter, ""), key(tea.KeyDown, ""),
	} {
		m := NewConfirm("delete", "Delete 3 documents?")
		m.SetSize(40, 1)
		dimensions(t, m.View(), 40, 1)
		if m.Update(tea.WindowSizeMsg{Width: 40, Height: 1}) != nil {
			t.Fatal("non-key message made a choice")
		}
		cmd := m.Update(k)
		if cmd == nil {
			t.Fatal("missing confirmation event")
		}
		msg := cmd()
		if k.String() == "y" {
			if event, ok := msg.(ConfirmedMsg); !ok || event.ID != "delete" {
				t.Fatal(msg)
			}
		} else if event, ok := msg.(CancelledMsg); !ok || event.ID != "delete" {
			t.Fatal(msg)
		}
		if m.Update(k) != nil || m.Handles(k) {
			t.Fatal("confirmation emitted more than once")
		}
	}
}

func TestTypeConfirmRequiresExactInput(t *testing.T) {
	for _, text := range []string{"", "Orders", "orders ", " orders", "ORDERs", "wrong"} {
		m := NewTypeConfirm("drop", "Type orders to drop sample_app.orders", "orders")
		m.SetText(text)
		m.SetSize(90, 1)
		if cmd := m.Update(key(tea.KeyEnter, "")); cmd != nil || !m.invalid || m.done {
			t.Fatalf("%q unexpectedly confirmed", text)
		}
		dimensions(t, m.View(), 90, 1)
		m.SetText("orders")
		if event, ok := m.Update(key(tea.KeyEnter, ""))().(ConfirmedMsg); !ok || event.ID != "drop" {
			t.Fatal("exact text did not confirm")
		}
		if m.Update(key(tea.KeyEnter, "")) != nil {
			t.Fatal("emitted a second event")
		}
	}
	m := NewTypeConfirm("drop", "Type orders", "orders")
	cmd := m.Update(key(tea.KeyEscape, ""))
	if event, ok := cmd().(CancelledMsg); !ok || event.ID != "drop" {
		t.Fatal("escape failed to cancel")
	}
	m = NewTypeConfirm("drop", "Type x", "x")
	m.Update(key('x', "x"))
	if m.Text() != "x" {
		t.Fatal("textinput did not accept input")
	}
}

func TestInlineDimensions(t *testing.T) {
	confirm := NewConfirm("id", "界界👩‍💻delete documents?")
	typed := NewTypeConfirm("id", "界界👩‍💻 Type orders", "orders")
	typed.SetText("orders")
	for _, w := range []int{0, 1, 2, 3, 4, 8, 20, 60, 100} {
		confirm.SetSize(w, 1)
		typed.SetSize(w, 1)
		dimensions(t, confirm.View(), w, 1)
		dimensions(t, typed.View(), w, 1)
		if w >= 4 && !strings.HasSuffix(strings.TrimRight(ansi.Strip(confirm.View()), " "), "y/N") {
			t.Fatal("question hid confirmation keys")
		}
	}
}

func helpSections() []Section {
	return []Section{
		{Title: "Global", Bindings: []Binding{{"tab", "Switch panels"}, {"/", "Query the collection"}, {"?", "Help"}}},
		{Title: "Documents", Bindings: []Binding{{"enter", "Open document"}, {"space", "Select document"}, {"d", "Delete selection"}}},
		{Title: "Indexes", Bindings: []Binding{{"X", "Drop index"}, {"esc", "Back"}}},
		{Title: "Stats", Bindings: []Binding{{"r", "Refresh server profiler"}}},
	}
}

func TestHelpFilterAndColumns(t *testing.T) {
	m := NewHelp(helpSections())
	m.SetSize(110, 22)
	if got := ansi.Strip(m.View()); !strings.Contains(got, "Global") || !strings.Contains(got, "Indexes") {
		t.Fatal("sections missing")
	}
	// Both sides must share rows rather than one long sequential list.
	if got := ansi.Strip(m.lines[0]); !strings.Contains(got, "Global") || !strings.Contains(got, "Indexes") {
		t.Fatalf("wide view did not use columns: %q", got)
	}
	m.SetFilter("DELETE")
	if len(m.filtered()) != 1 || m.filtered()[0].Title != "Documents" || len(m.filtered()[0].Bindings) != 1 {
		t.Fatal("description filter failed")
	}
	if got := ansi.Strip(strings.Join(m.lines, "\n")); strings.Contains(got, "Global") || !strings.Contains(got, "Delete selection") {
		t.Fatal(got)
	}
	m.SetFilter("indexes")
	if len(m.filtered()[0].Bindings) != 2 {
		t.Fatal("section title should match all bindings")
	}
	m.SetFilter("space")
	if len(m.filtered()) != 1 || m.filtered()[0].Bindings[0].Keys != "space" {
		t.Fatal("key filter failed")
	}
	m.SetFilter("no such binding")
	if !strings.Contains(ansi.Strip(m.View()), "No matching keys") {
		t.Fatal("empty state missing")
	}
	m.SetFilter("")
	m.Update(key('d', "d"))
	if m.Filter() != "d" || !m.filtering {
		t.Fatal("typing did not start filtering")
	}
	m.Update(key(tea.KeyEscape, ""))
	if m.Filter() != "" || m.filtering {
		t.Fatal("escape did not clear filter")
	}
	if _, ok := m.Update(key(tea.KeyEscape, ""))().(CloseMsg); !ok {
		t.Fatal("second escape did not close")
	}
}

func TestHelpScrollAndDimensions(t *testing.T) {
	sections := helpSections()
	for i := range 25 {
		sections[0].Bindings = append(sections[0].Bindings, Binding{Keys: fmt.Sprint(i), Desc: "界界 a much longer help description that wraps"})
	}
	m := NewHelp(sections)
	for _, w := range []int{0, 4, 5, 8, 9, 10, 15, 40, 79, 80, 110} {
		for _, h := range []int{0, 2, 3, 6, 12, 25} {
			m.SetSize(w, h)
			bw, bh := m.Dimensions()
			dimensions(t, m.View(), bw, bh)
		}
	}
	m.SetSize(55, 12)
	m.Update(key('G', "G"))
	if m.Offset() == 0 {
		t.Fatal("did not scroll to end")
	}
	m.Update(key('g', "g"))
	if m.Offset() != 0 {
		t.Fatal("did not scroll to start")
	}
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if m.Offset() == 0 {
		t.Fatal("half-page scroll failed")
	}
	m.Update(key('/', "/"))
	m.Update(key('j', "j"))
	if m.Filter() != "j" {
		t.Fatal("explicit filter mode swallowed navigation-like input")
	}
}

func TestPlaceCentersAndPreservesBase(t *testing.T) {
	got := ansi.Strip(Place("abcdefgh\nabcdefgh\nabcdefgh\nabcdefgh", "XY\nZZ", 8, 4))
	want := "abcdefgh\nabcXYfgh\nabcZZfgh\nabcdefgh"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = ansi.Strip(Place("abcdefgh\nabcdefgh\nabcdefgh", "XY\nZ", 8, 3))
	if !strings.Contains(got, "abcZ fgh") {
		t.Fatal("ragged overlay did not clear its rectangle")
	}
	base := theme.Current().Warn.Render("abcdefgh")
	got = Place(base, theme.Current().Accent.Render("X"), 8, 1)
	if !strings.Contains(got, "\x1b") || ansi.Strip(got) != "abcXefgh" {
		t.Fatal("compositing lost styling or base")
	}
}

func TestPlaceSizesAndOversizedBox(t *testing.T) {
	for _, w := range []int{0, 1, 2, 5, 10} {
		for _, h := range []int{0, 1, 3, 6} {
			view := Place("界界base\n界base", "界界界界界\noversized\nbox\nlast", w, h)
			dimensions(t, view, w, h)
			dimensions(t, Place("", "", w, h), w, h)
		}
	}
}
