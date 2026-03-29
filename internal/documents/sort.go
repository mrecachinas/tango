package documents

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tango/internal/state"
)

func PromptSort() {
	s := state.S
	s.SortForm.Clear(true)

	initialOrder := 0
	if s.CurrentSortOrder == -1 {
		initialOrder = 1
	}

	s.SortForm.
		AddInputField("Sort field", s.CurrentSortField, 30, nil, nil).
		AddDropDown("Order", []string{"Ascending", "Descending"}, initialOrder, nil).
		AddButton("Sort", func() {
			field := s.SortForm.GetFormItemByLabel("Sort field").(*tview.InputField).GetText()
			field = strings.TrimSpace(field)
			if field == "" {
				s.RootPages.SwitchToPage("main")
				s.App.SetFocus(s.DocList)
				return
			}
			_, option := s.SortForm.GetFormItemByLabel("Order").(*tview.DropDown).GetCurrentOption()
			s.CurrentSortField = field
			s.CurrentSortOrder = 1
			if option == "Descending" {
				s.CurrentSortOrder = -1
			}
			s.CurrentPage = 0
			s.RootPages.SwitchToPage("main")
			LoadDocuments()
		}).
		AddButton("Clear", func() {
			s.CurrentSortField = ""
			s.CurrentSortOrder = 0
			s.CurrentPage = 0
			s.RootPages.SwitchToPage("main")
			LoadDocuments()
		}).
		AddButton("Cancel", func() {
			s.RootPages.SwitchToPage("main")
			s.App.SetFocus(s.DocList)
		})

	s.SortForm.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			s.RootPages.SwitchToPage("main")
			s.App.SetFocus(s.DocList)
			return nil
		}
		return event
	})

	s.RootPages.SwitchToPage("sort")
	s.App.SetFocus(s.SortForm)
}
