package advisor

import (
	"os"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
)

func loadFixtures(t *testing.T, version string) bson.D {
	t.Helper()
	b, err := os.ReadFile("testdata/explain_mongo" + version + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var d bson.D
	if err := bson.UnmarshalExtJSON(b, true, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDumpReports(t *testing.T) {
	if os.Getenv("DUMP") == "" {
		t.Skip()
	}
	for _, v := range []string{"7", "8"} {
		for _, e := range loadFixtures(t, v) {
			raw := e.Value.(bson.D)
			p, err := ParseExplain(raw)
			if err != nil {
				t.Fatalf("%s/%s: %v", v, e.Key, err)
			}
			op, _ := OpFromCommand(getDoc(raw, "command"))
			a := Assess(p, &op, nil)
			t.Logf("==== mongo %s %s\n%s", v, e.Key, mjson.Join(a.Report("executionStats", op.ShapeText(), nil)))
		}
	}
}

var shopIndexes = []string{
	`{v: 2, key: {_id: 1}, name: "_id_"}`,
	`{v: 2, key: {status: 1}, name: "status_1"}`,
	`{v: 2, key: {status: 1, qty: 1}, name: "status_1_qty_1"}`,
	`{v: 2, key: {tags: 1}, name: "tags_1"}`,
	`{v: 2, key: {sku: 1}, name: "sku_1", unique: true}`,
}

func assess(t *testing.T, raw bson.D) Analysis {
	t.Helper()
	p, err := ParseExplain(raw)
	if err != nil {
		t.Fatal(err)
	}
	op, ok := OpFromCommand(getDoc(raw, "command"))
	if !ok {
		t.Fatalf("command not recognized: %v", getDoc(raw, "command"))
	}
	var specs []bson.D
	for _, s := range shopIndexes {
		specs = append(specs, doc(t, s))
	}
	return Assess(p, &op, specs)
}

func findingWith(a Analysis, sev Severity, text string) bool {
	for _, f := range a.Findings {
		if f.Severity == sev && strings.Contains(f.Text, text) {
			return true
		}
	}
	return false
}

func TestExplainFixtures(t *testing.T) {
	type want struct {
		flow     string // substring of the plan flow
		returned int64
		finding  []string // "sev:text"
		suggest  string   // key pattern; "" means none
		extends  string
		noIssues bool // no Warn or Bad findings
	}
	cases := map[string]want{
		"collscan":    {flow: "COLLSCAN", returned: 20, finding: []string{"bad:Collection scan"}, suggest: `{"cust": 1}`},
		"sort":        {flow: "IXSCAN status_1 → FETCH → SORT", returned: 10, finding: []string{"warn:In-memory sort", "warn:500:1"}, suggest: `{"status": 1, "created": -1}`, extends: "status_1"},
		"id":          {returned: 1, finding: []string{"good:Express path"}, noIssues: true},
		"covered":     {flow: "PROJECTION_COVERED", returned: 5000, finding: []string{"good:Covered query"}, noIssues: true},
		"fetchfilter": {flow: "IXSCAN status_1 → FETCH", finding: []string{"warn:Field cust is filtered after fetching"}, suggest: `{"status": 1, "cust": 1}`, extends: "status_1"},
		"good":        {returned: 1, noIssues: true},
		"group":       {flow: "status_1", noIssues: true},
		"lookup":      {returned: 20, noIssues: true},
		"count":       {flow: "COUNT_SCAN status_1", returned: 5000, finding: []string{"good:Count answered"}, noIssues: true},
		"distinct":    {flow: "DISTINCT_SCAN status_1", returned: 4, finding: []string{"good:Distinct values"}, noIssues: true},
		"update":      {flow: "COLLSCAN → UPDATE", returned: 1, finding: []string{"bad:Collection scan"}, suggest: `{"cust": 1}`},
		"out_planner": {flow: "IXSCAN status_1 → FETCH", noIssues: true},
		"out_exec":    {flow: "IXSCAN status_1 → FETCH", returned: 5000, noIssues: true},
		"timeout":     {flow: "COLLSCAN", finding: []string{"bad:time limit", "bad:Collection scan"}},
	}
	sev := map[string]Severity{"good": Good, "info": Info, "warn": Warn, "bad": Bad}
	for _, v := range []string{"7", "8"} {
		fixtures := loadFixtures(t, v)
		if len(fixtures) != len(cases) {
			t.Fatalf("mongo %s: %d fixtures, %d cases", v, len(fixtures), len(cases))
		}
		for _, e := range fixtures {
			w, ok := cases[e.Key]
			if !ok {
				t.Fatalf("no expectations for fixture %s", e.Key)
			}
			t.Run("mongo"+v+"/"+e.Key, func(t *testing.T) {
				raw := e.Value.(bson.D)
				a := assess(t, raw)
				if !strings.Contains(a.Plan.Winning.Flow(), w.flow) {
					t.Errorf("flow %q, want %q", a.Plan.Winning.Flow(), w.flow)
				}
				if w.returned != 0 && a.Plan.Returned() != w.returned {
					t.Errorf("returned %d, want %d", a.Plan.Returned(), w.returned)
				}
				for _, f := range w.finding {
					s, text, _ := strings.Cut(f, ":")
					if !findingWith(a, sev[s], text) {
						t.Errorf("missing %s finding %q in %+v", s, text, a.Findings)
					}
				}
				if w.noIssues && hasSeverity(a.Findings, Warn, Bad) {
					t.Errorf("unexpected issues: %+v", a.Findings)
				}
				var got string
				if len(a.Suggestions) > 0 {
					got = KeysString(a.Suggestions[0].Keys())
					if a.Suggestions[0].Extends != w.extends {
						t.Errorf("extends %q, want %q", a.Suggestions[0].Extends, w.extends)
					}
				}
				if got != w.suggest {
					t.Errorf("suggestion %q, want %q", got, w.suggest)
				}
				toks := a.Report("executionStats", a.Op.ShapeText(), raw)
				if txt := mjson.Join(toks); !strings.Contains(txt, "Winning plan") || !strings.Contains(txt, "Raw explain") {
					t.Errorf("report missing sections:\n%s", txt)
				}
			})
		}
	}
}

func TestExplainSBEDetails(t *testing.T) {
	fx := loadFixtures(t, "8")
	group := assess(t, getDoc(fx, "group"))
	if group.Plan.Engine != "sbe" || group.Plan.RatioReliable() {
		t.Fatalf("SBE group: engine %s reliable %v", group.Plan.Engine, group.Plan.RatioReliable())
	}
	lookup := assess(t, getDoc(fx, "lookup"))
	if !findingWith(lookup, Info, "hash join") || !lookup.Plan.Winning.has("EQ_LOOKUP") {
		t.Fatalf("lookup: %+v", lookup.Findings)
	}
	id := assess(t, getDoc(fx, "id"))
	if used := id.Plan.Winning.IndexesUsed(); len(used) != 1 || used[0] != "_id_" {
		t.Fatalf("express index: %v", used)
	}
}

func TestExistingIndexNotChosen(t *testing.T) {
	raw := getDoc(loadFixtures(t, "7"), "fetchfilter")
	p, _ := ParseExplain(raw)
	op, _ := OpFromCommand(getDoc(raw, "command"))
	specs := []bson.D{doc(t, `{key: {status: 1}, name: "status_1"}`), doc(t, `{key: {cust: 1, status: 1}, name: "cust_1_status_1"}`)}
	a := Assess(p, &op, specs)
	if len(a.Suggestions) != 0 || !findingWith(a, Info, "cust_1_status_1 already fits") {
		t.Fatalf("suggestions %+v findings %+v", a.Suggestions, a.Findings)
	}
}

func TestParseExplainErrors(t *testing.T) {
	if _, err := ParseExplain(bson.D{{Key: "ok", Value: 1}}); err == nil {
		t.Fatal("want error for output without a plan")
	}
}
