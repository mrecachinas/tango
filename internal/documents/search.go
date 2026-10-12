package documents

import "tango/internal/state"

func StartDocSearch() {
	s := state.S
	if len(s.DetailTokens) == 0 {
		return
	}
	s.View, s.Focus = "detail", "content"
	s.DocSearchActive = true
	s.PromptKind, s.PromptText = "search", s.DocSearchTerm
}

func EndDocSearch(clearHighlight bool) {
	s := state.S
	s.DocSearchActive = false
	if clearHighlight {
		s.DocSearchTerm = ""
	}
}

func ApplyDocSearchHighlight(term string) { state.S.DocSearchTerm = term }
