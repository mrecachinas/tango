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
		s.App.SetFocus(state.ActiveDocPanel())
	} else {
		s.PanelsFlex.AddItem(s.DbList, 0, 1, true)
		s.PanelsFlex.AddItem(s.CollList, 0, 1, false)
		s.PanelsFlex.AddItem(s.DocPages, 0, 3, false)
	}
	// Update doc list title to show/hide breadcrumb
	if s.CurrentColl != "" {
		s.DocList.SetTitle(state.FormatDocListTitle())
	}
	state.RefreshPanelBorders()
}
