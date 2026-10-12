package documents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
	"tango/internal/state"
)

func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func confirm(id, text, expected string, onYes func()) {
	state.S.Confirm = &state.Confirmation{ID: id, Question: text, Expected: expected, OnYes: onYes}
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// DeleteDocumentConfirm asks to delete the selected documents (or the one
// under the cursor). The _ids and namespace are captured now, so reloads
// while the prompt is open can't change what gets deleted.
func DeleteDocumentConfirm() {
	s := state.S
	if !s.DocsDeletable {
		state.ShowError("These results aren't stored documents (e.g. aggregation output), so they can't be deleted from here.")
		return
	}
	ns := s.DocsNS
	if !state.CheckWrite(state.ActRemove, ns) {
		return
	}
	var docs []bson.D
	if len(s.SelectedDocs) > 0 {
		docs = selectedDocsSlice()
	} else if idx := s.Cursor; idx >= 0 && idx < len(s.CurrentDocs) {
		docs = []bson.D{s.CurrentDocs[idx]}
	}
	if len(docs) == 0 {
		return
	}
	ids := make(bson.A, 0, len(docs))
	for _, d := range docs {
		id, ok := mjson.Get(d, "_id")
		if !ok {
			state.ShowError("Document has no _id; can't delete it safely.")
			return
		}
		ids = append(ids, id)
	}
	text := fmt.Sprintf("Delete %d selected documents from %s?", len(ids), ns)
	if len(ids) == 1 {
		text = fmt.Sprintf("Delete this document from %s?\n_id: %s", ns, truncate(mjson.Compact(ids[0]), 200))
	}
	confirm("delete", text, "", func() { deleteByIDs(ns, ids) })
}

func deleteByIDs(ns state.Namespace, ids bson.A) {
	s := state.S
	coll := s.Client.Database(ns.DB).Collection(ns.Coll)
	state.Async("Deleting", func(ctx context.Context) func() {
		res, err := coll.DeleteMany(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}})
		return func() {
			if err != nil {
				state.FlashError("Delete failed: " + err.Error())
				return
			}
			if int(res.DeletedCount) < len(ids) {
				state.FlashStatus(fmt.Sprintf("Deleted %d of %d documents (the rest no longer existed)", res.DeletedCount, len(ids)))
			} else {
				state.FlashStatus(fmt.Sprintf("Deleted %d document(s)", res.DeletedCount))
			}
			if s.DocsNS != ns {
				return
			}
			ClearSelection()
			if s.ResultLabel != "" {
				keys := map[string]bool{}
				for _, id := range ids {
					keys[mjson.Compact(id)] = true
				}
				removeDocsByID(keys)
				return
			}
			s.CountDirty = true
			ReloadDocuments()
		}
	})
}

// currentDoc returns the document an edit applies to: the one open in the
// detail/tree view, else the one under the list cursor.
func currentDoc() (bson.D, state.Namespace, bool) {
	s := state.S
	if (s.View == "detail" || s.View == "tree") && s.ExpandedDoc != nil {
		return s.ExpandedDoc, s.ExpandedNS, s.ExpandedEditable
	}
	if idx := s.Cursor; idx >= 0 && idx < len(s.CurrentDocs) {
		return s.CurrentDocs[idx], s.DocsNS, s.DocsEditable
	}
	return nil, state.Namespace{}, false
}

// runEditor prepares an ExecProcess request; Done is called by the root on the
// UI goroutine after Bubble Tea restores the terminal.
func runEditor(text string, done func(string, error)) {
	tmp, err := os.CreateTemp("", "tango-*.json")
	if err != nil {
		done("", fmt.Errorf("create editor file: %w", err))
		return
	}
	path := tmp.Name()
	if _, err := tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		done("", fmt.Errorf("write editor file: %w", err))
		return
	}
	_ = tmp.Close()

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	state.S.Editor = &state.EditorRequest{
		Command: exec.Command("sh", "-c", editor+" "+shellescape(path)),
		Done: func(err error) {
			defer func() { _ = os.Remove(path) }()
			if err != nil {
				done("", fmt.Errorf("editor %q failed: %w", editor, err))
				return
			}
			out, err := os.ReadFile(path)
			done(string(out), err)
		},
	}
}

// editText runs the editor on text and parses the result, offering to
// re-open the editor (with the user's text intact) when it doesn't parse.
func editText(text, original string, onDoc func(bson.D)) {
	runEditor(text, func(edited string, err error) {
		if err != nil {
			state.ShowError(err.Error())
			return
		}
		if original != "" && strings.TrimSpace(edited) == strings.TrimSpace(original) {
			state.FlashStatus("No changes")
			return
		}
		d, err := mjson.ParseDoc(edited)
		if err != nil {
			confirm("re-edit", "Invalid document:\n"+err.Error()+"\nRe-edit?", "", func() {
				editText(edited, original, onDoc)
			})
			return
		}
		onDoc(d)
	})
}

func EditDocument() {
	doc, ns, editable := currentDoc()
	if doc == nil {
		state.ShowError("No document selected")
		return
	}
	if !editable {
		state.ShowError("This document can't be edited here: it comes from an aggregation or was loaded with a projection.\nOpen it from a find() without a projection to edit it.")
		return
	}
	if !state.CheckWrite(state.ActUpdate, ns) {
		return
	}
	id, ok := mjson.Get(doc, "_id")
	if !ok {
		state.ShowError("Document has no _id; can't update it safely.")
		return
	}
	original := mjson.Pretty(doc)
	editText(original, original, func(newDoc bson.D) {
		if newID, ok := mjson.Get(newDoc, "_id"); ok && mjson.Compact(newID) != mjson.Compact(id) {
			state.ShowError("Changing _id isn't supported. Insert a new document instead.")
			return
		}
		repl := make(bson.D, 0, len(newDoc))
		for _, e := range newDoc {
			if e.Key != "_id" {
				repl = append(repl, e)
			}
		}
		saveDocument(ns, id, repl)
	})
}

func saveDocument(ns state.Namespace, id interface{}, repl bson.D) {
	s := state.S
	coll := s.Client.Database(ns.DB).Collection(ns.Coll)
	filter := bson.D{{Key: "_id", Value: id}}
	state.Async("Saving document", func(ctx context.Context) func() {
		res, err := coll.ReplaceOne(ctx, filter, repl)
		var updated bson.D
		if err == nil && res.MatchedCount > 0 {
			_ = coll.FindOne(ctx, filter).Decode(&updated)
		}
		return func() {
			if err != nil {
				state.ShowError("Failed to save document: " + err.Error())
				return
			}
			if res.MatchedCount == 0 {
				state.ShowError("The document no longer exists (it may have been deleted); nothing was saved.")
				return
			}
			state.FlashStatus("Document updated")
			if updated != nil && s.ExpandedNS == ns {
				if s.View == "detail" || s.View == "tree" {
					showDocument(updated, ns, true)
				}
			}
			if s.DocsNS != ns {
				return
			}
			if updated != nil {
				replaceDocByID(updated)
			}
			if s.ResultLabel == "" {
				ReloadDocuments()
			}
		}
	})
}

// replaceDocByID updates a document in the loaded result set in place.
func replaceDocByID(doc bson.D) {
	s := state.S
	key, ok := idKey(doc)
	if !ok {
		return
	}
	for i, d := range s.CurrentDocs {
		if k, ok := idKey(d); ok && k == key {
			s.CurrentDocs[i] = doc
			s.DocsRevision++
		}
	}
}

func InsertDocument() {
	s := state.S
	ns := state.Namespace{DB: s.CurrentDB, Coll: s.CurrentColl}
	if s.DocsNS.Coll != "" {
		ns = s.DocsNS // the collection whose documents are on screen
	}
	if ns.DB == "" || ns.Coll == "" {
		state.ShowError("Select a collection first")
		return
	}
	if !state.CheckWrite(state.ActInsert, ns) {
		return
	}
	editText("{\n  \n}\n", "", func(d bson.D) {
		if len(d) == 0 {
			state.FlashStatus("Empty document — nothing inserted")
			return
		}
		coll := s.Client.Database(ns.DB).Collection(ns.Coll)
		state.Async("Inserting document", func(ctx context.Context) func() {
			res, err := coll.InsertOne(ctx, d)
			return func() {
				if err != nil {
					state.ShowError("Insert failed: " + err.Error())
					return
				}
				state.FlashStatus("Inserted document " + truncate(mjson.Compact(res.InsertedID), 80) + " into " + ns.String())
				if s.DocsNS == ns && s.ResultLabel == "" {
					s.CountDirty = true
					ReloadDocuments()
				}
			}
		})
	})
}
