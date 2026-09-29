package documents

import (
	"context"
	"fmt"

	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
	"tango/internal/state"
)

// ShowIndexes lists the indexes of the current collection.
func ShowIndexes() {
	s := state.S
	if s.CurrentDB == "" || s.CurrentColl == "" {
		return
	}
	ShowIndexesFor(state.Namespace{DB: s.CurrentDB, Coll: s.CurrentColl})
}

// ShowIndexesFor lists the indexes of ns without changing the browsed collection.
func ShowIndexesFor(ns state.Namespace) {
	s := state.S
	coll := s.Client.Database(ns.DB).Collection(ns.Coll)
	state.Async("Loading indexes", func(ctx context.Context) func() {
		cur, err := coll.Indexes().List(ctx)
		var idxs []bson.D
		if err == nil {
			err = cur.All(ctx, &idxs)
		}
		return func() {
			if err != nil {
				state.ShowError(fmt.Sprintf("Error listing indexes on %s: %s", ns, err))
				return
			}
			s.CurrentIndexes = idxs
			s.IndexesNS = ns
			s.IndexList.Clear()
			for _, idx := range idxs {
				name, _ := mjson.Get(idx, "name")
				key, _ := mjson.Get(idx, "key")
				nameStr, _ := name.(string)
				s.IndexList.AddItem(tview.Escape(nameStr), tview.Escape(mjson.Compact(key)), 0, nil)
			}
			s.IndexList.SetTitle(fmt.Sprintf(" Indexes [yellow](%s)[white] [green]%d[white] ", tview.Escape(ns.String()), len(idxs)))
			s.DocPages.SwitchToPage("indexes")
			s.App.SetFocus(s.IndexList)
			state.RefreshPanelBorders()
		}
	})
}

// ShowIndexDetail shows the full specification of index i.
func ShowIndexDetail(i int) {
	s := state.S
	if i < 0 || i >= len(s.CurrentIndexes) {
		return
	}
	idx := s.CurrentIndexes[i]
	name, _ := mjson.Get(idx, "name")
	nameStr, _ := name.(string)
	state.SetDetail(fmt.Sprintf(" Index Detail [yellow](%s)[white] ", tview.Escape(nameStr)), mjson.Tokens(idx, true))
	s.DetailBackPage = "indexes"
}

func DropIndexConfirm() {
	s := state.S
	idx := s.IndexList.GetCurrentItem()
	if idx < 0 || idx >= len(s.CurrentIndexes) {
		return
	}
	ns := s.IndexesNS
	if !state.CheckWrite(state.ActDropIndex, ns) {
		return
	}
	nameVal, _ := mjson.Get(s.CurrentIndexes[idx], "name")
	name, _ := nameVal.(string)
	if name == "_id_" {
		state.FlashError("Cannot drop the _id_ index")
		return
	}
	confirm(s.ConfirmModal, "confirm", fmt.Sprintf("Drop index '%s' on %s? This cannot be undone!", name, ns), "Drop", s.IndexList, func() {
		coll := s.Client.Database(ns.DB).Collection(ns.Coll)
		state.Async("Dropping index", func(ctx context.Context) func() {
			_, err := coll.Indexes().DropOne(ctx, name)
			return func() {
				if err != nil {
					state.FlashError("Drop index failed: " + err.Error())
					return
				}
				state.FlashStatus(fmt.Sprintf(" [green]Index '%s' dropped", tview.Escape(name)))
				ShowIndexesFor(ns)
			}
		})
	})
}
