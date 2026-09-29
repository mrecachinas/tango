package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tango/internal/documents"
	"tango/internal/state"
)

func trimLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

// listFilterCapture implements type-to-filter for a list panel. "/" starts
// filtering explicitly; typing any other character starts it implicitly.
// While filtering, every printable key goes to the filter (including keys
// like q or W that are otherwise global shortcuts).
func listFilterCapture(text *string, active *bool, render func()) func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if *text != "" {
				*text = trimLastRune(*text)
				render()
				return nil
			}
			if *active {
				*active = false
				render()
				return nil
			}
		case tcell.KeyEscape:
			if *active || *text != "" {
				*text, *active = "", false
				render()
				return nil
			}
		case tcell.KeyEnter:
			*active = false
		case tcell.KeyRune:
			r := event.Rune()
			if r == '/' && !*active {
				*active = true
				render()
				return nil
			}
			if r != ' ' || *active {
				*text += string(r)
				*active = true
				render()
				return nil
			}
		}
		return event
	}
}

func setupKeybindings() {
	s := state.S

	s.DbList.SetInputCapture(listFilterCapture(&s.DbFilterText, &s.DbFilterActive, documents.RenderDBList))
	s.CollList.SetInputCapture(listFilterCapture(&s.CollFilterText, &s.CollFilterActive, documents.RenderCollList))

	// ── Document list ──
	s.DocList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == ' ' {
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
		if event.Key() != tcell.KeyRune {
			return event
		}
		switch event.Rune() {
		case 'e':
			documents.EditDocument()
		case 'c':
			documents.CopyToClipboard()
		case 't':
			documents.ToggleToTree()
		case '/':
			documents.StartDocSearch()
		default:
			return event
		}
		return nil
	})

	// ── Document tree view ──
	s.DocTree.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			documents.CollapseDocument()
			return nil
		}
		if event.Key() != tcell.KeyRune {
			return event
		}
		switch event.Rune() {
		case 'e':
			documents.EditDocument()
		case 'c':
			documents.CopyToClipboard()
		case 't':
			documents.ToggleToDetail()
		default:
			return event
		}
		return nil
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
		if event.Key() == tcell.KeyRune && event.Rune() == 'r' {
			documents.RefreshStats(true)
			return nil
		}
		return event
	})

	// ── Help view ──
	s.HelpView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Rune() == '?' {
			s.RootPages.SwitchToPage("main")
			state.RefreshPanelBorders()
			return nil
		}
		return event
	})

	// ── Global key bindings ──
	s.App.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// Modals, help, and the sort form handle their own keys; so does any
		// focused text input (query bar, search bar).
		if front, _ := s.RootPages.GetFrontPage(); front != "main" {
			return event
		}
		focused := s.App.GetFocus()
		if _, ok := focused.(*tview.InputField); ok {
			return event
		}
		filtering := (focused == s.DbList && s.DbFilterActive) || (focused == s.CollList && s.CollFilterActive)

		switch event.Key() {
		case tcell.KeyTab:
			state.CycleFocus(true)
			return nil
		case tcell.KeyBacktab:
			state.CycleFocus(false)
			return nil
		case tcell.KeyRune:
		default:
			return event
		}
		if filtering {
			return event
		}

		// Global commands that work from any panel
		switch event.Rune() {
		case '?':
			s.RootPages.SwitchToPage("help")
			s.App.SetFocus(s.HelpView)
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
			if s.LiveMode {
				documents.StopLiveMode()
			} else {
				documents.StartLiveMode()
			}
			return nil
		case 'i':
			if focused == s.DocList || focused == s.DocDetail || focused == s.DocTree {
				documents.InsertDocument()
				return nil
			}
		}

		switch focused {
		case s.DocDetail, s.DocTree, s.DbList:
			return event
		case s.CollList:
			switch event.Rune() {
			case 'I':
				if collName := state.GetSelectedCollName(); s.CurrentDB != "" && collName != "" {
					documents.ShowIndexesFor(state.Namespace{DB: s.CurrentDB, Coll: collName})
				}
				return nil
			case 'D':
				documents.DropCollectionConfirm()
				return nil
			}
			return event
		case s.IndexList:
			if event.Rune() == 'X' {
				documents.DropIndexConfirm()
				return nil
			}
		}

		switch event.Rune() {
		case '/':
			s.App.SetFocus(s.SearchBar)
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

		if focused != s.DocList {
			return event
		}
		switch event.Rune() {
		case 'd':
			if len(s.CurrentDocs) > 0 {
				documents.DeleteDocumentConfirm()
			}
		case 'e':
			documents.EditDocument()
		case 'x':
			if len(s.CurrentDocs) > 0 {
				documents.ExportJSON()
			}
		case 'C':
			if len(s.CurrentDocs) > 0 {
				documents.ExportCSV()
			}
		case 's':
			if s.CurrentDB != "" && s.CurrentColl != "" && s.ResultLabel == "" {
				documents.PromptSort()
			}
		case 'a':
			documents.SelectAll()
		case 'A':
			documents.ClearSelection()
			documents.RefreshDocListSelection()
		case 'n':
			documents.NextPage()
		case 'p':
			documents.PrevPage()
		default:
			return event
		}
		return nil
	})
}
