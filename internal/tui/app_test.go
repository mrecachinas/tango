package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tango/internal/documents"
	"tango/internal/metrics"
	"tango/internal/mjson"
	"tango/internal/state"
	"tango/internal/theme"
	"tango/internal/tui/doctable"
	"tango/internal/tui/palette"
	"tango/internal/tui/prompt"
)

func testModel(t *testing.T) *Model {
	t.Helper()
	state.ClosePost()
	state.S = &state.AppState{
		HostLabel: "localhost:27017", CurrentDB: "sample_app", CurrentColl: "events",
		View: "list", Focus: "content", DocsNS: state.Namespace{DB: "sample_app", Coll: "events"},
		DocsEditable: true, DocsDeletable: true,
		AllDBItems:  []state.ListEntry{{Name: "sample_app"}, {Name: "inventory"}},
		Collections: map[string][]state.ListEntry{"sample_app": {{Name: "events", Info: "500"}, {Name: "users", Info: "120"}}},
		CurrentDocs: []bson.D{
			{{Key: "_id", Value: int32(1)}, {Key: "name", Value: "[red]hi 界 🥭"}, {Key: "ts", Value: int32(3)}},
			{{Key: "_id", Value: int32(2)}, {Key: "name", Value: "second"}, {Key: "ts", Value: int32(1)}},
		},
		TotalDocs: 500, TotalKnown: true, TotalExact: true, SelectedDocs: map[int]bool{},
	}

	state.PageSize = 50
	theme.Set(theme.New(theme.Dark()))
	m := New()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 25})
	t.Cleanup(state.Shutdown)
	return m
}

func disconnectedClient(t *testing.T) *mongo.Client {
	t.Helper()
	client, err := mongo.Connect(context.Background(), options.Client().
		SetHosts([]string{"127.0.0.1:1"}).SetServerSelectionTimeout(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	return client
}

func press(m *Model, name string) tea.Cmd {
	key := tea.Key{}
	switch name {
	case "enter":
		key.Code = tea.KeyEnter
	case "esc":
		key.Code = tea.KeyEscape
	case "tab":
		key.Code = tea.KeyTab
	case "shift+tab":
		key.Code, key.Mod = tea.KeyTab, tea.ModShift
	case "ctrl+c":
		key.Code, key.Mod = 'c', tea.ModCtrl
	case "space":
		key.Code, key.Text = ' ', " "
	default:
		key.Code, key.Text = []rune(name)[0], name
	}
	_, cmd := m.Update(tea.KeyPressMsg(key))
	return cmd
}

// Event commands are synchronous in these tests; blink/spinner messages are
// ignored rather than starting an infinite timer loop.
func applyCmd(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, next := range msg {
			applyCmd(m, next)
		}
	case nil, cursor.BlinkMsg, spinner.TickMsg, tea.QuitMsg:
	default:
		_, next := m.Update(msg)
		applyCmd(m, next)
	}
}

func assertFrame(t *testing.T, m *Model, w, h int) string {
	t.Helper()
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	v := m.View()
	if !v.AltScreen || v.WindowTitle != "tango" {
		t.Fatalf("view settings: %+v", v)
	}
	lines := strings.Split(v.Content, "\n")
	if h == 0 {
		if v.Content != "" {
			t.Fatal("nonempty zero-height frame")
		}
		return ""
	}
	if len(lines) != h {
		t.Fatalf("lines=%d want %d", len(lines), h)
	}
	for i, line := range lines {
		if width := ansi.StringWidth(line); width != w {
			t.Fatalf("line %d width=%d want %d: %q", i, width, w, ansi.Strip(line))
		}
	}
	return ansi.Strip(v.Content)
}

func TestLayoutResizeAndOverlays(t *testing.T) {
	m := testModel(t)
	screen := assertFrame(t, m, 120, 25)
	for _, text := range []string{"Databases", "sample_app › events", "_id", "NORMAL", "1–2 of 500"} {
		if !strings.Contains(screen, text) {
			t.Errorf("missing %q:\n%s", text, screen)
		}
	}
	for _, size := range [][2]int{{40, 10}, {89, 18}, {90, 18}, {160, 50}, {1, 1}, {0, 0}, {20, 4}} {
		assertFrame(t, m, size[0], size[1])
	}
	if !strings.Contains(assertFrame(t, m, 30, 6), "Terminal too small") {
		t.Fatal("missing tiny-terminal fallback")
	}
	assertFrame(t, m, 120, 25)
	press(m, "?")
	for _, size := range [][2]int{{40, 10}, {120, 25}, {95, 12}} {
		assertFrame(t, m, size[0], size[1])
	}
	applyCmd(m, press(m, "esc"))
	press(m, ":")
	assertFrame(t, m, 40, 10)
	assertFrame(t, m, 120, 25)
}

func TestSelectionViewerAndModeToggle(t *testing.T) {
	m := testModel(t)
	press(m, "space")
	if !state.S.SelectedDocs[0] || state.S.Cursor != 1 {
		t.Fatalf("selection/cursor: %+v", state.S.SelectedDocs)
	}
	press(m, "a")
	if len(state.S.SelectedDocs) != 2 {
		t.Fatal("select all failed")
	}
	press(m, "A")
	if len(state.S.SelectedDocs) != 0 {
		t.Fatal("clear selection failed")
	}
	press(m, "v")
	if m.table.Mode() == doctable.ModeTable {
		t.Fatal("v did not change row mode")
	}
	press(m, "enter")
	if state.S.View != "tree" || state.S.ExpandedDoc == nil {
		t.Fatal("Enter did not open document tree")
	}
	press(m, "t")
	if state.S.View != "detail" {
		t.Fatal("t did not show JSON")
	}
	screen := assertFrame(t, m, 120, 25)
	if !strings.Contains(screen, "second") {
		t.Fatal("detail text missing")
	}
	press(m, "esc")
	if state.S.View != "list" {
		t.Fatal("Escape did not return to documents")
	}
}

func TestPromptsCaptureShortcutsAndSearch(t *testing.T) {
	m := testModel(t)
	press(m, "/")
	for _, char := range []string{"q", "W", "S", "L", "?"} {
		press(m, char)
	}
	if m.prompt.Text() != "qWSL?" || state.S.Quit || state.S.LiveMode || m.overlay != "" {
		t.Fatal("query input leaked global shortcut")
	}
	if m.mode() != "QUERY" || !strings.Contains(m.View().Content, "qWSL?") {
		t.Fatal("missing query mode/input")
	}
	applyCmd(m, press(m, "esc"))
	press(m, "enter")
	press(m, "/")
	applyCmd(m, press(m, "h"))
	applyCmd(m, press(m, "i"))
	if state.S.DocSearchTerm != "hi" || m.mode() != "SEARCH" || !strings.Contains(m.json.MatchInfo(), "1/1") {
		t.Fatalf("search: %q %s", state.S.DocSearchTerm, m.json.MatchInfo())
	}
	applyCmd(m, press(m, "enter"))
	if m.prompt.Active() || state.S.DocSearchTerm != "hi" {
		t.Fatal("Enter did not retain search highlight")
	}
	press(m, "/")
	applyCmd(m, press(m, "esc"))
	if state.S.DocSearchTerm != "" || state.S.DocSearchActive {
		t.Fatal("Escape did not clear search")
	}
}

func TestSidebarFocusFilteringAndAutoHide(t *testing.T) {
	m := testModel(t)
	press(m, "tab")
	if state.S.Focus != "sidebar" {
		t.Fatal("Tab did not focus sidebar")
	}
	press(m, "/")
	for _, char := range []string{"q", "W", "L", ":"} {
		press(m, char)
	}
	if m.SidebarFilter() != "qWL:" || state.S.Quit || state.S.LiveMode || m.overlay != "" || m.mode() != "FILTER" {
		t.Fatal("sidebar filter leaked shortcut")
	}
	press(m, "esc")
	press(m, "shift+tab")
	if state.S.Focus != "content" {
		t.Fatal("Shift+Tab did not cycle focus")
	}
	assertFrame(t, m, 60, 12)
	if m.showSidebar() {
		t.Fatal("narrow sidebar not hidden")
	}
	press(m, "W")
	if !m.showSidebar() {
		t.Fatal("W did not override auto-hide")
	}
	assertFrame(t, m, 60, 12)
	press(m, "W")
	if m.showSidebar() || state.S.Focus != "content" {
		t.Fatal("W did not restore content focus")
	}
	press(m, "tab")
	if !m.showSidebar() || state.S.Focus != "sidebar" {
		t.Fatal("Tab did not reveal hidden sidebar")
	}
}

func TestCollectionOpenAndApplyResync(t *testing.T) {
	m := testModel(t)
	state.S.Client = disconnectedClient(t)
	press(m, "tab")
	m.sidebar.SelectPath("sample_app", "users")
	applyCmd(m, press(m, "enter"))
	if state.S.CurrentColl != "users" {
		t.Fatal("collection message not routed")
	}
	m.Update(state.ApplyMsg{Fn: func() {
		documents.ShowResultDocs([]bson.D{{{Key: "_id", Value: 7}, {Key: "name", Value: "new page"}}},
			state.Namespace{DB: "sample_app", Coll: "users"}, "", true, true)
	}})
	if state.S.Focus != "content" || !strings.Contains(ansi.Strip(m.View().Content), "new page") {
		t.Fatal("ApplyMsg did not resync/focus documents")
	}
}

func TestConfirmCapturesActionAndTypeName(t *testing.T) {
	m := testModel(t)
	confirmed := 0
	open := func(expected string) {
		m.Update(state.ApplyMsg{Fn: func() {
			state.S.Confirm = &state.Confirmation{
				ID: "test", Question: "Delete document?", Expected: expected, OnYes: func() { confirmed++ },
			}
		}})
	}
	open("")
	if m.mode() != "CONFIRM" {
		t.Fatal("missing CONFIRM mode")
	}
	assertFrame(t, m, 40, 10)
	applyCmd(m, press(m, "enter"))
	if confirmed != 0 || state.S.Confirm != nil {
		t.Fatal("Enter must not confirm delete")
	}
	open("")
	applyCmd(m, press(m, "y"))
	if confirmed != 1 || m.overlay != "" {
		t.Fatal("y did not confirm")
	}
	open("events")
	for _, r := range "event" {
		press(m, string(r))
	}
	applyCmd(m, press(m, "enter"))
	if confirmed != 1 || m.overlay != "confirm" {
		t.Fatal("partial name confirmed")
	}
	press(m, "s")
	applyCmd(m, press(m, "enter"))
	if confirmed != 2 {
		t.Fatal("exact name did not confirm")
	}
}

func TestPaletteRawCommandsSortAndQuit(t *testing.T) {
	m := testModel(t)
	state.S.Client = disconnectedClient(t)
	press(m, ":")
	for _, r := range "sort ts -1" {
		press(m, string(r))
	}
	applyCmd(m, press(m, "enter"))
	if got := state.FormatSort(state.S.CurrentSort); got != "ts↓" || m.overlay != "" {
		t.Fatalf("raw sort: %q %q", got, m.overlay)
	}
	press(m, "s")
	if m.prompt.Text() != "sort -ts" || m.mode() != "COMMAND" {
		t.Fatal("sort prompt not prefilled")
	}
	applyCmd(m, press(m, "esc"))
	press(m, ":")
	press(m, "q")
	applyCmd(m, press(m, "enter"))
	if !state.S.Quit {
		t.Fatal(":q did not quit")
	}
}

func TestCtrlCAlwaysQuits(t *testing.T) {
	for _, context := range []string{"normal", "query", "palette", "help", "filter", "confirm"} {
		t.Run(context, func(t *testing.T) {
			m := testModel(t)
			switch context {
			case "query":
				press(m, "/")
			case "palette":
				press(m, ":")
			case "help":
				press(m, "?")
			case "filter":
				press(m, "tab")
				press(m, "/")
			case "confirm":
				m.Update(state.ApplyMsg{Fn: func() { state.S.Confirm = &state.Confirmation{ID: "test", Question: "Delete?"} }})
			}
			cmd := press(m, "ctrl+c")
			if !state.S.Quit {
				t.Fatal("ctrl+c did not set quit")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				// Bubble Tea batches the key command with the state's quit intent.
				if _, batch := cmd().(tea.BatchMsg); !batch {
					t.Fatal("missing quit command")
				}
			}
		})
	}
}

func TestReadOnlyMetricsAndLiveViewPreservation(t *testing.T) {
	m := testModel(t)
	state.S.ReadOnly = true
	state.S.LiveMode = true
	m.Update(metrics.Snapshot{Connected: true, Version: "7.0.31", OpsPerSec: 2})
	screen := assertFrame(t, m, 120, 25)
	if !strings.Contains(screen, "READ-ONLY") || !strings.Contains(screen, "LIVE") {
		t.Fatal("missing state badges")
	}
	press(m, "e")
	if state.S.Editor != nil || !strings.Contains(mjson.Join(state.S.DetailTokens), "Read-only") {
		t.Fatal("read-only edit permitted")
	}
	state.S.ReadOnly = false
	documents.ExpandDocument(0)
	m.Update(state.ApplyMsg{Fn: func() { state.S.DocsRevision++; state.S.CurrentDocs = []bson.D{{{Key: "_id", Value: 8}}} }})
	if state.S.View != "tree" {
		t.Fatal("live-like reload stole detail view")
	}
	press(m, "M")
	assertFrame(t, m, 120, 25)
}

func TestEditorUsesExecAndCompletionOnUpdate(t *testing.T) {
	m := testModel(t)
	called, completed := false, false
	m.Exec = func(cmd *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
		called = cmd.Args[0] == "test-editor"
		return func() tea.Msg { return cb(fmt.Errorf("editor error")) }
	}
	_, cmd := m.Update(state.ApplyMsg{Fn: func() {
		state.S.Editor = &state.EditorRequest{Command: exec.Command("test-editor"), Done: func(err error) { completed = err != nil }}
	}})
	if !called || completed {
		t.Fatal("editor callback ran before Update")
	}
	applyCmd(m, cmd)
	if !completed || state.S.Editor != nil {
		t.Fatal("editor request not completed")
	}
}

func TestPaletteActionsAreExposed(t *testing.T) {
	m := testModel(t)
	commands := m.commands()
	for _, value := range []string{":sort ", ":db ", ":q", ":drop database", "insert", "header", "table"} {
		found := false
		for _, item := range commands {
			if item.Value == value {
				found = true
			}
		}
		if !found {
			t.Errorf("missing command %q", value)
		}
	}
	m.Update(palette.ChooseMsg{Item: &palette.Item{Kind: palette.Action, Value: "query"}})
	if m.prompt.Kind() != prompt.Query || !m.prompt.Active() {
		t.Fatal("palette query action not routed")
	}
}

func TestSidebarEnterOpensFilteredCollectionAndEscapeBacksUp(t *testing.T) {
	m := testModel(t)
	state.S.Client = disconnectedClient(t)
	press(m, "tab")
	m.sidebar.SelectPath("sample_app", "users")
	press(m, "/")
	for _, char := range "users" {
		press(m, string(char))
	}
	applyCmd(m, press(m, "enter"))
	if state.S.CurrentColl != "users" || state.S.Focus != "content" {
		t.Fatal("Enter did not open filtered collection")
	}
	press(m, "esc")
	// The first sidebar Escape clears the retained filter.
	press(m, "esc")
	applyCmd(m, press(m, "esc"))
	if state.S.CurrentDB != "sample_app" || state.S.CurrentColl != "" || state.S.Focus != "sidebar" {
		t.Fatal("Escape did not back up to database")
	}
	applyCmd(m, press(m, "esc"))
	if state.S.CurrentDB != "" {
		t.Fatal("Escape did not back out of database")
	}
}

func TestJSONTokensPreserveLiteralTextWithSearch(t *testing.T) {
	m := testModel(t)
	doc := bson.D{
		{Key: "Name", Value: "name NAME"},
		{Key: "label", Value: "[red]hi[-] [::b]bold [\"region\"] [yellow[]"},
		{Key: "tags", Value: bson.A{"x", "[y]", bson.A{}}},
		{Key: "[k]", Value: bson.D{{Key: "a", Value: int32(1)}}},
	}
	m.Update(state.ApplyMsg{Fn: func() { state.SetDetail("Literal tokens", mjson.Tokens(doc, true)) }})
	m.json.SetLineNumbers(false)
	for _, term := range []string{"", "y", "[", "name"} {
		m.Update(state.ApplyMsg{Fn: func() { state.S.DocSearchTerm = term }})
		assertFrame(t, m, 240, 40)
		lines := strings.Split(ansi.Strip(m.json.View()), "\n")
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], " ")
		}
		got := strings.TrimRight(strings.Join(lines, "\n"), "\n")
		if want := mjson.Pretty(doc); got != want {
			t.Fatalf("term=%q\n got: %q\nwant: %q", term, got, want)
		}
	}
	if !strings.HasSuffix(m.json.MatchInfo(), "/3") {
		t.Fatalf("matches: %s", m.json.MatchInfo())
	}
}
