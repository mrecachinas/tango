package documents

import (
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

// SelectAll selects every loaded document.
func SelectAll() {
	s := state.S
	for i := range s.CurrentDocs {
		s.SelectedDocs[i] = true
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
	s.DocList.SetTitle(state.FormatDocListTitle())
	state.UpdateStatusHints()
}

func ClearSelection() {
	state.S.SelectedDocs = make(map[int]bool)
}

func selectedDocsSlice() []bson.D {
	s := state.S
	indices := make([]int, 0, len(s.SelectedDocs))
	for idx := range s.SelectedDocs {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	docs := make([]bson.D, 0, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(s.CurrentDocs) {
			docs = append(docs, s.CurrentDocs[idx])
		}
	}
	return docs
}
