package advisor

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestOpFromCommand(t *testing.T) {
	op, ok := OpFromCommand(doc(t, `{aggregate: "o", pipeline: [{$match: {a: 1}}, {$match: {b: {$gt: 2}}}, {$sort: {c: -1}}, {$group: {_id: "$a"}}], cursor: {batchSize: 5}, lsid: {id: 1}, $db: "shop", maxTimeMS: 5}`))
	if !ok || op.Kind != "aggregate" || op.Coll != "o" {
		t.Fatalf("%+v", op)
	}
	if got := compact(op.Filter, 0); got != `{"$and": [{"a": 1}, {"b": {"$gt": 2}}]}` {
		t.Fatalf("filter %s", got)
	}
	if got := compact(op.Sort, 0); got != `{"c": -1}` {
		t.Fatalf("sort %s", got)
	}
	if got := compact(op.Command, 0); got != `{"aggregate": "o", "pipeline": [{"$match": {"a": 1}}, {"$match": {"b": {"$gt": 2}}}, {"$sort": {"c": -1}}, {"$group": {"_id": "$a"}}], "cursor": {}}` {
		t.Fatalf("command %s", got)
	}

	op, ok = OpFromCommand(doc(t, `{delete: "o", deletes: [{q: {a: 1}, limit: 1}, {q: {b: 1}, limit: 1}], ordered: true}`))
	if !ok || op.Kind != "delete" || compact(op.Command, 0) != `{"delete": "o", "ordered": true, "deletes": [{"q": {"a": 1}, "limit": 1}]}` {
		t.Fatalf("%+v %s", op, compact(op.Command, 0))
	}
	if _, ok := OpFromCommand(doc(t, `{insert: "o", documents: []}`)); ok {
		t.Fatal("insert is not explainable")
	}
	if _, ok := OpFromCommand(doc(t, `{find: "o", $truncated: "..."}`)); ok {
		t.Fatal("truncated commands are not explainable")
	}
}

func TestOpFromProfile(t *testing.T) {
	op, ok := OpFromProfile(doc(t, `{op: "getmore", ns: "shop.o", command: {getMore: 1, collection: "o"}, originatingCommand: {find: "o", filter: {a: 1}, sort: {b: 1}}}`))
	if !ok || op.Kind != "find" || op.ShapeText() != `find {"a": "?"} sort {"b": 1}` {
		t.Fatalf("%+v %q", op, op.ShapeText())
	}
	op, ok = OpFromProfile(doc(t, `{op: "update", ns: "shop.o", command: {q: {a: 1}, u: {$set: {b: 1}}, multi: true}}`))
	if !ok || op.Kind != "update" || compact(op.Command, 0) != `{"update": "o", "updates": [{"q": {"a": 1}, "u": {"$set": {"b": 1}}, "multi": true}]}` {
		t.Fatalf("%+v %s", op, compact(op.Command, 0))
	}
	if _, ok := OpFromProfile(doc(t, `{op: "insert", ns: "shop.o", command: {insert: "o"}}`)); ok {
		t.Fatal("insert")
	}
}

func TestShape(t *testing.T) {
	a := Shape(doc(t, `{b: {$in: [1, 2, 3]}, a: 5, $or: [{c: {$gt: 1, $lt: 9}}, {d: /x/}], e: {$elemMatch: {f: 1}}, g: {$not: {$gt: 2}}}`))
	b := Shape(doc(t, `{a: 7, b: {$in: [9]}, $or: [{c: {$gt: 2, $lt: 3}}, {d: /y/}], e: {$elemMatch: {f: 2}}, g: {$not: {$gt: 0}}}`))
	if compact(a, 0) != compact(b, 0) {
		t.Fatalf("shapes differ:\n%s\n%s", compact(a, 0), compact(b, 0))
	}
	want := `{"$or": [{"c": {"$gt": "?", "$lt": "?"}}, {"d": /?/}], "a": "?", "b": {"$in": "?"}, "e": {"$elemMatch": {"f": "?"}}, "g": {"$not": {"$gt": "?"}}}`
	if got := compact(a, 0); got != want {
		t.Fatalf("shape %s", got)
	}
	if compact(Shape(doc(t, `{a: {x: 1}}`)), 0) != `{"a": "?"}` {
		t.Fatal("embedded document equality is a value")
	}
}

func TestShapeRegex(t *testing.T) {
	shape := func(src string) string { return compact(Shape(doc(t, src)), 0) }
	eq, prefix := shape(`{name: "ab"}`), shape(`{name: /^ab/}`)
	if eq == prefix || prefix != `{"name": /^?/}` {
		t.Fatalf("equality %s, prefix regex %s", eq, prefix)
	}
	if got := shape(`{name: /ab/i}`); got != `{"name": /?/i}` {
		t.Fatalf("unanchored %s", got)
	}
	if got := shape(`{name: {$regex: "^cd", $options: "i"}}`); got != `{"name": {"$options": "i", "$regex": /^?/}}` {
		t.Fatalf("$regex %s", got)
	}
	if got := shape(`{name: {$not: /^x/}}`); got != `{"name": {"$not": /^?/}}` {
		t.Fatalf("$not regex %s", got)
	}
	if shape(`{name: /^zz/}`) != prefix {
		t.Fatal("regex patterns should group together")
	}
}

func TestShapeAggregate(t *testing.T) {
	text := func(src string) string {
		op, ok := OpFromCommand(doc(t, `{aggregate: "o", pipeline: `+src+`}`))
		if !ok {
			t.Fatalf("not an op: %s", src)
		}
		return op.ShapeText()
	}
	byCust := text(`[{$match: {status: "A", qty: {$gt: 5}}}, {$group: {_id: "$customer", n: {$sum: 1}, total: {$sum: "$qty"}}}, {$sort: {n: -1}}, {$limit: 10}]`)
	want := `aggregate [$match {"qty": {"$gt": "?"}, "status": "?"}, $group {"_id": "$customer", "n": {"$sum": "?"}, "total": {"$sum": "$qty"}}, $sort {"n": -1}, $limit "?"]`
	if byCust != want {
		t.Fatalf("shape\n got %s\nwant %s", byCust, want)
	}
	if same := text(`[{$match: {qty: {$gt: 99}, status: "B"}}, {$group: {_id: "$customer", n: {$sum: 1}, total: {$sum: "$qty"}}}, {$sort: {n: -1}}, {$limit: 50}]`); same != byCust {
		t.Fatalf("literal-only change split the shape:\n%s\n%s", same, byCust)
	}
	if byRegion := text(`[{$match: {status: "A", qty: {$gt: 5}}}, {$group: {_id: "$region", n: {$sum: 1}, total: {$sum: "$qty"}}}, {$sort: {n: -1}}, {$limit: 10}]`); byRegion == byCust {
		t.Fatalf("$group keys merged: %s", byRegion)
	}

	lookup := func(from string) string {
		return text(`[{$match: {a: 1}}, {$lookup: {from: "` + from + `", localField: "cid", foreignField: "_id", as: "c"}}, {$unwind: {path: "$c", preserveNullAndEmptyArrays: true}}]`)
	}
	if got := lookup("customers"); got != `aggregate [$match {"a": "?"}, $lookup {"from": "customers", "localField": "cid", "foreignField": "_id", "as": "c"}, $unwind {"path": "$c", "preserveNullAndEmptyArrays": true}]` {
		t.Fatalf("lookup %s", got)
	}
	if lookup("customers") == lookup("vendors") {
		t.Fatal("$lookup.from merged")
	}

	nested := text(`[{$lookup: {from: "c", let: {id: "$cid"}, pipeline: [{$match: {$expr: {$eq: ["$_id", "$$id"]}, kind: "x"}}, {$project: {name: 1, tag: "lit"}}], as: "c"}}, {$facet: {top: [{$limit: 3}]}}, {$merge: {into: "out", on: "_id", whenMatched: "merge"}}]`)
	wantNested := `aggregate [$lookup {"from": "c", "let": {"id": "$cid"}, "pipeline": [{"$match": {"$expr": {"$eq": ["$_id", "$$id"]}, "kind": "?"}}, {"$project": {"name": 1, "tag": "?"}}], "as": "c"}, $facet {"top": [{"$limit": "?"}]}, $merge {"into": "out", "on": "_id", "whenMatched": "merge"}]`
	if nested != wantNested {
		t.Fatalf("nested\n got %s\nwant %s", nested, wantNested)
	}
}

func TestExplainCommand(t *testing.T) {
	op, _ := OpFromCommand(bson.D{{Key: "find", Value: "o"}, {Key: "filter", Value: bson.D{}}})
	cmd := op.ExplainCommand("", 1500)
	if compact(cmd, 0) != `{"explain": {"find": "o", "filter": {}}, "verbosity": "executionStats", "maxTimeMS": NumberLong(1500)}` {
		t.Fatal(compact(cmd, 0))
	}
	if compact(op.ExplainCommand("queryPlanner", 0), 0) != `{"explain": {"find": "o", "filter": {}}, "verbosity": "queryPlanner"}` {
		t.Fatal("queryPlanner")
	}
}
