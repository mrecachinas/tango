package query

import (
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestExplainCommand(t *testing.T) {
	cases := []struct{ in, verbosity, want string }{
		{`db.o.find({a: 1}).sort({b: -1}).limit(5).skip(10).explain()`, "executionStats",
			`{"find":"o","filter":{"a":1},"sort":{"b":-1},"skip":10,"limit":5}`},
		{`db.o.find({}, {x: 1}).hint("a_1").explain("queryPlanner")`, "queryPlanner",
			`{"find":"o","filter":{},"hint":"a_1","projection":{"x":1}}`},
		{`db.o.explain(true).findOne({a: 1})`, "allPlansExecution",
			`{"find":"o","filter":{"a":1},"singleBatch":true,"limit":1}`},
		{`db.o.explain("EXECUTIONSTATS").countDocuments({a: 1})`, "executionStats",
			`{"aggregate":"o","pipeline":[{"$match":{"a":1}},{"$group":{"_id":1,"n":{"$sum":1}}}],"cursor":{}}`},
		{`db.o.count().explain()`, "executionStats",
			`{"aggregate":"o","pipeline":[{"$match":{}},{"$group":{"_id":1,"n":{"$sum":1}}}],"cursor":{}}`},
		{`db.o.estimatedDocumentCount().explain()`, "executionStats",
			`{"count":"o"}`},
		{`db.o.distinct("tags", {a: 1}).explain(false)`, "queryPlanner",
			`{"distinct":"o","key":"tags","query":{"a":1}}`},
		{`db.o.aggregate([{$match: {a: 1}}], {allowDiskUse: true}).explain()`, "executionStats",
			`{"aggregate":"o","pipeline":[{"$match":{"a":1}}],"cursor":{},"allowDiskUse":true}`},
		{`db.o.explain().updateMany({a: 1}, {$set: {b: 2}})`, "executionStats",
			`{"update":"o","updates":[{"q":{"a":1},"u":{"$set":{"b":2}},"multi":true}]}`},
		{`db.o.explain().deleteOne({a: 1})`, "executionStats",
			`{"delete":"o","deletes":[{"q":{"a":1},"limit":1}]}`},
	}
	for _, c := range cases {
		q, err := parseShellQuery(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		q, verbosity, ok, err := splitExplain(q)
		if !ok || err != nil || verbosity != c.verbosity {
			t.Errorf("%s: split ok=%v err=%v verbosity=%q", c.in, ok, err, verbosity)
			continue
		}
		cmd, err := explainCommand(q)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		got, _ := bson.MarshalExtJSON(cmd, false, false)
		if string(got) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

func TestExplainErrors(t *testing.T) {
	cases := map[string]string{
		`db.o.find().explain("verbose")`:     "unknown explain verbosity",
		`db.o.explain()`:                     "must be followed by a query",
		`db.o.find().batchSize(2).explain()`: "unsupported cursor method",
		`db.o.insertOne({}).explain()`:       "can't explain insertOne",
		`db.o.distinct().explain()`:          "field name",
	}
	for in, want := range cases {
		q, err := parseShellQuery(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		q, _, ok, err := splitExplain(q)
		if !ok {
			t.Errorf("%s: not detected as explain", in)
			continue
		}
		if err == nil {
			_, err = explainCommand(q)
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err=%v, want %q", in, err, want)
		}
	}
	if _, _, ok, _ := splitExplain(shellQuery{Coll: "o", Method: "find"}); ok {
		t.Error("plain find detected as explain")
	}
}
