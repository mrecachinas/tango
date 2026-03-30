package ui

import "tango/internal/state"

func ToggleWideMode() {
	s := state.S
	s.WideMode = !s.WideMode
	applyWideMode()
}

func applyWideMode() {
	s := state.S
	s.PanelsFlex.Clear()
	if s.WideMode {
		s.PanelsFlex.AddItem(s.DocPages, 0, 1, true)
		// Show the right page based on current navigation state
		if s.CurrentColl != "" {
			s.DocPages.SwitchToPage("list")
			s.App.SetFocus(s.DocList)
		} else if s.CurrentDB != "" {
			s.DocPages.SwitchToPage("collections")
			s.App.SetFocus(s.CollList)
		} else {
			s.DocPages.SwitchToPage("databases")
			s.App.SetFocus(s.DbList)
		}
	} else {
		s.PanelsFlex.AddItem(s.DbList, 0, 1, true)
		s.PanelsFlex.AddItem(s.CollList, 0, 1, false)
		s.PanelsFlex.AddItem(s.DocPages, 0, 3, false)
		// Ensure docPages shows documents, not db/coll lists
		s.DocPages.SwitchToPage("list")
	}
	// Update doc list title to show/hide breadcrumb
	if s.CurrentColl != "" {
		s.DocList.SetTitle(state.FormatDocListTitle())
	}
	state.RefreshPanelBorders()
}
