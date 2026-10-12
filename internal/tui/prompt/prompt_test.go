package prompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func key(code rune, text string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Text: text} }

func change(t *testing.T, cmd tea.Cmd) ChangedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing ChangedMsg")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		// The domain event is last; do not execute cursor blink timers in tests.
		msg = batch[len(batch)-1]()
	}
	event, ok := msg.(ChangedMsg)
	if !ok {
		t.Fatalf("got %T, want ChangedMsg", msg)
	}
	return event
}

func checkDimensions(t *testing.T, m *Model) {
	t.Helper()
	view := m.View()
	if m.Height() == 0 {
		if view != "" {
			t.Fatal(view)
		}
		return
	}
	rows := strings.Split(view, "\n")
	if len(rows) != m.Height() {
		t.Fatalf("height=%d want=%d", len(rows), m.Height())
	}
	for _, row := range rows {
		if ansi.StringWidth(row) != m.width {
			t.Fatalf("row width=%d want=%d: %q", ansi.StringWidth(row), m.width, row)
		}
	}
}

func TestActiveInputEvents(t *testing.T) {
	for _, kind := range []Kind{Query, Command, Search} {
		m := New(kind, "/ ", "Query…")
		m.SetSize(30, 2)
		if m.Active() || m.View() != "" || m.Handles(key('x', "x")) {
			t.Fatal("new prompt must be inactive")
		}
		m.Focus()
		event := change(t, m.Update(key('x', "x")))
		if event.Kind != kind || event.Text != "x" {
			t.Fatal(event)
		}
		submit, ok := m.Update(key(tea.KeyEnter, ""))().(SubmitMsg)
		if !ok || submit.Kind != kind || submit.Text != "x" || m.Active() {
			t.Fatal(submit)
		}
		m.Focus()
		cancel, ok := m.Update(key(tea.KeyEscape, ""))().(CancelMsg)
		if !ok || cancel.Kind != kind || m.Active() {
			t.Fatal(cancel)
		}
	}
}

func TestHistoryRestoresDraft(t *testing.T) {
	m := New(Query, "/ ", "")
	m.SetSize(40, 1)
	m.SetHistory([]string{"one", "two", "three"})
	m.SetText("draft")
	m.Focus()
	for _, want := range []string{"three", "two", "one", "one"} {
		before := m.Text()
		cmd := m.Update(key(tea.KeyUp, ""))
		if m.Text() != want {
			t.Fatalf("history text=%q want=%q", m.Text(), want)
		}
		if want != before && change(t, cmd).Text != want {
			t.Fatal("history edit event missing")
		}
	}
	for _, want := range []string{"two", "three", "draft", "draft"} {
		before := m.Text()
		cmd := m.Update(key(tea.KeyDown, ""))
		if m.Text() != want {
			t.Fatalf("history text=%q want=%q", m.Text(), want)
		}
		if want != before && change(t, cmd).Text != want {
			t.Fatal("history edit event missing")
		}
	}
}

func TestCompletionCyclingAndMapping(t *testing.T) {
	m := New(Query, "/ ", "Query…")
	m.SetSize(50, 2)
	mapping := map[string]string{}
	calls := 0
	m.SetCompletion(func(text string) []string {
		calls++
		mapping = map[string]string{"escaped1": "db.one", "escaped2": "db.two", "escaped3": "db.three"}
		return []string{"escaped1", "escaped2", "escaped3", "escaped1"}
	}, func(candidate string) string { return mapping[candidate] })
	m.Focus()
	if got := strings.Join(m.Candidates(), ","); got != "db.one,db.two,db.three" {
		t.Fatal(got)
	}
	if !strings.Contains(ansi.Strip(m.View()), "db.one") {
		t.Fatal("missing ghost completion")
	}
	for _, want := range []string{"db.one", "db.two", "db.three", "db.one"} {
		if got := change(t, m.Update(key(tea.KeyTab, ""))).Text; got != want {
			t.Fatalf("completion=%q want=%q", got, want)
		}
	}
	if calls != 1 {
		t.Fatal("tab cycling discarded the original completion set")
	}
	if got := change(t, m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})).Text; got != "db.three" {
		t.Fatal(got)
	}
	if got := change(t, m.Update(key('x', "x"))).Text; got != "db.threex" || calls != 2 {
		t.Fatalf("editing after completion: %q calls=%d", got, calls)
	}
	checkDimensions(t, m)
}

func TestShiftTabStartsWithLastCandidate(t *testing.T) {
	m := New(Command, ": ", "")
	m.SetCompletion(func(string) []string { return []string{"one", "two"} })
	m.Focus()
	if got := change(t, m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})).Text; got != "two" {
		t.Fatal(got)
	}
}

func TestGhostTextWithoutStrip(t *testing.T) {
	m := New(Query, "/ ", "placeholder")
	m.SetSize(30, 1)
	m.SetText("db.")
	m.SetCompletion(func(string) []string { return []string{"db.orders.find("} })
	m.Focus()
	if m.Text() != "db." || m.Height() != 1 {
		t.Fatal("ghost completion must not change input")
	}
	if !strings.Contains(ansi.Strip(m.View()), "db.orders.find(") {
		t.Fatalf("missing inline ghost text: %q", m.View())
	}
	m.SetText("")
	if !strings.Contains(ansi.Strip(m.View()), "db.orders.find(") {
		t.Fatal("placeholder hid empty-input ghost completion")
	}
}

func TestDimensionsAndConfiguration(t *testing.T) {
	m := New(Search, "🔎 ", "界界界")
	m.SetText("界界👩‍💻abc")
	m.SetCompletion(func(string) []string { return []string{"界界👩‍💻abcdef", "two", "three"} })
	m.Focus()
	for _, w := range []int{0, 1, 2, 3, 4, 7, 15, 30} {
		for _, h := range []int{0, 1, 2, 9} {
			m.SetSize(w, h)
			checkDimensions(t, m)
		}
	}
	m.SetCandidateStrip(false)
	if m.Height() != 1 {
		t.Fatal("disabled strip still visible")
	}
	m.SetKind(Command)
	m.SetPrefix(": ")
	m.SetPlaceholder("command")
	if m.Kind() != Command {
		t.Fatal("kind was not updated")
	}
	m.Blur()
	checkDimensions(t, m)
}
