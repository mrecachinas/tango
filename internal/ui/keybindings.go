package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tango/internal/documents"
	"tango/internal/query"
	"tango/internal/state"
)

func setupKeybindings() {
	s := state.S

	// ── Database list ──
	s.DbList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2 {
			if len(s.DbFilterText) > 0 {
				s.DbFilterText = s.DbFilterText[:len(s.DbFilterText)-1]
				query.FilterDBList()
				return nil
			}
		}
		if event.Key() == tcell.KeyEscape {
			if s.DbFilterText != "" {
				s.DbFilterText = ""
				query.FilterDBList()
				return nil
			}
		}
		r := event.Rune()
		if r != 0 && r != ' ' && event.Key() == tcell.KeyRune {
			s.DbFilterText += string(r)
			query.FilterDBList()
			return nil
		}
		return event
	})

	// ── Collection list ──
	s.CollList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2 {
			if len(s.CollFilterText) > 0 {
				s.CollFilterText = s.CollFilterText[:len(s.CollFilterText)-1]
				query.FilterCollList()
				return nil
			}
		}
		if event.Key() == tcell.KeyEscape {
			if s.CollFilterText != "" {
				s.CollFilterText = ""
				query.FilterCollList()
				return nil
			}
		}
		r := event.Rune()
		if r != 0 && r != ' ' && event.Key() == tcell.KeyRune {
			s.CollFilterText += string(r)
			query.FilterCollList()
			return nil
		}
		return event
	})

	// ── Document list ──
	s.DocList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == ' ' {
			idx := s.DocList.GetCurrentItem()
			if idx >= 0 && idx < len(s.CurrentDocs) {
				documents.ToggleSelection(idx)
				if idx < s.DocList.GetItemCount()-1 {
					s.DocList.SetCurrentItem(idx + 1)
				}
			}
			return nil
		}
		return event
	})

	// ── Document detail view ──
	s.DocDetail.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			if s.DocSearchActive {
				documents.EndDocSearch(true)
				return nil
			}
			documents.CollapseDocument()
			return nil
		}
		if event.Rune() == 'e' {
			if !s.ReadOnly {
				documents.EditDocument()
			}
			return nil
		}
		if event.Rune() == 'c' {
			documents.CopyToClipboard()
			return nil
		}
		if event.Rune() == 't' {
			documents.ToggleToTree()
			return nil
		}
		if event.Rune() == '/' {
			documents.StartDocSearch()
			return nil
		}
		return event
	})

	// ── Document tree view ──
	s.DocTree.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			documents.CollapseDocument()
			return nil
		}
		if event.Rune() == 'e' {
			if !s.ReadOnly {
				documents.EditDocument()
			}
			return nil
		}
		if event.Rune() == 'c' {
			documents.CopyToClipboard()
			return nil
		}
		if event.Rune() == 't' {
			documents.ToggleToDetail()
			return nil
		}
		return event
	})
	s.DocTree.SetSelectedFunc(func(node *tview.TreeNode) {
		node.SetExpanded(!node.IsExpanded())
	})

	// ── Stats view ──
	s.StatsView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			s.DocPages.SwitchToPage("list")
			s.App.SetFocus(s.DocList)
			state.RefreshPanelBorders()
			return nil
		}
		if event.Rune() == 'r' {
			documents.ShowStats()
			return nil
		}
		return event
	})

	// ── Help view ──
	s.HelpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Rune() == '?' {
			s.RootPages.SwitchToPage("main")
			return nil
		}
		return event
	})

	// ── Global key bindings ──
	s.App.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		focused := s.App.GetFocus()
		if focused == s.SearchBar || focused == s.DocSearchBar {
			return event
		}
		if focused == s.DocDetail {
			return event
		}

		switch event.Key() {
		case tcell.KeyTab:
			state.CycleFocus(true)
			return nil
		case tcell.KeyBacktab:
			state.CycleFocus(false)
			return nil
		}

		// Global commands that work from any panel
		switch event.Rune() {
		case '?':
			name, _ := s.RootPages.GetFrontPage()
			if name == "help" {
				s.RootPages.SwitchToPage("main")
			} else {
				s.RootPages.SwitchToPage("help")
				s.App.SetFocus(s.HelpView)
			}
			return nil
		case ':':
			s.SearchBar.SetText(":")
			s.App.SetFocus(s.SearchBar)
			return nil
		case 'q':
			s.App.Stop()
			return nil
		case 'W':
			ToggleWideMode()
			return nil
		case 'S':
			documents.ShowStats()
			return nil
		case 'L':
			if state.S.LiveMode {
				documents.StopLiveMode()
			} else {
				documents.StartLiveMode()
			}
			return nil
		}

		// Collection list: pass through for filtering except specific commands
		if focused == s.CollList {
			switch event.Rune() {
			case 'I':
				collName := state.GetSelectedCollName()
				if s.CurrentDB != "" && collName != "" {
					s.CurrentColl = collName
					documents.ShowIndexes()
					return nil
				}
			case 'D':
				if s.ReadOnly {
					return nil
				}
				if s.CollList.GetItemCount() > 0 {
					documents.DropCollectionConfirm()
					return nil
				}
			}
			return event
		}
		if focused == s.DbList {
			return event
		}

		switch event.Rune() {
		case '/':
			s.App.SetFocus(s.SearchBar)
			return nil
		case 'd':
			if s.ReadOnly {
				return nil
			}
			if s.App.GetFocus() == s.DocList && len(s.CurrentDocs) > 0 {
				documents.DeleteDocumentConfirm()
			}
			return nil
		case 'x':
			if s.App.GetFocus() == s.DocList && len(s.CurrentDocs) > 0 {
				documents.ExportJSON()
			}
			return nil
		case 'C':
			if s.App.GetFocus() == s.DocList && len(s.CurrentDocs) > 0 {
				documents.ExportCSV()
			}
			return nil
		case 'i':
			if s.ReadOnly {
				return nil
			}
			if s.CurrentDB != "" && s.CurrentColl != "" {
				documents.InsertDocument()
			}
			return nil
		case 's':
			if s.App.GetFocus() == s.DocList && s.CurrentDB != "" && s.CurrentColl != "" {
				documents.PromptSort()
			}
			return nil
		case 'a':
			if s.App.GetFocus() == s.DocList {
				for i := 0; i < len(s.CurrentDocs); i++ {
					s.SelectedDocs[i] = true
				}
				documents.RefreshDocListSelection()
			}
			return nil
		case 'A':
			if s.App.GetFocus() == s.DocList {
				documents.ClearSelection()
				documents.RefreshDocListSelection()
			}
			return nil
		case 'X':
			if s.ReadOnly {
				return nil
			}
			if s.App.GetFocus() == s.IndexList && len(s.CurrentIndexes) > 0 {
				documents.DropIndexConfirm()
			}
			return nil
		case 'n':
			if s.LiveMode {
				return nil
			}
			if s.CurrentColl != "" {
				documents.NextPage()
			}
			return nil
		case 'p':
			if s.LiveMode {
				return nil
			}
			if s.CurrentColl != "" {
				documents.PrevPage()
			}
			return nil
		case 'r':
			if focused != s.StatsView {
				documents.Refresh()
				return nil
			}
		case 'R':
			documents.FullRefresh()
			return nil
		}

		return event
	})
}
