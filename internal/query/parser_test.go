package query

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/state"
)

func TestParseShellQuery(t *testing.T) {
	oid, _ := primitive.ObjectIDFromHex("65f000000000000000000000")
	cases := []struct {
		in    string
		coll  string
		meth  string
		args  bson.A
		chain []call
	}{
		{`db.users.find({})`, "users", "find", bson.A{bson.D{}}, nil},
		{`db.users.find()`, "users", "find", bson.A{}, nil},
		{`db.users.find({name: "a)b"})`, "users", "find", bson.A{bson.D{{Key: "name", Value: "a)b"}}}, nil},
		{`db.users.find({name: /a\)(b/})`, "users", "find", bson.A{bson.D{{Key: "name", Value: primitive.Regex{Pattern: `a\)(b`}}}}, nil},
		{`db.system.profile.find()`, "system.profile", "find", bson.A{}, nil},
		{`db.getCollection("my.coll").countDocuments({a: 1});`, "my.coll", "countDocuments", bson.A{bson.D{{Key: "a", Value: int32(1)}}}, nil},
		{`db.o.find({ref: ObjectId("65f000000000000000000000")}, {x: 1}).sort({b: 1, a: -1}).limit(5).skip(10)`,
			"o", "find",
			bson.A{bson.D{{Key: "ref", Value: oid}}, bson.D{{Key: "x", Value: int32(1)}}},
			[]call{
				{"sort", bson.A{bson.D{{Key: "b", Value: int32(1)}, {Key: "a", Value: int32(-1)}}}},
				{"limit", bson.A{int32(5)}},
				{"skip", bson.A{int32(10)}},
			}},
		{`db.c.distinct("tags", {x: {$in: [1, 2]}})`, "c", "distinct", bson.A{"tags", bson.D{{Key: "x", Value: bson.D{{Key: "$in", Value: bson.A{int32(1), int32(2)}}}}}}, nil},
	}
	for _, c := range cases {
		q, err := parseShellQuery(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if q.Coll != c.coll || q.Method != c.meth {
			t.Errorf("%s: coll=%q method=%q", c.in, q.Coll, q.Method)
		}
		if !reflect.DeepEqual(q.Args, c.args) {
			t.Errorf("%s: args=%#v want %#v", c.in, q.Args, c.args)
		}
		if len(q.Chain) != len(c.chain) || (len(c.chain) > 0 && !reflect.DeepEqual(q.Chain, c.chain)) {
			t.Errorf("%s: chain=%#v want %#v", c.in, q.Chain, c.chain)
		}
	}
}

func TestParseShellQueryErrors(t *testing.T) {
	for _, in := range []string{
		`db.find()`,
		`db.users`,
		`db.users.find({a: 1}`,
		`db.users.find({a: "x})`,
		`db.users.find({}) junk`,
		`db.users.find({a: Foo()})`,
		`db.getCollection().find()`,
	} {
		if _, err := parseShellQuery(in); err == nil {
			t.Errorf("%s: expected error", in)
		}
	}
}

func TestParseFilter(t *testing.T) {
	f, err := parseFilter(`{_id: {$in: [1, 5]}, name: 'x'}`)
	if err != nil {
		t.Fatal(err)
	}
	want := bson.D{
		{Key: "_id", Value: bson.D{{Key: "$in", Value: bson.A{int32(1), int32(5)}}}},
		{Key: "name", Value: "x"},
	}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("got %#v", f)
	}
	if f, err := parseFilter("  "); err != nil || len(f) != 0 {
		t.Errorf("empty filter: %#v %v", f, err)
	}
}

func TestOutputTarget(t *testing.T) {
	cases := []struct {
		stage bson.D
		want  string
		ok    bool
	}{
		{bson.D{{Key: "$out", Value: "c"}}, "db.c", true},
		{bson.D{{Key: "$out", Value: bson.D{{Key: "db", Value: "o"}, {Key: "coll", Value: "c"}}}}, "o.c", true},
		{bson.D{{Key: "$merge", Value: bson.D{{Key: "into", Value: "m"}}}}, "db.m", true},
		{bson.D{{Key: "$match", Value: bson.D{}}}, "", false},
	}
	for _, c := range cases {
		ns, ok := outputTarget([]bson.D{c.stage}, "db")
		if ok != c.ok || (ok && ns.String() != c.want) {
			t.Errorf("%v: got %v %v", c.stage, ns, ok)
		}
	}
}

func TestAutocompleteEscapesNames(t *testing.T) {
	state.S = &state.AppState{AllCollItems: []state.ListEntry{{Name: "we[ird]"}}}
	got := GetAutocompleteSuggestions("db.we")
	if len(got) != 1 || got[0] != "db.we[ird[]." {
		t.Fatalf("suggestions = %q", got)
	}
	if raw := SuggestionText(got[0]); raw != "db.we[ird]." {
		t.Errorf("raw = %q", raw)
	}
}
