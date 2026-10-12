package tui

import (
	tea "charm.land/bubbletea/v2"

	"tango/internal/documents"
	"tango/internal/state"
	"tango/internal/tui/prompt"
)

func (m *Model) key(key tea.KeyPressMsg) tea.Cmd {
	s := state.S
	k := key.String()
	if k == "ctrl+c" {
		s.Quit = true
		return tea.Quit
	}
	if m.overlay != "" {
		switch m.overlay {
		case "palette":
			return m.palette.Update(key)
		case "help":
			return m.help.Update(key)
		case "confirm":
			if m.typeConfirm != nil {
				return m.typeConfirm.Update(key)
			}
			return m.confirm.Update(key)
		}
	}
	if m.prompt.Handles(key) {
		return m.prompt.Update(key)
	}
	// Once filtering, even q/W/: are literal input. Outside filtering the
	// established global shortcuts take precedence over implicit type-to-filter.
	if s.Focus == "sidebar" && m.sidebar.Filtering() && m.sidebar.Handles(key) {
		cmd := m.sidebar.Update(key)
		if k == "enter" {
			cmd = m.sidebar.Update(key)
		}
		s.SelectedDB, s.SelectedColl = m.sidebar.Selected()
		return cmd
	}
	switch k {
	case "tab", "shift+tab":
		if s.Focus == "sidebar" {
			s.Focus = "content"
		} else {
			s.Focus = "sidebar"
			if !m.showSidebar() {
				visible := true
				m.sidebarOverride = &visible
				s.WideMode = false
			}
		}
		return nil
	case "?":
		m.overlay = "help"
		return m.help.Focus()
	case ":":
		m.palette.SetItems(m.commands())
		m.palette.SetText("")
		m.overlay = "palette"
		return m.palette.Focus()
	case "q":
		s.Quit = true
		return tea.Quit
	case "W":
		m.toggleSidebar()
		return nil
	case "M":
		m.expandedHeader = !m.expandedHeader
		return nil
	case "S":
		documents.ShowStats()
		return nil
	case "L":
		if s.LiveMode {
			documents.StopLiveMode()
		} else {
			documents.StartLiveMode()
		}
		return nil
	case "R":
		documents.FullRefresh()
		return nil
	case "r":
		if s.View == "stats" && s.Focus == "content" {
			documents.RefreshStats(true)
		} else {
			documents.Refresh()
		}
		return nil
	}
	if s.Focus == "sidebar" {
		db, coll := m.sidebar.Selected()
		s.SelectedDB, s.SelectedColl = db, coll
		switch k {
		case "I":
			if coll != "" {
				documents.ShowIndexesFor(state.Namespace{DB: db, Coll: coll})
			}
			return nil
		case "D":
			if coll != "" {
				documents.DropCollectionConfirm()
			} else {
				documents.DropDatabaseConfirm()
			}
			return nil
		}
		if m.sidebar.Handles(key) {
			cmd := m.sidebar.Update(key)
			s.SelectedDB, s.SelectedColl = m.sidebar.Selected()
			return cmd
		}
		return nil
	}
	if k == "esc" {
		m.back()
		return nil
	}
	if k == "i" && (s.View == "list" || s.View == "tree" || s.View == "detail") {
		documents.InsertDocument()
		return nil
	}
	switch s.View {
	case "tree", "detail":
		switch k {
		case "e":
			documents.EditDocument()
			return nil
		case "c":
			documents.CopyToClipboard()
			return nil
		case "t":
			if s.View == "tree" {
				documents.ToggleToDetail()
			} else {
				documents.ToggleToTree()
			}
			return nil
		case "/":
			documents.StartDocSearch()
			return nil
		}
		if s.View == "tree" && m.tree.Handles(key) {
			return m.tree.Update(key)
		}
		if s.View == "detail" && m.json.Handles(key) {
			return m.json.Update(key)
		}
	case "indexes":
		if k == "enter" {
			documents.ShowIndexDetail(s.IndexCursor)
			return nil
		}
		if k == "X" {
			documents.DropIndexConfirm()
			return nil
		}
		if m.indexes.Handles(key) {
			cmd := m.indexes.Update(key)
			s.IndexCursor = m.indexes.Cursor()
			return cmd
		}
	case "stats":
		if m.stats.Handles(key) {
			return m.stats.Update(key)
		}
	default:
		if m.table.Handles(key) {
			cmd := m.table.Update(key)
			s.Cursor = m.table.Cursor()
			return cmd
		}
		switch k {
		case "enter":
			documents.ExpandDocument(s.Cursor)
		case "space":
			documents.ToggleSelection(s.Cursor)
			if s.Cursor < len(s.CurrentDocs)-1 {
				s.Cursor++
			}
		case "v":
			m.table.ToggleMode()
		case "d":
			documents.DeleteDocumentConfirm()
		case "e":
			documents.EditDocument()
		case "x":
			documents.ExportJSON()
		case "C":
			documents.ExportCSV()
		case "s":
			if s.CurrentColl != "" && s.ResultLabel == "" {
				documents.PromptSort()
			}
		case "a":
			documents.SelectAll()
		case "A":
			documents.ClearSelection()
		case "n":
			documents.NextPage()
		case "p":
			documents.PrevPage()
		case "/":
			return m.openPrompt(prompt.Query, s.QueryText)
		}
	}
	return nil
}

func (m *Model) toggleSidebar() {
	visible := !m.showSidebar()
	m.sidebarOverride = &visible
	state.S.WideMode = !visible
	if !visible {
		state.S.Focus = "content"
	}
}

func (m *Model) back() {
	s := state.S
	switch s.View {
	case "tree", "detail":
		documents.CollapseDocument()
	case "indexes", "stats":
		s.View, s.Focus = "list", "content"
	default:
		s.Focus = "sidebar"
		visible := true
		m.sidebarOverride = &visible
		s.WideMode = false
		m.sidebar.SelectPath(s.CurrentDB, s.CurrentColl)
	}
}

func (m *Model) action(name string) tea.Cmd {
	switch name {
	case "query":
		return m.openPrompt(prompt.Query, state.S.QueryText)
	case "help":
		m.overlay = "help"
		return m.help.Focus()
	case "sidebar":
		m.toggleSidebar()
	case "header":
		m.expandedHeader = !m.expandedHeader
	case "table":
		m.table.ToggleMode()
	case "delete":
		documents.DeleteDocumentConfirm()
	case "edit":
		documents.EditDocument()
	case "insert":
		documents.InsertDocument()
	case "drop-collection":
		documents.DropCollectionConfirm()
	case "drop-index":
		documents.DropIndexConfirm()
	case "copy":
		documents.CopyToClipboard()
	case "all":
		documents.SelectAll()
	case "none":
		documents.ClearSelection()
	case "next":
		documents.NextPage()
	case "previous":
		documents.PrevPage()
	case "tree":
		if state.S.View == "tree" {
			documents.ToggleToDetail()
		} else {
			documents.ToggleToTree()
		}
	}
	return nil
}
