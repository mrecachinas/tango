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
		m.SetSize(40, 24)
		w, h := m.Dimensions()
		dimensions(t, m.View(), w, h)
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
		m.SetSize(90, 24)
		if cmd := m.Update(key(tea.KeyEnter, "")); cmd != nil || !m.invalid || m.done {
			t.Fatalf("%q unexpectedly confirmed", text)
		}
		w, h := m.Dimensions()
		dimensions(t, m.View(), w, h)
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

func TestDialogDimensionsAndLayout(t *testing.T) {
	confirm := NewConfirm("id", "界界👩‍💻delete documents?")
	typed := NewTypeConfirm("id", "界界👩‍💻 Type orders", "orders")
	typed.SetText("orders")
	for _, w := range []int{0, 1, 2, 3, 4, 8, 20, 60, 100} {
		for _, h := range []int{0, 1, 4, 8, 24} {
			confirm.SetSize(w, h)
			typed.SetSize(w, h)
			cw, ch := confirm.Dimensions()
			tw, th := typed.Dimensions()
			dimensions(t, confirm.View(), cw, ch)
			dimensions(t, typed.View(), tw, th)
			if cw > min(64, max(0, w-8)) || tw > min(64, max(0, w-8)) ||
				ch > max(0, h-4) || th > max(0, h-4) {
				t.Fatal("dialog exceeded its screen margins")
			}
		}
	}
	confirm = NewConfirm("delete", `Delete this document from sample_app.events? _id: ObjectId("012345678901234567890123")`)
	confirm.SetSize(150, 38)
	view := ansi.Strip(confirm.View())
	if !strings.Contains(view, "Delete document") || !strings.Contains(view, "y delete · n/esc cancel") ||
		!strings.HasPrefix(view, "╭") || !strings.Contains(view, "sample_app.events") {
		t.Fatalf("bad compact confirm: %q", view)
	}
	if !strings.Contains(confirm.View(), "38;2;255;107;107") {
		t.Fatal("destructive dialog border must use Err")
	}
	typed = NewTypeConfirm("drop", "Drop collection 'sample_app.orders'? This cannot be undone!", "orders")
	typed.SetSize(150, 38)
	view = ansi.Strip(typed.View())
	if !strings.Contains(view, "Drop collection") || !strings.Contains(view, "Type orders to confirm") ||
		!strings.Contains(view, "▏orders") || !strings.Contains(view, "enter drop · esc cancel") {
		t.Fatalf("bad type dialog: %q", view)
	}
	if strings.Contains(view, "✓") {
		t.Fatal("empty input must not show a match")
	}
	typed.SetText("orders")
	if !strings.Contains(ansi.Strip(typed.View()), "✓") {
		t.Fatal("exact input must show a live checkmark")
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
	if !m.Filtering() {
		t.Fatal("empty explicit filter mode must be visible to the root")
	}
	m.Update(key('j', "j"))
	if m.Filter() != "j" {
		t.Fatal("explicit filter mode swallowed navigation-like input")
	}
	m.Update(key(tea.KeyEnter, ""))
	if m.Filtering() {
		t.Fatal("enter must leave filter mode")
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

func TestHelpLongKeysAndGhostInput(t *testing.T) {
	keys := ":export json|csv [all] [path]"
	rows := sectionRows([]Section{{Title: "Commands", Bindings: []Binding{
		{Keys: keys, Desc: "Export every matching document to the chosen destination without losing syntax."},
	}}}, 50)
	plain := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(plain, keys) || strings.Contains(plain, "…") {
		t.Fatalf("help keys must not truncate: %q", plain)
	}
	if !strings.HasPrefix(ansi.Strip(rows[2]), strings.Repeat(" ", 26)) {
		t.Fatal("long-key description must have a hanging indent")
	}
	m := NewHelp(helpSections())
	m.SetSize(150, 38)
	w, h := m.Dimensions()
	if w > 110 || h > 34 {
		t.Fatal("help exceeds width/margins")
	}
	if !strings.Contains(ansi.Strip(m.View()), "/ filter ▏keys or descriptions") {
		t.Fatal("cursor must precede the ghost placeholder")
	}
	m.SetSize(90, 24)
	if strings.Contains(ansi.Strip(m.lines[0]), "Indexes") {
		t.Fatal("narrow help must use one scrollable column")
	}
	base := fit("base", 150, 38)
	placed := strings.Split(ansi.Strip(Place(base, m.View(), 150, 38)), "\n")
	if strings.Contains(placed[0], "╭") || strings.Contains(placed[1], "╭") {
		t.Fatal("help collided with the screen's top border")
	}
}
