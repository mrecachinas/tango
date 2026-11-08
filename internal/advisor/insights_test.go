package advisor

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func loadInsights(t *testing.T, version string) InsightsInput {
	t.Helper()
	b, err := os.ReadFile("testdata/insights_mongo" + version + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var d bson.D
	if err := bson.UnmarshalExtJSON(b, true, &d); err != nil {
		t.Fatal(err)
	}
	in := InsightsInput{
		DB:           "shop",
		Now:          time.Now(),
		ProfileLevel: int(getInt(getDoc(d, "level"), "was")),
		SlowMs:       getInt(getDoc(d, "level"), "slowms"),
		Profile:      asDocs(getArr(d, "profile")),
		Top:          getDoc(d, "top"),
		CurrentOps:   asDocs(getArr(d, "inprog")),
	}
	for _, c := range asDocs(getArr(d, "collections")) {
		in.Collections = append(in.Collections, CollectionInfo{
			Name:       getStr(c, "name"),
			Indexes:    asDocs(getArr(c, "indexes")),
			IndexStats: asDocs(getArr(c, "indexStats")),
			IndexSizes: getDoc(c, "indexSizes"),
		})
	}
	return in
}

func TestDumpInsights(t *testing.T) {
	if os.Getenv("DUMP") == "" {
		t.Skip("set DUMP=1 to print insights")
	}
	for _, v := range []string{"7", "8"} {
		ins := Analyze(loadInsights(t, v))
		fmt.Printf("===== mongo %s: level %d, %d entries\n", v, ins.ProfileLevel, ins.ProfileEntries)
		for _, r := range ins.Recommendations {
			fmt.Printf("[%d] %s\n    %s\n    %s %v\n", r.Severity, r.Title, strings.ReplaceAll(r.Detail, "\n", "\n    "), r.Command, r.Refs)
		}
		for _, s := range ins.Shapes {
			fmt.Printf("%-4s %-12s n=%d total=%s max=%s docs=%d keys=%d ret=%d plan=%q scan=%v %s\n",
				s.ID, s.NS, s.Count, Millis(s.TotalMs), Millis(s.MaxMs), s.Docs, s.Keys, s.Returned, s.PlanSummary, s.CollScan, s.Shape)
		}
		for _, h := range ins.Hot {
			fmt.Printf("hot %+v\n", h)
		}
		for _, u := range ins.Indexes {
			fmt.Printf("idx %s.%s %s ops=%d size=%s unused=%v redundant=%q\n", u.Coll, u.Name, u.Keys, u.Ops, Bytes(u.Size), u.Unused(), u.RedundantWith)
		}
		for _, o := range ins.Running {
			fmt.Printf("run %+v\n", o)
		}
	}
}

func findRec(recs []Recommendation, substr string) *Recommendation {
	for i := range recs {
		if strings.Contains(recs[i].Title, substr) {
			return &recs[i]
		}
	}
	return nil
}

func TestInsightsFixtures(t *testing.T) {
	for _, v := range []string{"7", "8"} {
		t.Run("mongo"+v, func(t *testing.T) {
			ins := Analyze(loadInsights(t, v))
			if ins.ProfileLevel != 1 || ins.ProfileEntries == 0 {
				t.Fatalf("level %d entries %d", ins.ProfileLevel, ins.ProfileEntries)
			}

			shapes := map[string]ShapeStat{}
			for _, s := range ins.Shapes {
				shapes[s.NS+" "+s.Shape] = s
			}
			cust := shapes[`shop.orders find {"cust": "?"}`]
			if cust.Count != 5 || !cust.CollScan || cust.Docs != 5*20000 || cust.ID == "" {
				t.Errorf("cust shape = %+v", cust)
			}
			// getMores charge the originating find without counting as runs.
			batched := shapes[`shop.orders find {"qty": {"$gt": "?"}, "status": "?"}`]
			if batched.Count != 1 || batched.Returned != 45 {
				t.Errorf("batched shape = %+v", batched)
			}
			if s := shapes[`shop.orders update {"cust": "?"}`]; s.Count != 1 || !s.CollScan {
				t.Errorf("update shape = %+v", s)
			}
			if s := shapes[`shop.orders delete {"cust": "?"}`]; s.Count != 1 || !s.CollScan {
				t.Errorf("delete shape = %+v", s)
			}
			for i := 1; i < len(ins.Shapes); i++ {
				if ins.Shapes[i].TotalMs > ins.Shapes[i-1].TotalMs {
					t.Fatalf("shapes not ranked by time")
				}
			}

			// The cust find, update and delete share one suggested index.
			r := findRec(ins.Recommendations, "Add an index on orders")
			if r == nil || r.Severity != Bad || r.Command != `db.orders.createIndex({"cust": 1})` || len(r.Refs) != 3 {
				t.Fatalf("cust index rec = %+v", r)
			}
			if r := findRec(ins.Recommendations, "Q1 averages"); r == nil || r.Command != ":explain Q1" || !strings.Contains(r.Detail, "$where") {
				t.Errorf("slow $where rec = %+v", r)
			}
			if findRec(ins.Recommendations, "on users") != nil {
				t.Errorf("tiny users collection should not get an index: %+v", ins.Recommendations)
			}
			if r := findRec(ins.Recommendations, "status_1 ("); r == nil || !strings.Contains(r.Title, "redundant with status_1_qty_1") ||
				r.Command != `db.orders.hideIndex("status_1")` {
				t.Errorf("redundant rec = %+v", r)
			}
			if r := findRec(ins.Recommendations, "tags_1"); r == nil || !strings.Contains(r.Title, "not been used") {
				t.Errorf("unused rec = %+v", r)
			}
			for _, name := range []string{"_id_", "sku_1"} {
				if r := findRec(ins.Recommendations, "."+name); r != nil {
					t.Errorf("%s must not be flagged: %+v", name, r)
				}
			}
			for i := 1; i < len(ins.Recommendations); i++ {
				if ins.Recommendations[i].Severity > ins.Recommendations[i-1].Severity {
					t.Fatalf("recommendations not ranked")
				}
			}

			if len(ins.Running) != 1 || ins.Running[0].NS != "shop.users" || ins.Running[0].Plan != "COLLSCAN" {
				t.Errorf("running = %+v", ins.Running)
			}
			if len(ins.Hot) == 0 || ins.Hot[0].NS != "shop.orders" {
				t.Errorf("hot = %+v", ins.Hot)
			}
			for _, h := range ins.Hot {
				if strings.Contains(h.NS, ".system.") {
					t.Errorf("system collection in hot list: %s", h.NS)
				}
			}
		})
	}
}

func profileEntry(op, ns string, cmd bson.D, ms, docs, keys, ret int64, plan string, ts time.Time) bson.D {
	return bson.D{
		{Key: "op", Value: op}, {Key: "ns", Value: ns}, {Key: "command", Value: cmd},
		{Key: "millis", Value: ms}, {Key: "docsExamined", Value: docs}, {Key: "keysExamined", Value: keys},
		{Key: "nreturned", Value: ret}, {Key: "planSummary", Value: plan}, {Key: "ts", Value: ts},
	}
}

func TestInsightsRules(t *testing.T) {
	now := time.Now()
	find := func(f bson.D) bson.D { return bson.D{{Key: "find", Value: "c"}, {Key: "filter", Value: f}} }
	idx := func(name string, keys bson.D, extra ...bson.E) bson.D {
		return append(bson.D{{Key: "name", Value: name}, {Key: "key", Value: keys}}, extra...)
	}
	in := InsightsInput{
		DB: "d", Now: now, ProfileLevel: 1, SlowMs: 100,
		Profile: []bson.D{
			// Uses an existing index badly: an index fits, so explain is suggested.
			profileEntry("query", "d.c", find(bson.D{{Key: "a", Value: 1}}), 300, 50000, 0, 1, "COLLSCAN", now),
			// Unfiltered scan: no index can help.
			profileEntry("query", "d.c", find(bson.D{}), 200, 50000, 0, 50000, "COLLSCAN", now),
			// Index-backed and selective: nothing to do.
			profileEntry("query", "d.c", find(bson.D{{Key: "b", Value: 1}}), 900, 10, 10, 10, "IXSCAN { b: 1 }", now),
			// Grouping returns little by design: not flagged without a COLLSCAN.
			profileEntry("command", "d.c", bson.D{{Key: "aggregate", Value: "c"}, {Key: "pipeline", Value: bson.A{
				bson.D{{Key: "$match", Value: bson.D{{Key: "b", Value: 1}}}}, bson.D{{Key: "$group", Value: bson.D{{Key: "_id", Value: nil}}}},
			}}}, 800, 90000, 90000, 1, "IXSCAN { b: 1 }", now),
			profileEntry("query", "d.system.profile", find(bson.D{}), 5000, 1, 0, 1, "COLLSCAN", now),
		},
		Collections: []CollectionInfo{{
			Name: "c",
			Indexes: []bson.D{
				idx("_id_", bson.D{{Key: "_id", Value: 1}}),
				idx("a_1", bson.D{{Key: "a", Value: 1}}),
				idx("b_1", bson.D{{Key: "b", Value: 1}}),
				idx("b_1_c_1", bson.D{{Key: "b", Value: 1}, {Key: "c", Value: 1}}, bson.E{Key: "unique", Value: true}),
				idx("b_-1", bson.D{{Key: "b", Value: -1}}),
				idx("ttl", bson.D{{Key: "t", Value: 1}}, bson.E{Key: "expireAfterSeconds", Value: 60}),
				idx("p_1", bson.D{{Key: "p", Value: 1}}, bson.E{Key: "partialFilterExpression", Value: bson.D{{Key: "p", Value: bson.D{{Key: "$exists", Value: true}}}}}),
			},
			IndexStats: []bson.D{
				{{Key: "name", Value: "_id_"}, {Key: "accesses", Value: bson.D{{Key: "ops", Value: int64(0)}}}},
				{{Key: "name", Value: "a_1"}, {Key: "accesses", Value: bson.D{{Key: "ops", Value: int64(3)}}}},
				{{Key: "name", Value: "ttl"}, {Key: "accesses", Value: bson.D{{Key: "ops", Value: int64(0)}}}},
				{{Key: "name", Value: "p_1"}, {Key: "accesses", Value: bson.D{{Key: "ops", Value: int64(0)}, {Key: "since", Value: now}}}},
			},
			IndexSizes: bson.D{{Key: "p_1", Value: int32(4096)}},
		}},
		CurrentOps: []bson.D{
			{{Key: "op", Value: "query"}, {Key: "ns", Value: "d.c"}, {Key: "opid", Value: int32(7)}, {Key: "secs_running", Value: int64(42)}, {Key: "planSummary", Value: "COLLSCAN"}},
			{{Key: "op", Value: "update"}, {Key: "ns", Value: "d.c"}, {Key: "opid", Value: int32(8)}, {Key: "secs_running", Value: int64(30)}, {Key: "waitingForLock", Value: true}},
			{{Key: "op", Value: "getmore"}, {Key: "ns", Value: "d.c"}, {Key: "secs_running", Value: int64(99)},
				{Key: "command", Value: bson.D{{Key: "getMore", Value: int64(1)}}}, {Key: "cursor", Value: bson.D{{Key: "tailable", Value: true}}}},
			{{Key: "op", Value: "query"}, {Key: "ns", Value: "other.c"}, {Key: "secs_running", Value: int64(99)}},
			{{Key: "op", Value: "query"}, {Key: "ns", Value: "d.c"}, {Key: "secs_running", Value: int64(0)}},
		},
	}
	ins := Analyze(in)

	if len(ins.Shapes) != 4 {
		t.Fatalf("shapes = %d, want 4 (system.profile skipped)", len(ins.Shapes))
	}
	if r := findRec(ins.Recommendations, "although a_1 fits"); r == nil || r.Command != ":explain Q3" {
		t.Errorf("fits rec = %+v", r)
	}
	if r := findRec(ins.Recommendations, "with no filter"); r == nil {
		t.Errorf("unfiltered rec missing")
	}
	if r := findRec(ins.Recommendations, "Add an index"); r != nil {
		t.Errorf("unexpected index rec %+v", r)
	}
	if r := findRec(ins.Recommendations, "c.p_1 (4.0 KB) has not been used since"); r == nil {
		t.Errorf("partial index unused rec missing: %+v", ins.Recommendations)
	}
	for _, name := range []string{"c._id_", "c.ttl", "c.a_1 "} {
		if r := findRec(ins.Recommendations, name); r != nil {
			t.Errorf("%s flagged: %+v", name, r)
		}
	}
	// b_1 and b_-1 are both prefixes of the unique b_1_c_1; b_-1 also mirrors b_1.
	for _, name := range []string{"c.b_1 is redundant", "c.b_-1 is redundant"} {
		if findRec(ins.Recommendations, name) == nil {
			t.Errorf("missing %q in %+v", name, ins.Recommendations)
		}
	}
	if len(ins.Running) != 2 || ins.Running[0].Secs != 42 {
		t.Fatalf("running = %+v", ins.Running)
	}
	if r := findRec(ins.Recommendations, "query on d.c has run for 42s"); r == nil || r.Severity != Bad || r.Command != ":killop 7" {
		t.Errorf("killop rec = %+v", r)
	}
	if r := findRec(ins.Recommendations, "update on d.c"); r == nil || !strings.Contains(r.Detail, "lock") {
		t.Errorf("lock rec = %+v", r)
	}
}

func TestInsightsProfilerState(t *testing.T) {
	cases := []struct {
		in    InsightsInput
		title string
		cmd   string
	}{
		{InsightsInput{DB: "d", ProfileLevel: 0}, "profiler is off in d", "db.setProfilingLevel(1, {slowms: 50})"},
		{InsightsInput{DB: "d", ProfileLevel: 2, SlowMs: 100}, "every operation", "db.setProfilingLevel(1, {slowms: 100})"},
		{InsightsInput{DB: "d", ProfileLevel: -1, ProfileErr: errors.New("not authorized")}, "Can't read the profiler", ""},
	}
	for _, c := range cases {
		ins := Analyze(c.in)
		if r := findRec(ins.Recommendations, c.title); r == nil || r.Command != c.cmd {
			t.Errorf("%q: %+v", c.title, ins.Recommendations)
		}
	}
	if ins := Analyze(InsightsInput{DB: "d", ProfileLevel: 1}); len(ins.Recommendations) != 0 {
		t.Errorf("quiet db: %+v", ins.Recommendations)
	}
}

func TestInsightsRedundancySkipsSpecialIndexes(t *testing.T) {
	idx := func(name string, keys bson.D) bson.D {
		return bson.D{{Key: "name", Value: name}, {Key: "key", Value: keys}}
	}
	in := InsightsInput{DB: "d", ProfileLevel: 1, Collections: []CollectionInfo{{
		Name: "c",
		Indexes: []bson.D{
			idx("a_1", bson.D{{Key: "a", Value: 1}}),
			idx("a_1_loc_2dsphere", bson.D{{Key: "a", Value: 1}, {Key: "loc", Value: "2dsphere"}}),
			idx("b_1", bson.D{{Key: "b", Value: 1}}),
			idx("b_1_t_text", bson.D{{Key: "b", Value: 1}, {Key: "_fts", Value: "text"}, {Key: "_ftsx", Value: 1}}),
			idx("h_hashed", bson.D{{Key: "h", Value: "hashed"}}),
			idx("h_1_x_1", bson.D{{Key: "h", Value: 1}, {Key: "x", Value: 1}}),
			idx("w_1", bson.D{{Key: "w.$**", Value: 1}}),
			idx("w_1_y_1", bson.D{{Key: "w.$**", Value: 1}, {Key: "y", Value: 1}}),
			idx("p_1", bson.D{{Key: "p", Value: 1}}),
			idx("p_1_q_1", bson.D{{Key: "p", Value: 1}, {Key: "q", Value: 1}}),
		},
	}}}
	got := map[string]string{}
	for _, u := range Analyze(in).Indexes {
		got[u.Name] = u.RedundantWith
	}
	for _, name := range []string{"a_1", "b_1", "h_hashed", "w_1"} {
		if got[name] != "" {
			t.Errorf("%s flagged redundant with %s", name, got[name])
		}
	}
	if got["p_1"] != "p_1_q_1" {
		t.Errorf("plain prefix not flagged: %+v", got)
	}
}

func TestInsightsUnknownCatalog(t *testing.T) {
	now := time.Now()
	find := func(coll string) bson.D {
		return bson.D{{Key: "find", Value: coll}, {Key: "filter", Value: bson.D{{Key: "a", Value: 1}}}}
	}
	ids := []bson.D{{{Key: "name", Value: "_id_"}, {Key: "key", Value: bson.D{{Key: "_id", Value: 1}}}}}
	in := InsightsInput{
		DB: "d", Now: now, ProfileLevel: 1,
		Profile: []bson.D{
			profileEntry("query", "d.known", find("known"), 300, 50000, 0, 1, "COLLSCAN", now),
			profileEntry("query", "d.failed", find("failed"), 200, 50000, 0, 1, "COLLSCAN", now),
			profileEntry("query", "d.capped", find("capped"), 100, 50000, 0, 1, "COLLSCAN", now),
		},
		Collections: []CollectionInfo{
			{Name: "known", Indexes: ids},
			{Name: "failed", IndexesErr: errors.New("timeout")},
		},
	}
	ins := Analyze(in)
	if r := findRec(ins.Recommendations, "Add an index on known for Q1"); r == nil {
		t.Errorf("known catalog should get an index rec: %+v", ins.Recommendations)
	}
	for _, c := range []struct{ coll, id string }{{"failed", "Q2"}, {"capped", "Q3"}} {
		if r := findRec(ins.Recommendations, "Add an index on "+c.coll); r != nil {
			t.Errorf("unknown catalog for %s got an index rec: %+v", c.coll, r)
		}
		if r := findRec(ins.Recommendations, c.id+" scans "+c.coll+", whose indexes weren't read"); r == nil || r.Command != ":explain "+c.id {
			t.Errorf("%s: unknown catalog rec = %+v", c.coll, r)
		}
	}
	for _, s := range ins.Shapes {
		if s.NS != "d.known" && s.Missing != nil {
			t.Errorf("%s: Missing set without a catalog: %+v", s.NS, s.Missing)
		}
	}
}

func TestInsightsShardedIndexStats(t *testing.T) {
	early, late := time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour)
	stat := func(name, shard string, ops int64, since time.Time) bson.D {
		return bson.D{{Key: "name", Value: name}, {Key: "shard", Value: shard},
			{Key: "accesses", Value: bson.D{{Key: "ops", Value: ops}, {Key: "since", Value: since}}}}
	}
	idx := func(name, field string) bson.D {
		return bson.D{{Key: "name", Value: name}, {Key: "key", Value: bson.D{{Key: field, Value: 1}}}}
	}
	in := InsightsInput{DB: "d", ProfileLevel: 1, Collections: []CollectionInfo{{
		Name:    "c",
		Indexes: []bson.D{idx("a_1", "a"), idx("b_1", "b")},
		IndexStats: []bson.D{
			stat("a_1", "s0", 500, early), stat("a_1", "s1", 0, late),
			stat("b_1", "s0", 0, early), stat("b_1", "s1", 0, late),
		},
	}}}
	ins := Analyze(in)
	got := map[string]IndexUsage{}
	for _, u := range ins.Indexes {
		got[u.Name] = u
	}
	if a := got["a_1"]; a.Ops != 500 || a.Unused() {
		t.Errorf("a_1 = %+v, want ops summed across shards", a)
	}
	if b := got["b_1"]; b.Ops != 0 || !b.Unused() || !b.Since.Equal(late) {
		t.Errorf("b_1 = %+v, want unused since the latest shard reset", b)
	}
	if findRec(ins.Recommendations, "c.a_1") != nil {
		t.Errorf("a_1 flagged: %+v", ins.Recommendations)
	}
}
