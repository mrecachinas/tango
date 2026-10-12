package documents

import (
	"context"
	"fmt"

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
			s.IndexCursor = 0
			s.ListRevision++
			s.View, s.Focus = "indexes", "content"
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
	state.SetDetail("Index Detail · "+nameStr, mjson.Tokens(idx, true))
	s.DetailBackPage = "indexes"
}

func DropIndexConfirm() {
	s := state.S
	idx := s.IndexCursor
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
	confirm("drop-index", fmt.Sprintf("Drop index '%s' on %s? This cannot be undone!", name, ns), "", func() {
		coll := s.Client.Database(ns.DB).Collection(ns.Coll)
		state.Async("Dropping index", func(ctx context.Context) func() {
			_, err := coll.Indexes().DropOne(ctx, name)
			return func() {
				if err != nil {
					state.FlashError("Drop index failed: " + err.Error())
					return
				}
				state.FlashStatus(fmt.Sprintf("Index '%s' dropped", name))
				ShowIndexesFor(ns)
			}
		})
	})
}
