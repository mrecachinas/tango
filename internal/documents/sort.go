package documents

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

// SetSort applies a sort spec (nil clears it) and reloads from page one.
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
	s.SortForm.Clear(true)

	field, initialOrder := "", 0
	if len(s.CurrentSort) > 0 {
		field = s.CurrentSort[0].Key
		if v, ok := state.ToFloat64(s.CurrentSort[0].Value); ok && v < 0 {
			initialOrder = 1
		}
	}
	back := func() {
		s.RootPages.SwitchToPage("main")
		s.App.SetFocus(s.DocList)
		state.RefreshPanelBorders()
	}

	s.SortForm.
		AddInputField("Sort field", field, 30, nil, nil).
		AddDropDown("Order", []string{"Ascending", "Descending"}, initialOrder, nil).
		AddButton("Sort", func() {
			f := strings.TrimSpace(s.SortForm.GetFormItemByLabel("Sort field").(*tview.InputField).GetText())
			back()
			if f == "" {
				return
			}
			_, option := s.SortForm.GetFormItemByLabel("Order").(*tview.DropDown).GetCurrentOption()
			order := 1
			if option == "Descending" {
				order = -1
			}
			SetSort(bson.D{{Key: f, Value: order}})
		}).
		AddButton("Clear", func() {
			back()
			SetSort(nil)
		}).
		AddButton("Cancel", back)

	s.SortForm.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			back()
			return nil
		}
		return event
	})

	s.RootPages.SwitchToPage("sort")
	s.App.SetFocus(s.SortForm)
}
