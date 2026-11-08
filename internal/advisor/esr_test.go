package advisor

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
)

func doc(t *testing.T, s string) bson.D {
	t.Helper()
	if s == "" {
		return nil
	}
	d, err := mjson.ParseDoc(s)
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return d
}

func TestSuggest(t *testing.T) {
	cases := []struct {
		name, filter, sort string
		want               []string
	}{
		{"equality", `{a: 1}`, ``, []string{`{"a": 1}`}},
		{"esr order", `{d: {$gt: 5}, a: 1}`, `{c: -1}`, []string{`{"a": 1, "c": -1, "d": 1}`}},
		{"in without sort is equality", `{a: {$in: [1, 2]}, b: {$lt: 3}}`, ``, []string{`{"a": 1, "b": 1}`}},
		{"in with sort is range", `{a: {$in: [1, 2]}, b: 1}`, `{c: 1}`, []string{`{"b": 1, "c": 1, "a": 1}`}},
		{"single in is equality", `{a: {$in: [1]}}`, `{c: 1}`, []string{`{"a": 1, "c": 1}`}},
		{"sort on equality field dropped", `{a: 1}`, `{a: 1, b: -1}`, []string{`{"a": 1, "b": -1}`}},
		{"range in sort dropped", `{a: 1, b: {$gt: 0}}`, `{b: -1}`, []string{`{"a": 1, "b": -1}`}},
		{"low selectivity last", `{a: {$ne: 1}, b: 2}`, ``, []string{`{"b": 1, "a": 1}`}},
		{"anchored regex is range", `{name: /^ab/, a: 1}`, ``, []string{`{"a": 1, "name": 1}`}},
		{"unanchored regex is low", `{name: /ab/, x: {$gt: 1}}`, ``, []string{`{"x": 1, "name": 1}`}},
		{"case-insensitive regex is low", `{name: {$regex: "^ab", $options: "i"}, x: {$gt: 1}}`, ``, []string{`{"x": 1, "name": 1}`}},
		{"and flattened", `{$and: [{a: 1}, {b: {$gte: 2}}]}`, ``, []string{`{"a": 1, "b": 1}`}},
		{"or per branch", `{s: 1, $or: [{a: 1}, {b: 2}]}`, ``, []string{`{"s": 1, "a": 1}`, `{"s": 1, "b": 1}`}},
		{"_id equality skipped", `{_id: 5, a: 1}`, ``, nil},
		{"sort only", ``, `{created: -1}`, []string{`{"created": -1}`}},
		{"nothing", `{}`, ``, nil},
		{"where only", `{$where: "1"}`, ``, nil},
		{"text score sort ignored", `{a: 1}`, `{score: {$meta: "textScore"}}`, []string{`{"a": 1}`}},
		{"or inside and", `{$and: [{$or: [{a: 1}, {b: 2}]}]}`, ``, []string{`{"a": 1}`, `{"b": 1}`}},
		{"or inside and with siblings", `{s: 1, $and: [{t: {$gt: 1}}, {$or: [{a: 1}, {b: 2}]}]}`, ``, []string{`{"s": 1, "a": 1, "t": 1}`, `{"s": 1, "b": 1, "t": 1}`}},
		{"two ors in and", `{$and: [{$or: [{a: 1}, {b: 2}]}, {$or: [{c: 1}, {d: 2}]}]}`, ``, []string{`{"a": 1}`, `{"b": 1}`}},
		{"or nested in or branch", `{$or: [{$or: [{a: 1}, {b: 2}]}, {c: 3}]}`, ``, []string{`{"a": 1}`, `{"b": 1}`, `{"c": 1}`}},
		{"deeply nested or terminates", `{$or: [{$or: [{$or: [{$or: [{$or: [{$or: [{a: 1}]}]}]}]}]}]}`, ``, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Suggest(doc(t, c.filter), doc(t, c.sort))
			var keys []string
			for _, s := range got {
				keys = append(keys, KeysString(s.Keys()))
			}
			if len(keys) != len(c.want) {
				t.Fatalf("got %v, want %v", keys, c.want)
			}
			for i := range keys {
				if keys[i] != c.want[i] {
					t.Fatalf("got %v, want %v", keys, c.want)
				}
			}
		})
	}
}

func TestSuggestNotes(t *testing.T) {
	s := Suggest(doc(t, `{a: 1, $where: "x"}`), nil)
	if len(s) != 1 || len(s[0].Notes) != 1 {
		t.Fatalf("want one suggestion with a $where note, got %+v", s)
	}
	if got := s[0].Rationale(); got != "equality a" {
		t.Fatalf("rationale %q", got)
	}
	s = Suggest(doc(t, `{a: 1, b: {$gt: 1}, c: {$ne: 2}}`), doc(t, `{d: -1}`))
	if got := s[0].Rationale(); got != "equality a → sort d ↓ → range b → then c (low selectivity)" {
		t.Fatalf("rationale %q", got)
	}
}

func indexes(t *testing.T, specs ...string) []Index {
	var out []Index
	for _, s := range specs {
		out = append(out, ParseIndex(doc(t, s)))
	}
	return out
}

func TestAgainst(t *testing.T) {
	ixs := indexes(t,
		`{name: "_id_", key: {_id: 1}}`,
		`{name: "a_1", key: {a: 1}}`,
		`{name: "b_1_a_1_c_-1", key: {b: 1, a: 1, c: -1}}`,
		`{name: "u_1", key: {u: 1}, unique: true}`,
		`{name: "p_1_q_1", key: {p: 1, q: 1}, partialFilterExpression: {p: {$exists: true}}}`,
		`{name: "h_1_x_1", key: {h: 1, x: 1}, hidden: true}`,
	)
	cases := []struct {
		name, filter, sort, satisfied, extends string
	}{
		{"exact", `{a: 1}`, ``, "a_1", ""},
		{"equality set order free", `{a: 1, b: 2}`, `{c: -1}`, "b_1_a_1_c_-1", ""},
		{"reverse sort walks backwards", `{a: 1, b: 2}`, `{c: 1}`, "b_1_a_1_c_-1", ""},
		{"extends prefix", `{a: 1, z: 2}`, ``, "", "a_1"},
		{"unique not extended", `{u: 1, z: 2}`, ``, "", ""},
		{"partial ignored", `{p: 1, q: 2}`, ``, "", ""},
		{"hidden ignored", `{h: 1, x: 2}`, ``, "", ""},
		{"low selectivity tail optional", `{a: 1, n: {$ne: 1}}`, ``, "a_1", ""},
		{"sort satisfied by _id", ``, `{_id: -1}`, "_id_", ""},
		{"range must follow sort", `{b: 1, a: 2, d: {$gt: 1}}`, `{c: -1}`, "", "b_1_a_1_c_-1"},
		{"equality reordered to extend", `{a: 2, b: 1, d: {$gt: 1}}`, `{c: -1}`, "", "b_1_a_1_c_-1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sugs := Suggest(doc(t, c.filter), doc(t, c.sort))
			if len(sugs) != 1 {
				t.Fatalf("suggestions %+v", sugs)
			}
			sat, s := sugs[0].Against(ixs)
			if sat != c.satisfied || s.Extends != c.extends {
				t.Fatalf("satisfied %q extends %q; want %q %q", sat, s.Extends, c.satisfied, c.extends)
			}
		})
	}
}

func TestAgainstExtends(t *testing.T) {
	cases := []struct {
		name, index, filter, sort, keys, extends string
	}{
		{"literal prefix", `{name: "a_1", key: {a: 1}}`, `{a: 1, z: 2}`, ``, `{"a": 1, "z": 1}`, "a_1"},
		{"reversed prefix", `{name: "a_-1", key: {a: -1}}`, `{a: 1, z: 2}`, ``, `{"a": 1, "z": 1}`, "a_-1"},
		{"equality reordered", `{name: "b_1", key: {b: 1}}`, `{a: 1, b: 2}`, ``, `{"b": 1, "a": 1}`, "b_1"},
		{"three fields reordered", `{name: "b_1_a_1", key: {b: 1, a: 1}}`, `{a: 1, b: 1, c: 1}`, ``, `{"b": 1, "a": 1, "c": 1}`, "b_1_a_1"},
		{"sort breaks prefix", `{name: "b_1_c_1", key: {b: 1, c: 1}}`, `{a: 1, b: 2}`, `{c: 1}`, `{"a": 1, "b": 1, "c": 1}`, ""},
		{"mixed directions", `{name: "a_1_c_-1", key: {a: 1, c: -1}}`, `{a: 1, d: {$gt: 1}}`, `{c: 1}`, `{"a": 1, "c": 1, "d": 1}`, ""},
		{"ttl never extended", `{name: "created_1", key: {created: 1}, expireAfterSeconds: 3600}`, `{created: {$gt: 1}, status: 1}`, ``, `{"status": 1, "created": 1}`, ""},
		{"ttl equality never extended", `{name: "created_1", key: {created: 1}, expireAfterSeconds: 3600}`, `{created: 1, status: 1}`, ``, `{"created": 1, "status": 1}`, ""},
		{"sparse never extended", `{name: "a_1", key: {a: 1}, sparse: true}`, `{a: 1, z: 2}`, ``, `{"a": 1, "z": 1}`, ""},
		{"collation never extended", `{name: "a_1", key: {a: 1}, collation: {locale: "fr"}}`, `{a: 1, z: 2}`, ``, `{"a": 1, "z": 1}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sugs := Suggest(doc(t, c.filter), doc(t, c.sort))
			if len(sugs) != 1 {
				t.Fatalf("suggestions %+v", sugs)
			}
			sat, s := sugs[0].Against(indexes(t, c.index))
			if sat != "" {
				t.Fatalf("unexpectedly satisfied by %s", sat)
			}
			if got := KeysString(s.Keys()); got != c.keys || s.Extends != c.extends {
				t.Fatalf("keys %s extends %q; want %s %q", got, s.Extends, c.keys, c.extends)
			}
			if s.Extends != "" {
				ix := indexes(t, c.index)[0]
				if !ix.IsPrefixOf(Index{Keys: s.Keys()}) {
					t.Fatalf("%s is not a literal prefix of %s", c.index, KeysString(s.Keys()))
				}
			}
		})
	}
}

func TestAgainstSparse(t *testing.T) {
	ixs := indexes(t, `{name: "a_1", key: {a: 1}, sparse: true}`, `{name: "a_1_b_1", key: {a: 1, b: 1}, sparse: true}`)
	cases := []struct {
		filter, sort, satisfied string
	}{
		{`{a: 5}`, ``, "a_1"},
		{`{a: "x"}`, ``, "a_1"},
		{`{a: null}`, ``, ""},
		{`{a: {$eq: null}}`, ``, ""},
		{`{a: {$gt: 1}}`, ``, "a_1"},
		{`{a: {$gte: null}}`, ``, ""},
		{`{a: {$in: [1, 2]}}`, ``, "a_1"},
		{`{a: {$in: [1, null]}}`, ``, ""},
		{`{a: {$exists: true}}`, ``, "a_1"},
		{`{a: {$exists: false}}`, ``, ""},
		{`{a: /^x/}`, ``, "a_1"},
		{`{a: {$ne: 1}}`, ``, ""},
		{`{a: {$ne: 1, $gt: 0}}`, ``, "a_1"},
		{`{a: {$nin: [1]}}`, ``, ""},
		{`{a: 1, b: null}`, ``, ""},
		{`{a: 1, b: 2}`, ``, "a_1_b_1"},
		{``, `{a: 1}`, ""},
		{`{$and: [{a: null}, {a: 5}]}`, ``, "a_1"},
	}
	for _, c := range cases {
		t.Run(c.filter+c.sort, func(t *testing.T) {
			sugs := Suggest(doc(t, c.filter), doc(t, c.sort))
			if len(sugs) != 1 {
				t.Fatalf("suggestions %+v", sugs)
			}
			if sat, _ := sugs[0].Against(ixs); sat != c.satisfied {
				t.Fatalf("satisfied %q, want %q", sat, c.satisfied)
			}
		})
	}
}

func TestIndexParsing(t *testing.T) {
	ix := ParseIndex(doc(t, `{name: "w", key: {"$**": 1}}`))
	if !ix.Special {
		t.Fatal("wildcard should be special")
	}
	ix = ParseIndex(doc(t, `{name: "t", key: {created: 1}, expireAfterSeconds: 60}`))
	if !ix.TTL {
		t.Fatal("ttl")
	}
	keys := parseKeys("{ _id: 1 }")
	if len(keys) != 1 || keys[0].Field != "_id" || keys[0].Dir != 1 {
		t.Fatalf("string key pattern: %+v", keys)
	}
	a := ParseIndex(doc(t, `{name: "a", key: {a: 1, b: -1}}`))
	b := ParseIndex(doc(t, `{name: "b", key: {a: -1, b: 1, c: 1}}`))
	c := ParseIndex(doc(t, `{name: "c", key: {a: 1, b: 1, c: 1}}`))
	if !a.IsPrefixOf(b) || a.IsPrefixOf(c) || b.IsPrefixOf(a) {
		t.Fatal("IsPrefixOf")
	}
}

func TestCollRef(t *testing.T) {
	for in, want := range map[string]string{
		"orders":     "db.orders",
		"my-coll":    `db.getCollection("my-coll")`,
		"a.b":        `db.getCollection("a.b")`,
		"1x":         `db.getCollection("1x")`,
		"runCommand": `db.getCollection("runCommand")`,
	} {
		if got := CollRef(in); got != want {
			t.Errorf("CollRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatting(t *testing.T) {
	if Count(1234567) != "1,234,567" || Count(-1000) != "-1,000" || Count(12) != "12" {
		t.Fatal("Count")
	}
	if ratio(5000, 10) != "500:1" || ratio(15, 10) != "1.5:1" || ratio(5, 0) != "5.0:1" {
		t.Fatal("ratio", ratio(5000, 10), ratio(15, 10), ratio(5, 0))
	}
	if Millis(4) != "4ms" || Millis(2.5) != "2.5ms" || Millis(1500) != "1.5s" {
		t.Fatal("Millis", Millis(4), Millis(2.5), Millis(1500))
	}
}
