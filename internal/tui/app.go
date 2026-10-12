// Package tui composes tango's terminal application. Domain data lives in
// state; widgets and rendering live here. ApplyMsg is the only entry point for
// background work to mutate the data store.
package tui

import (
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"tango/internal/documents"
	"tango/internal/metrics"
	"tango/internal/query"
	"tango/internal/state"
	"tango/internal/tui/chrome"
	"tango/internal/tui/doctable"
	"tango/internal/tui/jsontree"
	"tango/internal/tui/jsonview"
	"tango/internal/tui/overlay"
	"tango/internal/tui/palette"
	"tango/internal/tui/prompt"
)

type Model struct {
	sidebar                                           *sidebarModel
	table                                             *doctable.Model
	tree                                              *jsontree.Model
	json                                              *jsonview.Model
	indexes                                           *doctable.Model
	stats                                             *statsModel
	prompt                                            *prompt.Model
	palette                                           *palette.Model
	help                                              *overlay.Help
	confirm                                           *overlay.Confirm
	typeConfirm                                       *overlay.TypeConfirm
	confirmation                                      *state.Confirmation
	spinner                                           spinner.Model
	snapshot                                          metrics.Snapshot
	overlay                                           string
	width, height, bodyHeight, sidebarWidth           int
	expandedHeader                                    bool
	sidebarOverride                                   *bool
	docsRev, detailRev, listRev, statsRev, sidebarRev uint64
	pathDB, pathColl                                  string
	selectedDB, selectedColl                          string
	initialized                                       bool
	sized                                             bool
	cachedSidebar                                     map[string]bool

	// Exec defaults to tea.ExecProcess. Headless tests can run the same command
	// and callback without Bubble Tea's terminal handoff.
	Exec func(*exec.Cmd, tea.ExecCallback) tea.Cmd
}

func New() *Model {
	s := state.S
	if s == nil {
		s = &state.AppState{}
		state.S = s
	}
	if s.View == "" {
		s.View = "list"
	}
	if s.Focus == "" {
		s.Focus = "sidebar"
	}
	if s.QueryTimeout <= 0 {
		s.QueryTimeout = 10 * time.Second
	}
	if s.SelectedDocs == nil {
		s.SelectedDocs = map[int]bool{}
	}
	m := &Model{
		sidebar: newSidebar(), table: doctable.New(), tree: jsontree.New(),
		json: jsonview.New(), indexes: doctable.New(), stats: newStats(),
		prompt:  prompt.New(prompt.Query, "/ ", "{field: value} or db.collection.find(…)"),
		palette: palette.New(nil), help: overlay.NewHelp(helpSections()),
		spinner: chrome.NewSpinner(), width: 80, height: 24, Exec: tea.ExecProcess,
		cachedSidebar: map[string]bool{},
	}
	m.prompt.SetCandidateStrip(false)
	m.sync()
	m.resize()
	return m
}

func (m *Model) Init() tea.Cmd { return m.spinner.Tick }

// SetSnapshot is called by the metrics sink through state.Post.
func (m *Model) SetSnapshot(snapshot metrics.Snapshot) { m.snapshot = snapshot }

func (m *Model) PromptText() string    { return m.prompt.Text() }
func (m *Model) SidebarFilter() string { return m.sidebar.FilterText() }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case state.ApplyMsg:
		if msg.Fn != nil {
			msg.Fn()
		}
	case tea.WindowSizeMsg:
		m.sized = true
		m.width, m.height = max(0, msg.Width), max(0, msg.Height)
	case tea.KeyPressMsg:
		cmd = m.key(msg)
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
	case metrics.Snapshot:
		m.SetSnapshot(msg)
	case sidebarSelectDB:
		documents.SelectDatabase(msg.DB, nil)
	case sidebarOpenColl:
		if state.S.CurrentDB != msg.DB {
			documents.SelectDatabase(msg.DB, func() { documents.SelectCollection(msg.Coll) })
		} else {
			documents.SelectCollection(msg.Coll)
		}
	case sidebarBack:
		db, coll := m.sidebar.Selected()
		documents.StopLiveMode()
		state.ClearDocPanel()
		state.S.CurrentColl = ""
		if coll != "" {
			state.S.CurrentDB = db
			m.sidebar.SelectPath(db, "")
		} else {
			state.S.CurrentDB = ""
			m.sidebar.Collapse(db)
		}
		state.S.SelectedDB, state.S.SelectedColl = db, ""
		state.S.Focus = "sidebar"
	case palette.CloseMsg, overlay.CloseMsg:
		m.closeOverlay()
	case palette.ChooseMsg:
		m.closeOverlay()
		if msg.Item != nil {
			m.palette.Remember(*msg.Item)
			value := msg.Item.Value
			if strings.HasSuffix(value, " ") {
				state.S.PromptKind, state.S.PromptText = "command", strings.TrimPrefix(value, ":")
			} else if msg.Item.Kind == palette.Action {
				cmd = m.action(value)
			} else {
				query.ExecuteQuery(value)
			}
		} else {
			query.ExecuteQuery(":" + strings.TrimPrefix(strings.TrimSpace(msg.Input), ":"))
		}
	case prompt.ChangedMsg:
		if msg.Kind == prompt.Search {
			documents.ApplyDocSearchHighlight(msg.Text)
		}
	case prompt.SubmitMsg:
		m.prompt.Blur()
		switch msg.Kind {
		case prompt.Search:
			documents.ApplyDocSearchHighlight(msg.Text)
			documents.EndDocSearch(false)
		case prompt.Command:
			query.ExecuteQuery(":" + strings.TrimPrefix(msg.Text, ":"))
		default:
			query.ExecuteQuery(msg.Text)
		}
	case prompt.CancelMsg:
		m.prompt.Blur()
		if msg.Kind == prompt.Search {
			documents.EndDocSearch(true)
		}
	case overlay.ConfirmedMsg:
		request := m.confirmation
		m.closeOverlay()
		state.S.Confirm = nil
		if request != nil && request.ID == msg.ID && request.OnYes != nil {
			request.OnYes()
		}
	case overlay.CancelledMsg:
		m.closeOverlay()
		state.S.Confirm = nil
	default:
		if m.prompt.Active() {
			cmd = m.prompt.Update(msg)
		}
		if m.overlay == "palette" {
			cmd = m.palette.Update(msg)
		}
		if m.overlay == "help" {
			cmd = m.help.Update(msg)
		}
		if m.typeConfirm != nil && m.overlay == "confirm" {
			cmd = m.typeConfirm.Update(msg)
		}
	}
	return m, tea.Batch(cmd, m.sync())
}

func (m *Model) sync() tea.Cmd {
	s := state.S
	if s.View != "stats" && s.StatsStopCh != nil {
		close(s.StatsStopCh)
		s.StatsStopCh = nil
		s.StatsEpoch++
	}
	if !m.initialized || m.sidebarRev != s.SidebarRevision {
		for db := range m.cachedSidebar {
			if _, present := s.Collections[db]; !present {
				m.sidebar.SetCollections(db, nil)
			}
			m.sidebar.SetLoading(db, false)
		}
		m.cachedSidebar = map[string]bool{}
		m.sidebar.SetDatabases(sidebarDatabaseEntries(s.AllDBItems))
		for db, entries := range s.Collections {
			m.sidebar.SetCollections(db, sidebarEntries(entries))
			m.cachedSidebar[db] = true
		}
		for db, loading := range s.CollectionsLoading {
			m.sidebar.SetLoading(db, loading)
			m.cachedSidebar[db] = true
		}
		m.sidebarRev = s.SidebarRevision
	}
	m.sidebar.SetActive(s.CurrentDB, s.CurrentColl)
	if m.pathDB != s.CurrentDB || m.pathColl != s.CurrentColl {
		m.sidebar.SelectPath(s.CurrentDB, s.CurrentColl)
		m.pathDB, m.pathColl = s.CurrentDB, s.CurrentColl
	}
	if m.selectedDB != s.SelectedDB || m.selectedColl != s.SelectedColl {
		m.sidebar.SelectPath(s.SelectedDB, s.SelectedColl)
		m.selectedDB, m.selectedColl = s.SelectedDB, s.SelectedColl
	}
	if !m.initialized || m.docsRev != s.DocsRevision {
		m.table.SetDocs(s.CurrentDocs)
		m.docsRev = s.DocsRevision
	}
	m.table.SetCursor(s.Cursor)
	s.Cursor = m.table.Cursor()
	m.table.SetSelected(s.SelectedDocs)
	if s.CurrentColl == "" && len(s.CurrentDocs) == 0 {
		text := "Tab to browse databases · : commands"
		if s.Focus == "sidebar" {
			text = "↵ open a collection · : commands"
		}
		m.table.SetEmptyText(text)
	} else {
		m.table.SetEmptyText("No documents")
	}
	if !m.initialized || m.detailRev != s.DetailRevision {
		m.json.SetTokens(s.DetailTokens)
		m.tree.SetDoc(s.ExpandedDoc)
		m.detailRev = s.DetailRevision
	}
	m.json.SetSearch(s.DocSearchTerm)
	if !m.initialized || m.listRev != s.ListRevision {
		m.indexes.SetDocs(documents.IndexRows(s.CurrentIndexes))
		m.indexes.SetEmptyText("No indexes")
		m.listRev = s.ListRevision
	}
	m.indexes.SetCursor(s.IndexCursor)
	if !m.initialized || m.statsRev != s.StatsRevision {
		m.stats.SetData(s.StatsUpdated, s.StatsSections, s.StatsText)
		m.statsRev = s.StatsRevision
	}
	m.initialized = true
	var cmd tea.Cmd
	if s.PromptKind != "" {
		cmd = m.openPrompt(prompt.Kind(s.PromptKind), s.PromptText)
		s.PromptKind, s.PromptText = "", ""
	}
	if s.Confirm != nil && m.confirmation != s.Confirm {
		m.closeOverlay()
		m.confirmation = s.Confirm
		m.overlay = "confirm"
		if s.Confirm.Expected == "" {
			m.confirm = overlay.NewConfirm(s.Confirm.ID, s.Confirm.Question)
		} else {
			m.typeConfirm = overlay.NewTypeConfirm(s.Confirm.ID, s.Confirm.Question, s.Confirm.Expected)
			cmd = tea.Batch(cmd, m.typeConfirm.Focus())
		}
	}
	if s.Editor != nil {
		editor := s.Editor
		s.Editor = nil
		cmd = tea.Batch(cmd, m.Exec(editor.Command, func(err error) tea.Msg {
			return state.ApplyMsg{Fn: func() { editor.Done(err) }}
		}))
	}
	m.resize()
	if s.Quit {
		return tea.Quit
	}
	return cmd
}

func (m *Model) openPrompt(kind prompt.Kind, text string) tea.Cmd {
	m.prompt.SetKind(kind)
	prefix, placeholder := "/ ", "{field: value} or db.collection.find(…)"
	switch kind {
	case prompt.Command:
		prefix, placeholder = ": ", "Command"
	case prompt.Search:
		prefix, placeholder = "/ ", "Search document"
	}
	m.prompt.SetPrefix(prefix)
	m.prompt.SetPlaceholder(placeholder)
	history := state.S.QueryHistory
	if kind == prompt.Search {
		history = nil
	}
	if kind == prompt.Command {
		history = nil
		for _, entry := range state.S.QueryHistory {
			if strings.HasPrefix(entry, ":") {
				history = append(history, strings.TrimPrefix(entry, ":"))
			}
		}
	}
	m.prompt.SetHistory(history)
	if kind == prompt.Search {
		m.prompt.SetCompletion(nil)
	} else {
		m.prompt.SetCompletion(func(text string) []string {
			if kind == prompt.Command {
				entries := query.GetAutocompleteSuggestions(":" + text)
				for i := range entries {
					entries[i] = strings.TrimPrefix(entries[i], ":")
				}
				return entries
			}
			return query.GetAutocompleteSuggestions(text)
		})
	}
	m.prompt.SetText(text)
	return m.prompt.Focus()
}

func (m *Model) closeOverlay() {
	m.overlay = ""
	m.palette.Blur()
	m.confirm, m.typeConfirm, m.confirmation = nil, nil, nil
}
