package documents

import (
	"context"
	"fmt"

	"github.com/rivo/tview"

	"tango/internal/state"
)

func DropCollectionConfirm() {
	s := state.S
	name := state.GetSelectedCollName()
	if s.CurrentDB == "" || name == "" {
		return
	}
	ns := state.Namespace{DB: s.CurrentDB, Coll: name}
	if !state.CheckWrite(state.ActDropCollection, ns) {
		return
	}
	confirm(s.DropModal, "drop-confirm", fmt.Sprintf("Drop collection '%s'? This cannot be undone!", ns), "Drop", s.CollList, func() {
		coll := s.Client.Database(ns.DB).Collection(ns.Coll)
		state.Async("Dropping collection", func(ctx context.Context) func() {
			err := coll.Drop(ctx)
			return func() {
				if err != nil {
					state.FlashError("Drop failed: " + err.Error())
					return
				}
				state.FlashStatus(fmt.Sprintf(" [green]Collection '%s' dropped", tview.Escape(ns.String())))
				if s.CurrentDB == ns.DB {
					SelectDatabase(ns.DB, nil)
				}
			}
		})
	})
}
