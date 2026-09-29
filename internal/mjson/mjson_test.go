package mjson

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func roundTrip(t *testing.T, d bson.D) bson.D {
	t.Helper()
	// Normalise through BSON so the comparison uses decoded types.
	raw, err := bson.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var want bson.D
	if err := bson.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for _, pretty := range []bool{true, false} {
		text := Join(Tokens(want, pretty))
		got, err := ParseDoc(text)
		if err != nil {
			t.Fatalf("ParseDoc(%s): %v", text, err)
		}
		gotRaw, _ := bson.Marshal(got)
		if !bytes.Equal(gotRaw, raw) {
			t.Fatalf("round trip mismatch (pretty=%v)\ntext: %s\n got: %#v\nwant: %#v", pretty, text, got, want)
		}
	}
	return want
}

func TestRoundTripPreservesTypesAndOrder(t *testing.T) {
	oid := primitive.NewObjectID()
	dec, _ := primitive.ParseDecimal128("1234.5678")
	roundTrip(t, bson.D{
		{Key: "_id", Value: oid},
		{Key: "z", Value: int32(1)},
		{Key: "a", Value: int64(2)},
		{Key: "big", Value: int64(9007199254740993)},
		{Key: "neg", Value: int64(-5)},
		{Key: "f", Value: 2.0},
		{Key: "frac", Value: 0.1},
		{Key: "tiny", Value: 1e-300},
		{Key: "huge", Value: 1.5e300},
		{Key: "negzero", Value: math.Copysign(0, -1)},
		{Key: "when", Value: primitive.NewDateTimeFromTime(time.Date(2024, 2, 3, 4, 5, 6, 789e6, time.UTC))},
		{Key: "ancient", Value: primitive.DateTime(-62198755200000 - 1000)},
		{Key: "dec", Value: dec},
		{Key: "s", Value: "quote \" backslash \\ newline \n tab \t [red]tag[-] ü 🥭"},
		{Key: "b", Value: true},
		{Key: "n", Value: nil},
		{Key: "arr", Value: bson.A{int32(1), "x", bson.D{{Key: "k", Value: int64(3)}}, bson.A{}}},
		{Key: "nested", Value: bson.D{{Key: "y", Value: int32(1)}, {Key: "x", Value: bson.D{}}}},
		{Key: "uuid", Value: primitive.Binary{Subtype: 4, Data: []byte("0123456789abcdef")}},
		{Key: "bin", Value: primitive.Binary{Subtype: 0, Data: []byte{0, 1, 2, 255}}},
		{Key: "re", Value: primitive.Regex{Pattern: "^ab[c/]", Options: "mi"}},
		{Key: "re2", Value: primitive.Regex{Pattern: "^abc$", Options: "i"}},
		{Key: "ts", Value: primitive.Timestamp{T: 1700000000, I: 7}},
		{Key: "min", Value: primitive.MinKey{}},
		{Key: "max", Value: primitive.MaxKey{}},
		{Key: "nan", Value: math.NaN()},
		{Key: "code", Value: primitive.JavaScript("function() {}")},
		{Key: "a.dotted key", Value: int32(1)},
	})
}

func TestRoundTripCompoundID(t *testing.T) {
	roundTrip(t, bson.D{{Key: "_id", Value: bson.D{{Key: "b", Value: int32(1)}, {Key: "a", Value: int32(2)}}}})
}

func TestPrettyFormat(t *testing.T) {
	got := Pretty(bson.D{{Key: "a", Value: int32(1)}, {Key: "b", Value: bson.A{int64(2)}}})
	want := "{\n  \"a\": 1,\n  \"b\": [\n    NumberLong(2)\n  ]\n}"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if c := Compact(bson.D{{Key: "a", Value: 1.5}, {Key: "b", Value: bson.D{}}}); c != `{"a": 1.5, "b": {}}` {
		t.Errorf("compact = %s", c)
	}
}

func TestParseDocShellSyntax(t *testing.T) {
	cases := []struct {
		in   string
		want bson.D
	}{
		{`{name: 'Alice', age: 30}`, bson.D{{Key: "name", Value: "Alice"}, {Key: "age", Value: int32(30)}}},
		{`{"a": 1,}`, bson.D{{Key: "a", Value: int32(1)}}},
		{`{a: [1, 2,], }`, bson.D{{Key: "a", Value: bson.A{int32(1), int32(2)}}}},
		{`{status: active}`, bson.D{{Key: "status", Value: "active"}}},
		{`{"x.y": {$gt: 5}}`, bson.D{{Key: "x.y", Value: bson.D{{Key: "$gt", Value: int32(5)}}}}},
		{`{a: 'it\'s "q"'}`, bson.D{{Key: "a", Value: `it's "q"`}}},
		{`{n: NumberLong("9007199254740993"), i: NumberInt(3), l: Long(4)}`, bson.D{{Key: "n", Value: int64(9007199254740993)}, {Key: "i", Value: int32(3)}, {Key: "l", Value: int64(4)}}},
		{`{d: new Date(0)}`, bson.D{{Key: "d", Value: primitive.DateTime(0)}}},
		{`{d: ISODate("2024-01-02")}`, bson.D{{Key: "d", Value: primitive.NewDateTimeFromTime(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))}}},
		{`{d: ISODate("2024-01-02T03:04:05+01:00")}`, bson.D{{Key: "d", Value: primitive.NewDateTimeFromTime(time.Date(2024, 1, 2, 2, 4, 5, 0, time.UTC))}}},
		{`{name: /^al(ice)?$/i}`, bson.D{{Key: "name", Value: primitive.Regex{Pattern: "^al(ice)?$", Options: "i"}}}},
		{`{p: /a\/b[/]/}`, bson.D{{Key: "p", Value: primitive.Regex{Pattern: "a/b[/]", Options: ""}}}},
		{`{v: -Infinity}`, bson.D{{Key: "v", Value: math.Inf(-1)}}},
		{"{a: 1 // comment\n, /* block */ b: 2}", bson.D{{Key: "a", Value: int32(1)}, {Key: "b", Value: int32(2)}}},
		{`{url: "http://x/y"}`, bson.D{{Key: "url", Value: "http://x/y"}}},
		{`{k: MinKey}`, bson.D{{Key: "k", Value: primitive.MinKey{}}}},
		{`{"$oid-like": {"$oid": "65f000000000000000000000"}}`, bson.D{{Key: "$oid-like", Value: mustOID("65f000000000000000000000")}}},
	}
	for _, c := range cases {
		got, err := ParseDoc(c.in)
		if err != nil {
			t.Errorf("ParseDoc(%s): %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseDoc(%s)\n got: %#v\nwant: %#v", c.in, got, c.want)
		}
	}
}

func mustOID(h string) primitive.ObjectID {
	o, _ := primitive.ObjectIDFromHex(h)
	return o
}

func TestParseDocObjectID(t *testing.T) {
	d, err := ParseDoc(`{ref: ObjectId("65f000000000000000000000")}`)
	if err != nil {
		t.Fatal(err)
	}
	if d[0].Value != mustOID("65f000000000000000000000") {
		t.Errorf("got %#v", d)
	}
	d, err = ParseDoc(`{_id: ObjectId()}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d[0].Value.(primitive.ObjectID); !ok {
		t.Errorf("ObjectId() should generate an id, got %#v", d)
	}
}

func TestParseDocErrors(t *testing.T) {
	for _, in := range []string{
		`{a: ObjectId("nothex")}`,
		`{a: NumberLong("x")}`,
		`{a: Foo(1)}`,
		`{a: "unterminated}`,
		`{a: /unterminated}`,
		`{a: 1`,
		`[1, 2]`,
	} {
		if _, err := ParseDoc(in); err == nil {
			t.Errorf("ParseDoc(%s): expected error", in)
		}
	}
}

func TestParseArgs(t *testing.T) {
	a, err := ParseArgs(`{a: 1}, {b: 1, _id: 0}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 {
		t.Fatalf("got %d args", len(a))
	}
	if _, ok := a[0].(bson.D); !ok {
		t.Errorf("arg 0 type %T", a[0])
	}
	a, err = ParseArgs(`"field", {x: /a,b/}`)
	if err != nil || len(a) != 2 || a[0] != "field" {
		t.Fatalf("got %#v %v", a, err)
	}
	a, err = ParseArgs(`[{$match: {}}, {$sort: {b: 1, a: -1}}]`)
	if err != nil {
		t.Fatal(err)
	}
	stages := a[0].(bson.A)
	sortStage := stages[1].(bson.D)[0].Value.(bson.D)
	if sortStage[0].Key != "b" || sortStage[1].Key != "a" {
		t.Errorf("sort key order not preserved: %#v", sortStage)
	}
	if a, err = ParseArgs("  "); err != nil || len(a) != 0 {
		t.Errorf("empty args: %#v %v", a, err)
	}
}

func TestTokensKinds(t *testing.T) {
	toks := Tokens(bson.D{{Key: "k", Value: "[red]v"}}, false)
	var kinds []Kind
	for _, tk := range toks {
		kinds = append(kinds, tk.Kind)
	}
	want := []Kind{Punct, Key, Punct, String, Punct}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("kinds = %v, want %v", kinds, want)
	}
	if !strings.Contains(Join(toks), `"[red]v"`) {
		t.Errorf("raw text should be unescaped: %s", Join(toks))
	}
}
