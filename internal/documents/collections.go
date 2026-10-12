package documents

import (
	"context"
	"fmt"

	"tango/internal/state"
)

func DropCollectionConfirm() {
	s := state.S
	name := state.GetSelectedCollName()
	ns := state.Namespace{DB: s.SelectedDB, Coll: name}
	if ns.DB == "" {
		ns.DB = s.CurrentDB
	}
	if ns.DB == "" || name == "" {
		return
	}
	if !state.CheckWrite(state.ActDropCollection, ns) {
		return
	}
	confirm("drop-collection", fmt.Sprintf("Drop collection '%s'? This cannot be undone!", ns), name, func() {
		coll := s.Client.Database(ns.DB).Collection(ns.Coll)
		state.Async("Dropping collection", func(ctx context.Context) func() {
			err := coll.Drop(ctx)
			return func() {
				if err != nil {
					state.FlashError("Drop failed: " + err.Error())
					return
				}
				state.FlashStatus(fmt.Sprintf("Collection '%s' dropped", ns))
				delete(s.Collections, ns.DB)
				if s.CurrentDB == ns.DB {
					SelectDatabase(ns.DB, nil)
				}
			}
		})
	})
}

func DropDatabaseConfirm() {
	s := state.S
	name := s.SelectedDB
	if name == "" {
		name = s.CurrentDB
	}
	if name == "" {
		return
	}
	if !state.CheckWrite(state.ActDropDatabase, state.Namespace{DB: name}) {
		return
	}
	confirm("drop-database", "Drop database '"+name+"'? This cannot be undone!", name, func() {
		state.Async("Dropping database", func(ctx context.Context) func() {
			err := s.Client.Database(name).Drop(ctx)
			return func() {
				if err != nil {
					state.FlashError("Drop failed: " + err.Error())
					return
				}
				if s.CurrentDB == name {
					s.CurrentDB, s.CurrentColl = "", ""
					state.ClearDocPanel()
				}
				FullRefresh()
			}
		})
	})
}
