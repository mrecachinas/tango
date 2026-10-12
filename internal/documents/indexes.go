package documents

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
	"tango/internal/state"
)

// IndexRows keeps the common options in a stable table order without flattening
// key patterns or partial filters. The original specs remain available in detail.
func IndexRows(indexes []bson.D) []bson.D {
	rows := make([]bson.D, len(indexes))
	for i, spec := range indexes {
		value := func(key string) any {
			v, _ := mjson.Get(spec, key)
			return v
		}
		name := value("name")
		nameStr, _ := name.(string)
		unique, _ := value("unique").(bool)
		sparse, _ := value("sparse").(bool)
		partial := value("partialFilterExpression")
		if partial != nil {
			partial = mjson.Compact(partial)
		}
		rows[i] = bson.D{
			{Key: "name", Value: name},
			{Key: "keys", Value: mjson.Compact(value("key"))},
			{Key: "unique", Value: unique || nameStr == "_id_"},
			{Key: "sparse", Value: sparse},
			{Key: "ttl", Value: blankIfNil(value("expireAfterSeconds"))},
			{Key: "partial", Value: blankIfNil(partial)},
			{Key: "size", Value: blankIfNil(value("size"))},
		}
		for _, option := range spec {
			switch option.Key {
			case "name", "key", "unique", "sparse", "expireAfterSeconds", "partialFilterExpression", "size", "v":
				continue
			}
			switch option.Value.(type) {
			case bson.D, bson.M, bson.A:
				option.Value = mjson.Compact(option.Value)
			}
			rows[i] = append(rows[i], option)
		}
	}
	return rows
}

// blankIfNil keeps optional index columns stable while rendering absent
// options as empty cells instead of null.
func blankIfNil(v any) any {
	if v == nil {
		return ""
	}
	return v
}

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
