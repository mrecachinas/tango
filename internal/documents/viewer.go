package documents

import (
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/mjson"
	"tango/internal/state"
)

func DocTitle(doc bson.D, _ state.Namespace, viewLabel string) string {
	id, ok := mjson.Get(doc, "_id")
	if !ok {
		return viewLabel
	}
	idStr := mjson.Compact(id)
	switch v := id.(type) {
	case primitive.ObjectID:
		idStr = v.Hex()
	case string:
		idStr = v
	}
	return truncate(idStr, 80)
}

func ExpandDocument(index int) {
	s := state.S
	if index < 0 || index >= len(s.CurrentDocs) {
		return
	}
	showDocument(s.CurrentDocs[index], s.DocsNS, s.DocsEditable)
}

func showDocument(doc bson.D, ns state.Namespace, editable bool) {
	s := state.S
	state.SetDetail(DocTitle(doc, ns, "Document"), mjson.Tokens(doc, true))
	s.ExpandedDoc, s.ExpandedNS, s.ExpandedEditable = doc, ns, editable
	s.View = "tree"
}

func CollapseDocument() {
	s := state.S
	EndDocSearch(true)
	s.View = s.DetailBackPage
	if s.View == "" {
		s.View = "list"
	}
	s.Focus = "content"
}

func ToggleToTree() {
	if state.S.ExpandedDoc == nil {
		return
	}
	EndDocSearch(true)
	state.S.View, state.S.Focus = "tree", "content"
}

func ToggleToDetail() {
	if state.S.ExpandedDoc == nil {
		return
	}
	state.S.View, state.S.Focus = "detail", "content"
}
