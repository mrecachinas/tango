package documents

import (
	"strings"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

func SetSort(spec bson.D) {
	s := state.S
	s.CurrentSort = spec
	s.CurrentPage = 0
	s.PageStarts = nil
	s.SnapshotID = nil
	LoadDocuments()
}

func PromptSort() {
	s := state.S
	fields := make([]string, 0, len(s.CurrentSort))
	for _, e := range s.CurrentSort {
		name := e.Key
		if v, ok := state.ToFloat64(e.Value); ok && v < 0 {
			name = "-" + name
		}
		fields = append(fields, name)
	}
	s.PromptKind, s.PromptText = "command", "sort "+strings.Join(fields, " ")
}
