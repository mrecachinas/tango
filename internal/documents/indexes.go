package documents

import (
	"fmt"
	"time"

	"tango/internal/state"
)

func ShowIndexes() {
	s := state.S
	if s.CurrentDB == "" || s.CurrentColl == "" {
		return
	}

	ctx, cancel := state.NewCtx()
	defer cancel()

	cursor, err := s.Client.Database(s.CurrentDB).Collection(s.CurrentColl).Indexes().List(ctx)
	if err != nil {
		state.ShowError(fmt.Sprintf("Error listing indexes: %s", err))
		return
	}
	defer func() { _ = cursor.Close(ctx) }()

	s.CurrentIndexes = nil
	if err := cursor.All(ctx, &s.CurrentIndexes); err != nil {
		state.ShowError(fmt.Sprintf("Error decoding indexes: %s", err))
		return
	}

	s.IndexList.Clear()
	for _, idx := range s.CurrentIndexes {
		name, _ := idx["name"].(string)
		keySpec := fmt.Sprintf("%v", idx["key"])
		s.IndexList.AddItem(name, keySpec, 0, nil)
	}

	s.IndexList.SetTitle(fmt.Sprintf(" Indexes [yellow](%s.%s)[white] [green]%d[white] ", s.CurrentDB, s.CurrentColl, len(s.CurrentIndexes)))
	s.DocPages.SwitchToPage("indexes")
	s.App.SetFocus(s.IndexList)
	state.RefreshPanelBorders()
}

func DropIndexConfirm() {
	if !state.CheckWriteAccess() {
		return
	}
	s := state.S
	idx := s.IndexList.GetCurrentItem()
	if idx < 0 || idx >= len(s.CurrentIndexes) {
		return
	}
	name, _ := s.CurrentIndexes[idx]["name"].(string)
	if name == "_id_" {
		s.StatusBar.SetText(" [red]Cannot drop the _id_ index")
		go func() {
			time.Sleep(2 * time.Second)
			s.App.QueueUpdateDraw(func() {
				s.StatusBar.SetText(s.DefaultStatusText)
			})
		}()
		return
	}

	s.ConfirmModal.ClearButtons()
	s.ConfirmModal.
		SetText(fmt.Sprintf("Drop index '%s'? This cannot be undone!", name)).
		AddButtons([]string{"Drop", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Drop" {
				dropIndex(name)
			}
			s.RootPages.HidePage("confirm")
			s.RootPages.SwitchToPage("main")
			s.App.SetFocus(s.IndexList)
		})
	s.RootPages.ShowPage("confirm")
	s.App.SetFocus(s.ConfirmModal)
}

func dropIndex(name string) {
	if !state.CheckWriteAccess() {
		return
	}
	s := state.S
	ctx, cancel := state.NewCtx()
	defer cancel()
	_, err := s.Client.Database(s.CurrentDB).Collection(s.CurrentColl).Indexes().DropOne(ctx, name)
	if err != nil {
		s.StatusBar.SetText(" [red]Drop index failed: " + err.Error())
		return
	}
	s.StatusBar.SetText(fmt.Sprintf(" [green]Index '%s' dropped successfully", name))
	ShowIndexes()
}
