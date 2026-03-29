package documents

import (
	"strings"

	"tango/internal/state"
)

func StartDocSearch() {
	s := state.S
	s.DocSearchOriginal = s.DocDetail.GetText(true)
	s.DocSearchActive = true
	s.DocSearchBar.SetText("")
	s.DocDetailFlex.AddItem(s.DocSearchBar, 1, 0, false)
	s.App.SetFocus(s.DocSearchBar)
}

func EndDocSearch(clearHighlight bool) {
	s := state.S
	if !s.DocSearchActive {
		return
	}
	s.DocSearchActive = false
	s.DocDetailFlex.RemoveItem(s.DocSearchBar)
	if clearHighlight && s.DocSearchOriginal != "" {
		s.DocDetail.SetText(s.DocSearchOriginal)
	}
	s.DocSearchOriginal = ""
	s.App.SetFocus(s.DocDetail)
	state.RefreshPanelBorders()
}

func ApplyDocSearchHighlight(term string) {
	s := state.S
	if s.DocSearchOriginal == "" {
		return
	}
	if term == "" {
		s.DocDetail.SetText(s.DocSearchOriginal)
		return
	}
	s.DocDetail.SetText(highlightSearchMatches(s.DocSearchOriginal, term))
}

// highlightSearchMatches wraps case-insensitive matches of term in the
// tview-colored text with a yellow-background highlight, being careful
// not to match inside tview color tags like [cyan] or [white].
func highlightSearchMatches(colored, term string) string {
	if term == "" {
		return colored
	}
	lowerTerm := strings.ToLower(term)
	var result strings.Builder
	i := 0
	for i < len(colored) {
		// Skip tview color tags (e.g. [cyan], [white], [-:-])
		if colored[i] == '[' {
			end := strings.IndexByte(colored[i:], ']')
			if end != -1 {
				result.WriteString(colored[i : i+end+1])
				i += end + 1
				continue
			}
		}
		// Try to match term at this position (case-insensitive),
		// but only against visible (non-tag) characters.
		matched, matchEnd := matchVisibleChars(colored, i, lowerTerm)
		if matched {
			// Extract the matched segment (may contain color tags)
			result.WriteString("[black:yellow]")
			result.WriteString(colored[i:matchEnd])
			result.WriteString("[-:-]")
			i = matchEnd
			continue
		}
		result.WriteByte(colored[i])
		i++
	}
	return result.String()
}

// matchVisibleChars checks if the visible characters starting at pos in s
// match lowerTerm (case-insensitive), skipping over tview color tags.
// Returns (true, endIndex) if matched, (false, 0) otherwise.
func matchVisibleChars(s string, pos int, lowerTerm string) (bool, int) {
	si := pos
	ti := 0
	for ti < len(lowerTerm) && si < len(s) {
		// Skip color tags
		if s[si] == '[' {
			end := strings.IndexByte(s[si:], ']')
			if end != -1 {
				si += end + 1
				continue
			}
		}
		if strings.ToLower(string(s[si]))[0] != lowerTerm[ti] {
			return false, 0
		}
		si++
		ti++
	}
	if ti == len(lowerTerm) {
		return true, si
	}
	return false, 0
}
