package documents

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

func TestCurrentDocIgnoresHiddenListCursorOutsideDocumentViews(t *testing.T) {
	oldState := state.S
	t.Cleanup(func() { state.S = oldState })

	listDoc := bson.D{{Key: "_id", Value: "hidden"}}
	expanded := bson.D{{Key: "_id", Value: "shown"}}
	for _, tc := range []struct {
		view     string
		expanded bson.D
		want     bson.D
	}{
		{view: "list", want: listDoc},
		{view: "tree", expanded: expanded, want: expanded},
		{view: "detail", expanded: expanded, want: expanded},
		{view: "detail"},
		{view: "tree"},
		{view: "indexes"},
		{view: "stats"},
	} {
		state.S = &state.AppState{
			View:             tc.view,
			CurrentDocs:      []bson.D{listDoc},
			DocsNS:           users,
			DocsEditable:     true,
			ExpandedDoc:      tc.expanded,
			ExpandedNS:       users,
			ExpandedEditable: true,
		}
		got, _, _ := currentDoc()
		if (got == nil) != (tc.want == nil) || (got != nil && got[0].Value != tc.want[0].Value) {
			t.Errorf("view %q (expanded=%v): got %v, want %v", tc.view, tc.expanded != nil, got, tc.want)
		}
	}
}

func TestDeleteDocumentConfirmOnlyFromList(t *testing.T) {
	oldState := state.S
	t.Cleanup(func() { state.S = oldState })

	doc := bson.D{{Key: "_id", Value: "hidden"}}
	for _, view := range []string{"detail", "tree", "indexes", "stats", "list"} {
		state.S = &state.AppState{
			View:          view,
			CurrentDocs:   []bson.D{doc},
			DocsNS:        users,
			DocsDeletable: true,
			ExpandedDoc:   doc,
			ExpandedNS:    users,
		}
		DeleteDocumentConfirm()
		if view == "list" {
			if state.S.Confirm == nil || state.S.Confirm.ID != "delete" {
				t.Errorf("list view: expected a delete confirmation, got %+v", state.S.Confirm)
			}
			continue
		}
		if state.S.Confirm != nil {
			t.Errorf("view %q: delete prompted for the hidden list document", view)
		}
		if flash, _, _ := state.Status(); flash == "" {
			t.Errorf("view %q: expected a status message", view)
		}
	}
}

func TestEditDocumentInNonDocumentDetailDoesNotOpenEditor(t *testing.T) {
	oldState := state.S
	t.Cleanup(func() { state.S = oldState })

	state.S = &state.AppState{
		View:         "list",
		CurrentDocs:  []bson.D{{{Key: "_id", Value: "hidden"}}},
		DocsNS:       users,
		DocsEditable: true,
	}
	state.ShowMessage(" Index ", "an index detail")
	EditDocument()
	if state.S.Editor != nil {
		t.Fatal("edit opened the editor for the hidden list document")
	}
	if state.S.DetailTitle != " Error " {
		t.Errorf("expected an error message, got title %q", state.S.DetailTitle)
	}
}
