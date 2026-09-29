package documents

import (
	"fmt"

	"tango/internal/state"
)

func StartDocSearch() {
	s := state.S
	if len(s.DetailTokens) == 0 || s.DocSearchActive {
		return
	}
	s.DocSearchActive = true
	s.DocSearchBar.SetLabel(" Search: ")
	s.DocSearchBar.SetText("")
	s.DocDetailFlex.AddItem(s.DocSearchBar, 1, 0, false)
	s.App.SetFocus(s.DocSearchBar)
}

// EndDocSearch hides the search bar; clearHighlight also removes highlights.
func EndDocSearch(clearHighlight bool) {
	s := state.S
	if !s.DocSearchActive {
		return
	}
	s.DocSearchActive = false
	s.DocDetailFlex.RemoveItem(s.DocSearchBar)
	if clearHighlight {
		ApplyDocSearchHighlight("")
	}
	s.App.SetFocus(s.DocDetail)
	state.RefreshPanelBorders()
}

// ApplyDocSearchHighlight re-renders the detail view highlighting term and
// scrolls to the first match.
func ApplyDocSearchHighlight(term string) {
	s := state.S
	if len(s.DetailTokens) == 0 {
		return
	}
	s.DocSearchTerm = term
	text, n := state.RenderTokens(s.DetailTokens, term)
	s.DocDetail.SetText(text)
	if term == "" {
		s.DocSearchBar.SetLabel(" Search: ")
		s.DocDetail.Highlight()
		return
	}
	s.DocSearchBar.SetLabel(fmt.Sprintf(" Search (%d): ", n))
	if n > 0 {
		s.DocDetail.Highlight("m0").ScrollToHighlight()
	} else {
		s.DocDetail.Highlight()
	}
}
