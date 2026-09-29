package documents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
	"tango/internal/state"
)

func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// confirm shows a two-button modal; onYes runs after the modal is closed.
func confirm(modal *tview.Modal, page, text, action string, back tview.Primitive, onYes func()) {
	s := state.S
	modal.ClearButtons()
	modal.SetText(tview.Escape(text)).
		AddButtons([]string{action, "Cancel"}).
		SetDoneFunc(func(_ int, label string) {
			s.RootPages.HidePage(page)
			s.RootPages.SwitchToPage("main")
			if back != nil {
				s.App.SetFocus(back)
			}
			state.RefreshPanelBorders()
			if label == action {
				onYes()
			}
		})
	modal.SetFocus(0)
	s.RootPages.ShowPage(page)
	s.App.SetFocus(modal)
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
	} else if idx := s.DocList.GetCurrentItem(); idx >= 0 && idx < len(s.CurrentDocs) {
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
	confirm(s.ConfirmModal, "confirm", text, "Delete", s.DocList, func() { deleteByIDs(ns, ids) })
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
				state.FlashStatus(fmt.Sprintf(" [yellow]Deleted %d of %d documents (the rest no longer existed)", res.DeletedCount, len(ids)))
			} else {
				state.FlashStatus(fmt.Sprintf(" [green]Deleted %d document(s)", res.DeletedCount))
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
	if name, _ := s.DocPages.GetFrontPage(); (name == "detail" || name == "tree") && s.ExpandedDoc != nil {
		return s.ExpandedDoc, s.ExpandedNS, s.ExpandedEditable
	}
	if idx := s.DocList.GetCurrentItem(); idx >= 0 && idx < len(s.CurrentDocs) {
		return s.CurrentDocs[idx], s.DocsNS, s.DocsEditable
	}
	return nil, state.Namespace{}, false
}

// runEditor opens text in $EDITOR and returns the edited text.
func runEditor(text string) (string, error) {
	s := state.S
	tmp, err := os.CreateTemp("", "tango-*.json")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	path := tmp.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	_ = tmp.Close()

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	var runErr error
	s.App.Suspend(func() {
		cmd := exec.Command("sh", "-c", editor+" "+shellescape(path))
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		runErr = cmd.Run()
	})
	if runErr != nil {
		return "", fmt.Errorf("editor %q failed: %w", editor, runErr)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read edited file: %w", err)
	}
	return string(out), nil
}

// editText runs the editor on text and parses the result, offering to
// re-open the editor (with the user's text intact) when it doesn't parse.
func editText(text, original string, back tview.Primitive, onDoc func(bson.D)) {
	s := state.S
	edited, err := runEditor(text)
	if err != nil {
		state.ShowError(err.Error())
		return
	}
	if original != "" && strings.TrimSpace(edited) == strings.TrimSpace(original) {
		state.FlashStatus(" [gray]No changes")
		return
	}
	d, err := mjson.ParseDoc(edited)
	if err != nil {
		confirm(s.ConfirmModal, "confirm", "Invalid document:\n"+err.Error(), "Re-edit", back, func() {
			editText(edited, original, back, onDoc)
		})
		return
	}
	onDoc(d)
}

func EditDocument() {
	s := state.S
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
	back := s.App.GetFocus()
	editText(original, original, back, func(newDoc bson.D) {
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
			state.FlashStatus(" [green]Document updated")
			if updated != nil && s.ExpandedNS == ns {
				if page, _ := s.DocPages.GetFrontPage(); page == "detail" || page == "tree" {
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
			item := docListItem(doc)
			if s.SelectedDocs[i] {
				item = "✓ " + item
			}
			if i < s.DocList.GetItemCount() {
				s.DocList.SetItemText(i, item, "")
			}
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
	back := s.App.GetFocus()
	editText("{\n  \n}\n", "", back, func(d bson.D) {
		if len(d) == 0 {
			state.FlashStatus(" [gray]Empty document — nothing inserted")
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
				state.FlashStatus(" [green]Inserted document " + tview.Escape(truncate(mjson.Compact(res.InsertedID), 80)) + " into " + tview.Escape(ns.String()))
				if s.DocsNS == ns && s.ResultLabel == "" {
					s.CountDirty = true
					ReloadDocuments()
				}
			}
		})
	})
}
