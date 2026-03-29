package documents

import (
	"fmt"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

func ToggleSelection(idx int) {
	s := state.S
	if s.SelectedDocs[idx] {
		delete(s.SelectedDocs, idx)
	} else {
		s.SelectedDocs[idx] = true
	}
	RefreshDocListSelection()
}

func RefreshDocListSelection() {
	s := state.S
	for i := 0; i < s.DocList.GetItemCount(); i++ {
		main, _ := s.DocList.GetItemText(i)
		main = strings.TrimPrefix(main, "✓ ")
		if s.SelectedDocs[i] {
			s.DocList.SetItemText(i, "✓ "+main, "")
		} else {
			s.DocList.SetItemText(i, main, "")
		}
	}
	updateDocListTitle()
}

func updateDocListTitle() {
	state.S.DocList.SetTitle(state.FormatDocListTitle())
}

func ClearSelection() {
	state.S.SelectedDocs = make(map[int]bool)
}

func selectedDocsSlice() []bson.M {
	s := state.S
	indices := make([]int, 0, len(s.SelectedDocs))
	for idx := range s.SelectedDocs {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	docs := make([]bson.M, 0, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(s.CurrentDocs) {
			docs = append(docs, s.CurrentDocs[idx])
		}
	}
	return docs
}

// UpdateDocListTitle is the exported version for cross-package use.
func UpdateDocListTitle() {
	s := state.S
	totalPages := (s.TotalDocs + int64(state.PageSize) - 1) / int64(state.PageSize)
	if totalPages == 0 {
		totalPages = 1
	}
	title := fmt.Sprintf(" Documents [yellow](%s.%s)[white] [green][%d/%d][white] %d total",
		s.CurrentDB, s.CurrentColl, s.CurrentPage+1, totalPages, s.TotalDocs)
	if s.CurrentSortField != "" {
		arrow := "↑"
		if s.CurrentSortOrder == -1 {
			arrow = "↓"
		}
		title += fmt.Sprintf(" [aqua]⇅ %s %s[white]", s.CurrentSortField, arrow)
	}
	if n := len(s.SelectedDocs); n > 0 {
		title += fmt.Sprintf(" [blue]%d selected[white]", n)
	}
	title += " "
	s.DocList.SetTitle(title)
}
