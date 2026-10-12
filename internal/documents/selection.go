package documents

import (
	"sort"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

func ToggleSelection(idx int) {
	s := state.S
	if idx < 0 || idx >= len(s.CurrentDocs) {
		return
	}
	if s.SelectedDocs == nil {
		s.SelectedDocs = map[int]bool{}
	}
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
	if s.SelectedDocs == nil {
		s.SelectedDocs = map[int]bool{}
	}
	for i := range s.CurrentDocs {
		s.SelectedDocs[i] = true
	}
	RefreshDocListSelection()
}

func RefreshDocListSelection() {
	// The root syncs selection after every message without rebuilding rows.
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
