package documents

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "..", "_")
	return s
}

func ExportJSON() {
	s := state.S
	filename := "export.json"
	if s.CurrentDB != "" && s.CurrentColl != "" {
		filename = sanitizeFilename(s.CurrentDB) + "." + sanitizeFilename(s.CurrentColl) + ".json"
	}

	docs := s.CurrentDocs
	if len(s.SelectedDocs) > 0 {
		docs = selectedDocsSlice()
	}

	data, err := json.MarshalIndent(docs, "", "  ")
	if err != nil {
		state.ShowError(fmt.Sprintf("JSON marshal error: %v", err))
		return
	}

	if err := os.WriteFile(filename, data, 0600); err != nil {
		state.ShowError(fmt.Sprintf("Write error: %v", err))
		return
	}

	msg := fmt.Sprintf("[green]Exported %d docs to %s", len(docs), filename)
	original := s.StatusBar.GetText(false)
	s.StatusBar.SetText(msg)
	go func() {
		time.Sleep(2 * time.Second)
		s.App.QueueUpdateDraw(func() {
			s.StatusBar.SetText(original)
		})
	}()
}

func ExportCSV() {
	s := state.S
	filename := "export.csv"
	if s.CurrentDB != "" && s.CurrentColl != "" {
		filename = sanitizeFilename(s.CurrentDB) + "." + sanitizeFilename(s.CurrentColl) + ".csv"
	}

	docs := s.CurrentDocs
	if len(s.SelectedDocs) > 0 {
		docs = selectedDocsSlice()
	}

	// Flatten all docs and collect unique column names
	flattened := make([]map[string]string, len(docs))
	colSet := make(map[string]bool)
	for i, doc := range docs {
		flat := flattenDoc("", doc)
		flattened[i] = flat
		for k := range flat {
			colSet[k] = true
		}
	}

	// Sort columns alphabetically, but _id first
	var columns []string
	hasID := false
	for k := range colSet {
		if k == "_id" {
			hasID = true
			continue
		}
		columns = append(columns, k)
	}
	sort.Strings(columns)
	if hasID {
		columns = append([]string{"_id"}, columns...)
	}

	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		state.ShowError(fmt.Sprintf("CSV create error: %v", err))
		return
	}
	defer func() { _ = f.Close() }()

	w := csv.NewWriter(f)
	if err := w.Write(columns); err != nil {
		state.ShowError(fmt.Sprintf("CSV write error: %v", err))
		return
	}

	for _, flat := range flattened {
		row := make([]string, len(columns))
		for j, col := range columns {
			row[j] = flat[col]
		}
		if err := w.Write(row); err != nil {
			state.ShowError(fmt.Sprintf("CSV write error: %v", err))
			return
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		state.ShowError(fmt.Sprintf("CSV flush error: %v", err))
		return
	}

	msg := fmt.Sprintf("[green]Exported %d docs to %s", len(docs), filename)
	original := s.StatusBar.GetText(false)
	s.StatusBar.SetText(msg)
	go func() {
		time.Sleep(2 * time.Second)
		s.App.QueueUpdateDraw(func() {
			s.StatusBar.SetText(original)
		})
	}()
}

func flattenDoc(prefix string, doc bson.M) map[string]string {
	result := make(map[string]string)
	for k, v := range doc {
		fullKey := prefix + k
		switch val := v.(type) {
		case bson.M:
			for fk, fv := range flattenDoc(fullKey+".", val) {
				result[fk] = fv
			}
		default:
			if v == nil {
				result[fullKey] = ""
				continue
			}
			rt := fmt.Sprintf("%T", v)
			if rt == "bson.A" || strings.HasPrefix(rt, "[]") {
				b, err := json.Marshal(v)
				if err != nil {
					result[fullKey] = fmt.Sprintf("%v", v)
				} else {
					result[fullKey] = string(b)
				}
			} else {
				result[fullKey] = fmt.Sprintf("%v", v)
			}
		}
	}
	return result
}

func CopyToClipboard() {
	s := state.S
	var text string
	name, _ := s.DocPages.GetFrontPage()
	if name == "tree" {
		idx := s.DocList.GetCurrentItem()
		if idx >= 0 && idx < len(s.CurrentDocs) {
			jsonBytes, err := json.MarshalIndent(s.CurrentDocs[idx], "", "  ")
			if err != nil {
				s.StatusBar.SetText(" [red]Clipboard error: " + err.Error())
				return
			}
			text = string(jsonBytes)
		}
	} else {
		text = s.DocDetail.GetText(true)
	}
	cmd := exec.Command("pbcopy")
	pipe, err := cmd.StdinPipe()
	if err != nil {
		s.StatusBar.SetText(" [red]Clipboard error: " + err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		s.StatusBar.SetText(" [red]Clipboard error: " + err.Error())
		return
	}
	_, _ = pipe.Write([]byte(text))
	_ = pipe.Close()
	if err := cmd.Wait(); err != nil {
		s.StatusBar.SetText(" [red]Clipboard error: " + err.Error())
		return
	}
	prev := s.StatusBar.GetText(false)
	s.StatusBar.SetText(" [green]Copied to clipboard!")
	go func() {
		time.Sleep(2 * time.Second)
		s.App.QueueUpdateDraw(func() {
			s.StatusBar.SetText(prev)
		})
	}()
}
