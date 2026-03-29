package documents

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func DeleteDocumentConfirm() {
	if !state.CheckWriteAccess() {
		return
	}
	s := state.S
	if len(s.SelectedDocs) > 0 {
		n := len(s.SelectedDocs)
		s.ConfirmModal.
			SetText(fmt.Sprintf("Delete %d selected documents?", n)).
			AddButtons([]string{"Delete", "Cancel"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				if buttonLabel == "Delete" {
					deleteBatchDocuments()
				}
				s.RootPages.HidePage("confirm")
				s.RootPages.SwitchToPage("main")
				s.App.SetFocus(s.DocList)
			})
		s.RootPages.ShowPage("confirm")
		s.App.SetFocus(s.ConfirmModal)
		return
	}
	idx := s.DocList.GetCurrentItem()
	if idx < 0 || idx >= len(s.CurrentDocs) {
		return
	}
	doc := s.CurrentDocs[idx]
	idVal := doc["_id"]
	s.ConfirmModal.
		SetText(fmt.Sprintf("Delete this document?\n_id: %v", idVal)).
		AddButtons([]string{"Delete", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Delete" {
				deleteDocument(idx)
			}
			s.RootPages.HidePage("confirm")
			s.RootPages.SwitchToPage("main")
			s.App.SetFocus(s.DocList)
		})
	s.RootPages.ShowPage("confirm")
	s.App.SetFocus(s.ConfirmModal)
}

func deleteBatchDocuments() {
	if !state.CheckWriteAccess() {
		return
	}
	s := state.S
	state.ShowLoading("Deleting documents")
	defer state.DoneLoading()
	coll := s.Client.Database(s.CurrentDB).Collection(s.CurrentColl)
	var failed int
	for idx := range s.SelectedDocs {
		if idx < 0 || idx >= len(s.CurrentDocs) {
			continue
		}
		doc := s.CurrentDocs[idx]
		ctx, cancel := state.NewCtx()
		_, err := coll.DeleteOne(ctx, bson.M{"_id": doc["_id"]})
		cancel()
		if err != nil {
			failed++
		}
	}
	ClearSelection()
	if failed > 0 {
		s.StatusBar.SetText(fmt.Sprintf(" [yellow]Deleted with %d failures", failed))
	} else {
		s.StatusBar.SetText(" [green]Documents deleted successfully")
	}
	s.CountDirty = true
	LoadDocuments()
}

func deleteDocument(idx int) {
	if !state.CheckWriteAccess() {
		return
	}
	s := state.S
	state.ShowLoading("Deleting document")
	defer state.DoneLoading()
	if idx < 0 || idx >= len(s.CurrentDocs) {
		return
	}
	doc := s.CurrentDocs[idx]
	ctx, cancel := state.NewCtx()
	defer cancel()
	_, err := s.Client.Database(s.CurrentDB).Collection(s.CurrentColl).DeleteOne(ctx, bson.M{"_id": doc["_id"]})
	if err != nil {
		s.StatusBar.SetText(" [red]Delete failed: " + err.Error())
		return
	}
	s.StatusBar.SetText(" [green]Document deleted successfully")
	s.CountDirty = true
	LoadDocuments()
}

func EditDocument() {
	if !state.CheckWriteAccess() {
		return
	}
	s := state.S
	idx := s.DocList.GetCurrentItem()
	if idx < 0 || idx >= len(s.CurrentDocs) {
		state.ShowError("No document selected")
		return
	}
	doc := s.CurrentDocs[idx]

	jsonBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		state.ShowError("Failed to marshal document: " + err.Error())
		return
	}

	tmpFile, err := os.CreateTemp("", "tango-*.json")
	if err != nil {
		state.ShowError("Failed to create temp file: " + err.Error())
		return
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmpFile.Write(jsonBytes); err != nil {
		_ = tmpFile.Close()
		state.ShowError("Failed to write temp file: " + err.Error())
		return
	}
	_ = tmpFile.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	s.App.Suspend(func() {
		cmd := exec.Command("sh", "-c", editor+" "+shellescape(tmpPath))
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	})

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		state.ShowError("Failed to read edited file: " + err.Error())
		return
	}

	var newDoc bson.M
	if err := json.Unmarshal(edited, &newDoc); err != nil {
		state.ShowError("Invalid JSON: " + err.Error())
		return
	}

	// Remove _id from the replacement doc — MongoDB doesn't allow changing it
	delete(newDoc, "_id")

	ctx, cancel := state.NewCtx()
	defer cancel()
	_, err = s.Client.Database(s.CurrentDB).Collection(s.CurrentColl).ReplaceOne(
		ctx, bson.M{"_id": doc["_id"]}, newDoc,
	)
	if err != nil {
		state.ShowError("Failed to save document: " + err.Error())
		return
	}

	s.StatusBar.SetText(" [green]Document updated successfully")
	LoadDocuments()
}

func InsertDocument() {
	if !state.CheckWriteAccess() {
		return
	}
	s := state.S
	tmpFile, err := os.CreateTemp("", "tango-insert-*.json")
	if err != nil {
		state.ShowError(fmt.Sprintf("Failed to create temp file: %v", err))
		return
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmpFile.WriteString("{\n  \n}\n"); err != nil {
		_ = tmpFile.Close()
		state.ShowError(fmt.Sprintf("Failed to write template: %v", err))
		return
	}
	_ = tmpFile.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	s.App.Suspend(func() {
		cmd := exec.Command("sh", "-c", editor+" "+shellescape(tmpPath))
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	})

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		state.ShowError(fmt.Sprintf("Failed to read edited file: %v", err))
		return
	}

	var newDoc bson.M
	if err := json.Unmarshal(data, &newDoc); err != nil {
		state.ShowError(fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if len(newDoc) == 0 {
		return
	}

	ctx, cancel := state.NewCtx()
	defer cancel()

	_, err = s.Client.Database(s.CurrentDB).Collection(s.CurrentColl).InsertOne(ctx, newDoc)
	if err != nil {
		state.ShowError(fmt.Sprintf("Insert failed: %v", err))
		return
	}

	s.DocDetail.SetText("[green]Document inserted successfully.")
	s.DocDetail.SetTitle(" Insert ")
	s.CountDirty = true
	LoadDocuments()
}
