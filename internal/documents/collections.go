package documents

import (
	"fmt"
	"strings"

	"tango/internal/state"
)

func DropCollectionConfirm() {
	if !state.CheckWriteAccess() {
		return
	}
	s := state.S
	idx := s.CollList.GetCurrentItem()
	if idx < 0 || idx >= s.CollList.GetItemCount() {
		return
	}
	name, _ := s.CollList.GetItemText(idx)
	if i := strings.Index(name, " [gray]"); i != -1 {
		name = name[:i]
	}
	s.DropModal.ClearButtons()
	s.DropModal.
		SetText(fmt.Sprintf("Drop collection '%s'? This cannot be undone!", name)).
		AddButtons([]string{"Drop", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "Drop" {
				dropCollection(name)
			}
			s.RootPages.HidePage("drop-confirm")
			s.RootPages.SwitchToPage("main")
			s.App.SetFocus(s.CollList)
		})
	s.RootPages.ShowPage("drop-confirm")
	s.App.SetFocus(s.DropModal)
}

func dropCollection(name string) {
	if !state.CheckWriteAccess() {
		return
	}
	s := state.S
	state.ShowLoading("Dropping collection")
	defer state.DoneLoading()
	ctx, cancel := state.NewCtx()
	defer cancel()
	err := s.Client.Database(s.CurrentDB).Collection(name).Drop(ctx)
	if err != nil {
		s.StatusBar.SetText(" [red]Drop failed: " + err.Error())
		return
	}
	s.StatusBar.SetText(fmt.Sprintf(" [green]Collection '%s' dropped successfully", name))
	state.ClearDocPanel()
	SelectDatabase(s.CurrentDB)
}
