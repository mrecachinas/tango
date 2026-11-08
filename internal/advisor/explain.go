package advisor

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"go.mongodb.org/mongo-driver/bson"
)

// Stage is one node of a query plan tree.
type Stage struct {
	Name     string
	Shard    string
	Spec     bson.D // the node's own fields, without children
	Children []*Stage

	Stats                 bool // per-stage execution stats are present
	NReturned, Keys, Docs int64
	TimeMs                int64
}

// PipeStage is an aggregation stage that runs after the query layer.
type PipeStage struct {
	Name      string
	NReturned int64
	TimeMs    int64
	Stats     bool
	UsedDisk  bool
}

// Plan is a parsed explain result.
type Plan struct {
	Namespace string
	Version   string
	Engine    string // classic or sbe
	Winning   *Stage
	Rejected  []*Stage
	Pipeline  []PipeStage

	HasStats              bool
	Success               bool
	Error                 string
	NReturned, Keys, Docs int64
	TimeMs                int64
}

var childKeys = map[string]bool{
	"inputStage": true, "inputStages": true, "outerStage": true, "innerStage": true,
	"thenStage": true, "elseStage": true, "shards": true,
}

func parseStage(d bson.D) *Stage {
	if q := getDoc(d, "queryPlan"); q != nil && getStr(d, "stage") == "" {
		d = q
	}
	st := &Stage{Name: getStr(d, "stage")}
	for _, e := range d {
		if !childKeys[e.Key] {
			st.Spec = append(st.Spec, e)
			continue
		}
		switch e.Key {
		case "inputStages":
			for _, c := range asDocs(toArr(e.Value)) {
				st.Children = append(st.Children, parseStage(c))
			}
		case "shards":
			for _, sh := range asDocs(toArr(e.Value)) {
				if c := parseShard(sh); c != nil {
					st.Children = append(st.Children, c)
				}
			}
		default:
			if c := toD(e.Value); c != nil {
				st.Children = append(st.Children, parseStage(c))
			}
		}
	}
	if n, ok := getNum(d, "nReturned"); ok {
		st.Stats, st.NReturned = true, int64(n)
		st.Keys = getInt(d, "keysExamined")
		st.Docs = getInt(d, "docsExamined")
		st.TimeMs = getInt(d, "executionTimeMillisEstimate")
		if t, ok := getNum(d, "executionTimeMillis"); ok {
			st.TimeMs = int64(t)
			st.Keys, st.Docs = getInt(d, "totalKeysExamined"), getInt(d, "totalDocsExamined")
		}
	}
	return st
}

func parseShard(sh bson.D) *Stage {
	plan := getDoc(sh, "executionStages")
	if plan == nil {
		plan = getDoc(sh, "winningPlan")
	}
	if plan == nil {
		return nil
	}
	st := parseStage(plan)
	st.Shard = getStr(sh, "shardName")
	if !st.Stats {
		if n, ok := getNum(sh, "nReturned"); ok {
			st.Stats, st.NReturned = true, int64(n)
			st.Keys, st.Docs = getInt(sh, "totalKeysExamined"), getInt(sh, "totalDocsExamined")
			st.TimeMs = getInt(sh, "executionTimeMillis")
		}
	}
	return st
}

// classicTree reports whether execution stages use classic (upper-case)
// stage names. SBE stages (lower-case) describe slots rather than the logical
// plan, so the winning plan is shown instead.
func classicTree(d bson.D) bool {
	name := getStr(d, "stage")
	if name == "" || !unicode.IsUpper([]rune(name)[0]) {
		return false
	}
	for _, sh := range asDocs(getArr(d, "shards")) {
		if es := getDoc(sh, "executionStages"); es != nil && !classicTree(es) {
			return false
		}
	}
	return true
}

// ParseExplain reads explain output for find, count, distinct, aggregate and
// write commands, from replica sets or sharded clusters, in either engine.
func ParseExplain(raw bson.D) (*Plan, error) {
	p := &Plan{Version: getStr(getDoc(raw, "serverInfo"), "version"), Success: true}
	qp, es := getDoc(raw, "queryPlanner"), getDoc(raw, "executionStats")
	if qp == nil {
		if stages := asDocs(getArr(raw, "stages")); len(stages) > 0 {
			for i, st := range stages {
				if c := getDoc(st, "$cursor"); i == 0 && c != nil {
					qp, es = getDoc(c, "queryPlanner"), getDoc(c, "executionStats")
					continue
				}
				p.Pipeline = append(p.Pipeline, pipeStage(st))
			}
		} else if shards := getDoc(raw, "shards"); shards != nil {
			return parseShardedAggregate(p, raw, shards)
		}
	}
	if qp == nil {
		return nil, fmt.Errorf("unrecognized explain output: no queryPlanner section")
	}
	p.Namespace = getStr(qp, "namespace")
	wp := getDoc(qp, "winningPlan")
	p.Engine = "classic"
	if getDoc(wp, "queryPlan") != nil || getDoc(wp, "slotBasedPlan") != nil {
		p.Engine = "sbe"
	}
	p.Winning = parseStage(wp)
	for _, r := range asDocs(getArr(qp, "rejectedPlans")) {
		p.Rejected = append(p.Rejected, parseStage(r))
	}
	for _, sh := range asDocs(getArr(wp, "shards")) {
		for _, r := range asDocs(getArr(sh, "rejectedPlans")) {
			rs := parseStage(r)
			rs.Shard = getStr(sh, "shardName")
			p.Rejected = append(p.Rejected, rs)
		}
	}
	if es != nil {
		p.applyStats(es)
	}
	return p, nil
}

func (p *Plan) applyStats(es bson.D) {
	p.HasStats = true
	if _, ok := mjsonHas(es, "executionSuccess"); ok {
		p.Success = getBool(es, "executionSuccess")
	}
	p.Error = getStr(es, "errorMessage")
	p.NReturned = getInt(es, "nReturned")
	p.Keys = getInt(es, "totalKeysExamined")
	p.Docs = getInt(es, "totalDocsExamined")
	p.TimeMs = getInt(es, "executionTimeMillis")
	root := getDoc(es, "executionStages")
	if root != nil && classicTree(root) {
		p.Winning = parseStage(root)
		return
	}
	// Annotate shard children of the winning plan with per-shard totals.
	byShard := map[string]bson.D{}
	for _, sh := range asDocs(getArr(root, "shards")) {
		byShard[getStr(sh, "shardName")] = sh
	}
	if p.Winning != nil {
		for _, c := range p.Winning.Children {
			if sh, ok := byShard[c.Shard]; ok && !c.Stats {
				c.Stats, c.NReturned = true, getInt(sh, "nReturned")
				c.Keys, c.Docs = getInt(sh, "totalKeysExamined"), getInt(sh, "totalDocsExamined")
				c.TimeMs = getInt(sh, "executionTimeMillis")
			}
		}
	}
}

func mjsonHas(d bson.D, key string) (interface{}, bool) {
	for _, e := range d {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func pipeStage(st bson.D) PipeStage {
	ps := PipeStage{UsedDisk: getBool(st, "usedDisk")}
	for _, e := range st {
		if strings.HasPrefix(e.Key, "$") {
			ps.Name = e.Key
			break
		}
	}
	if n, ok := getNum(st, "nReturned"); ok {
		ps.Stats, ps.NReturned = true, int64(n)
		ps.TimeMs = getInt(st, "executionTimeMillisEstimate")
	}
	return ps
}

// parseShardedAggregate handles aggregate explain from mongos, which nests a
// full explain per shard and describes the merge separately.
func parseShardedAggregate(p *Plan, raw, shards bson.D) (*Plan, error) {
	root := &Stage{Name: "SHARD_MERGE"}
	if mt := getStr(raw, "mergeType"); mt != "" {
		root.Spec = bson.D{{Key: "mergeType", Value: mt}}
	}
	for _, e := range shards {
		sub, err := ParseExplain(toD(e.Value))
		if err != nil || sub.Winning == nil {
			continue
		}
		sub.Winning.Shard = e.Key
		if sub.HasStats && !sub.Winning.Stats {
			sub.Winning.Stats, sub.Winning.NReturned = true, sub.NReturned
			sub.Winning.Keys, sub.Winning.Docs, sub.Winning.TimeMs = sub.Keys, sub.Docs, sub.TimeMs
		}
		root.Children = append(root.Children, sub.Winning)
		p.Rejected = append(p.Rejected, sub.Rejected...)
		p.Namespace, p.Engine = sub.Namespace, sub.Engine
		if sub.HasStats {
			p.HasStats = true
			p.Success = p.Success && sub.Success
			if sub.Error != "" {
				p.Error = sub.Error
			}
			p.NReturned += sub.NReturned
			p.Keys += sub.Keys
			p.Docs += sub.Docs
			p.TimeMs = max(p.TimeMs, sub.TimeMs)
		}
	}
	if len(root.Children) == 0 {
		return nil, fmt.Errorf("unrecognized sharded explain output")
	}
	if merger := getArr(getDoc(raw, "splitPipeline"), "mergerPart"); merger != nil {
		for _, st := range asDocs(merger) {
			p.Pipeline = append(p.Pipeline, pipeStage(st))
		}
	}
	p.Winning = root
	return p, nil
}

func (st *Stage) walk(fn func(st, parent *Stage)) {
	var rec func(s, parent *Stage)
	rec = func(s, parent *Stage) {
		fn(s, parent)
		for _, c := range s.Children {
			rec(c, s)
		}
	}
	rec(st, nil)
}

func (st *Stage) has(names ...string) bool {
	found := false
	st.walk(func(s, _ *Stage) {
		for _, n := range names {
			found = found || s.Name == n
		}
	})
	return found
}

func isIndexStage(name string) bool {
	return name == "IXSCAN" || name == "COUNT_SCAN" || name == "DISTINCT_SCAN" ||
		name == "IDHACK" || strings.HasPrefix(name, "EXPRESS")
}

func (st *Stage) indexName() string {
	if name := getStr(st.Spec, "indexName"); name != "" {
		return name
	}
	if st.Name == "IDHACK" || st.Name == "EXPRESS_IDHACK" {
		return "_id_"
	}
	return ""
}

// IndexesUsed lists the indexes in a plan tree, in order of appearance.
func (st *Stage) IndexesUsed() []string {
	var names []string
	seen := map[string]bool{}
	if st == nil {
		return nil
	}
	st.walk(func(s, _ *Stage) {
		if name := s.indexName(); name != "" && isIndexStage(s.Name) && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	})
	return names
}

// Flow summarizes a plan from leaf to root, e.g. "IXSCAN status_1 → FETCH".
func (st *Stage) Flow() string {
	if st == nil {
		return ""
	}
	label := st.Name
	if isIndexStage(st.Name) && st.indexName() != "" {
		label += " " + st.indexName()
	}
	switch len(st.Children) {
	case 0:
		return label
	case 1:
		return st.Children[0].Flow() + " → " + label
	}
	var parts []string
	seen := map[string]bool{}
	for _, c := range st.Children {
		if f := c.Flow(); !seen[f] {
			seen[f] = true
			parts = append(parts, f)
		}
	}
	return label + "(" + strings.Join(parts, " | ") + ")"
}

// Severity orders findings from good news to problems.
type Severity int

const (
	Good Severity = iota
	Info
	Warn
	Bad
)

// Finding is one observation about a plan.
type Finding struct {
	Severity Severity
	Text     string
}

// Analysis is a plan with its findings and index advice.
type Analysis struct {
	Plan        *Plan
	Op          *Op
	Findings    []Finding
	Suggestions []Suggestion
}

// filterFields lists the top-level field names a filter tests.
func filterFields(filter bson.D) []string {
	var out []string
	seen := map[string]bool{}
	var rec func(d bson.D)
	rec = func(d bson.D) {
		for _, e := range d {
			if e.Key == "$and" || e.Key == "$or" || e.Key == "$nor" {
				for _, sub := range asDocs(toArr(e.Value)) {
					rec(sub)
				}
				continue
			}
			if !strings.HasPrefix(e.Key, "$") && !seen[e.Key] {
				seen[e.Key] = true
				out = append(out, e.Key)
			}
		}
	}
	rec(filter)
	return out
}

func unbounded(intervals bson.A) bool {
	if len(intervals) != 1 {
		return false
	}
	s, _ := intervals[0].(string)
	return s == "[MinKey, MaxKey]" || s == "[MaxKey, MinKey]"
}

// Assess derives findings and index suggestions. op (the analyzed command)
// and indexes (the collection's listIndexes output) are optional.
func Assess(p *Plan, op *Op, indexSpecs []bson.D) Analysis {
	a := Analysis{Plan: p, Op: op}
	add := func(s Severity, format string, args ...interface{}) {
		a.Findings = append(a.Findings, Finding{Severity: s, Text: fmt.Sprintf(format, args...)})
	}
	if p.HasStats && !p.Success {
		switch msg := p.Error; {
		case strings.Contains(msg, "time limit"):
			add(Bad, "Execution hit the time limit before finishing; the statistics below are partial and undercount the real cost.")
		case msg == "":
			add(Bad, "Execution failed.")
		default:
			add(Bad, "Execution failed: %s", msg)
		}
	}
	needIndex := false
	collscan, fullScan, covered := false, false, false
	root := p.Winning
	if root != nil {
		root.walk(func(st, parent *Stage) {
			switch {
			case st.Name == "COLLSCAN":
				filter := getDoc(st.Spec, "filter")
				if len(filter) > 0 {
					collscan, needIndex = true, true
					read := ""
					if st.Stats {
						read = fmt.Sprintf(" (%s read, %s matched)", Count(st.Docs), Count(st.NReturned))
					}
					add(Bad, "Collection scan: every document is read and tested against the filter%s.", read)
				} else if op != nil && len(op.Sort) > 0 {
					needIndex = true
				} else {
					add(Info, "Collection scan without a filter reads documents in natural order — expected for an unfiltered query.")
				}
			case st.Name == "SORT":
				needIndex = true
				add(Warn, "In-memory sort on %s: no index provides this order, so matching documents are buffered and sorted before any are returned.",
					compact(get(st.Spec, "sortPattern"), 120))
				if getBool(st.Spec, "usedDisk") || getInt(st.Spec, "spills") > 0 {
					add(Bad, "The sort exceeded its memory limit and spilled to disk.")
				}
			case st.Name == "IXSCAN":
				bounds := getDoc(st.Spec, "indexBounds")
				if len(bounds) > 0 && unbounded(toArr(bounds[0].Value)) {
					fullScan, needIndex = true, true
					add(Warn, "Index %s is scanned end to end: there are no bounds on its leading field %q, so it likely only supplies sort order.",
						st.indexName(), bounds[0].Key)
				}
				if getBool(st.Spec, "isMultiKey") {
					add(Info, "Index %s is multikey (indexes array elements), so one document can have many keys.", st.indexName())
				}
			case st.Name == "FETCH":
				filter := getDoc(st.Spec, "filter")
				if len(filter) == 0 {
					break
				}
				var ix string
				for _, c := range st.Children {
					if names := c.IndexesUsed(); len(names) > 0 {
						ix = names[0]
					}
				}
				if ix != "" {
					needIndex = true
					add(Warn, "%s filtered after fetching documents, not inside index %s; documents that fail the test are read anyway.",
						fieldList(filterFields(filter)), ix)
				}
			case st.Name == "IDHACK" || strings.HasPrefix(st.Name, "EXPRESS"):
				add(Good, "Express path: single-document lookup through index %s.", st.indexName())
			case st.Name == "COUNT_SCAN":
				add(Good, "Count answered from index %s without reading documents.", st.indexName())
			case st.Name == "DISTINCT_SCAN":
				add(Good, "Distinct values read straight from index %s.", st.indexName())
			case st.Name == "PROJECTION_COVERED":
				covered = true
				if root.has("DISTINCT_SCAN") {
					break
				}
				add(Good, "Covered query: answered entirely from the index, no documents fetched.")
			case st.Name == "SINGLE_SHARD":
				add(Good, "Targeted a single shard.")
			case strings.HasPrefix(st.Name, "SHARD_MERGE") && len(st.Children) > 1:
				add(Info, "Scatter-gather across %d shards; including the shard key in the filter lets mongos target one shard.", len(st.Children))
			case st.Name == "EQ_LOOKUP":
				from := getStr(st.Spec, "foreignCollection")
				if _, c, ok := strings.Cut(from, "."); ok {
					from = c
				}
				field := getStr(st.Spec, "foreignField")
				switch getStr(st.Spec, "strategy") {
				case "IndexedLoopJoin":
					add(Good, "$lookup from %s uses an indexed join.", from)
				case "NestedLoopJoin":
					add(Warn, "$lookup from %s runs a nested-loop join, scanning %s once per input document. Index it: %s.createIndex({%s: 1})",
						from, from, CollRef(from), field)
				case "HashJoin":
					add(Info, "$lookup from %s uses a hash join because %s.%s has no index; fine while %s is small, index it if it grows.",
						from, from, field, from)
				}
			}
		})
	}
	if p.HasStats && p.RatioReliable() {
		ret := p.Returned()
		worst := max(p.Docs, p.Keys)
		if !collscan && !fullScan && worst >= 100 && worst >= 10*max(ret, 1) {
			needIndex = true
			add(Warn, "Examined %s index keys and %s documents to return %s (%s): far more is read than returned.",
				Count(p.Keys), Count(p.Docs), Count(ret), ratio(worst, ret))
		}
		if !covered && p.Docs == 0 && p.Keys > 0 && !root.has("FETCH", "COUNT_SCAN", "DISTINCT_SCAN", "IDHACK") && !hasExpress(root) {
			add(Good, "Covered query: answered entirely from the index, no documents fetched.")
		}
	}
	if len(p.Rejected) > 0 {
		if len(p.Rejected) == 1 {
			add(Info, "1 other plan was considered and rejected.")
		} else {
			add(Info, "%d other plans were considered and rejected.", len(p.Rejected))
		}
	}
	if op != nil && needIndex {
		a.suggest(op, indexSpecs, add)
	}
	if !hasSeverity(a.Findings, Warn, Bad) && p.HasStats && p.Success && root != nil && root.IndexesUsed() != nil {
		add(Good, "Efficient: %s keys and %s documents examined for %s returned.", Count(p.Keys), Count(p.Docs), Count(p.Returned()))
	}
	if len(a.Findings) == 0 {
		add(Good, "No problems found in this plan.")
	}
	sort.SliceStable(a.Findings, func(i, j int) bool { return a.Findings[i].Severity > a.Findings[j].Severity })
	return a
}

// Returned is the number of results a plan produced. COUNT, UPDATE and
// DELETE roots report 0, so their input (the matched documents) is used.
func (p *Plan) Returned() int64 {
	if r := p.Winning; r != nil && len(r.Children) == 1 && r.Children[0].Stats {
		switch r.Name {
		case "COUNT", "UPDATE", "DELETE", "BATCHED_DELETE":
			return r.Children[0].NReturned
		}
	}
	return p.NReturned
}

// RatioReliable reports whether examined:returned measures the query
// layer. SBE plans that push $group down report post-group counts.
func (p *Plan) RatioReliable() bool {
	return p.Winning != nil && (p.Engine != "sbe" || !p.Winning.has("GROUP"))
}

func hasExpress(st *Stage) bool {
	found := false
	st.walk(func(s, _ *Stage) { found = found || strings.HasPrefix(s.Name, "EXPRESS") })
	return found
}

func hasSeverity(fs []Finding, sev ...Severity) bool {
	for _, f := range fs {
		for _, s := range sev {
			if f.Severity == s {
				return true
			}
		}
	}
	return false
}

func fieldList(fields []string) string {
	switch len(fields) {
	case 0:
		return "Fields are"
	case 1:
		return "Field " + fields[0] + " is"
	}
	return "Fields " + strings.Join(fields, ", ") + " are"
}

func (a *Analysis) suggest(op *Op, specs []bson.D, add func(Severity, string, ...interface{})) {
	var indexes []Index
	for _, spec := range specs {
		indexes = append(indexes, ParseIndex(spec))
	}
	used := map[string]bool{}
	if a.Plan.Winning != nil {
		for _, n := range a.Plan.Winning.IndexesUsed() {
			used[n] = true
		}
	}
	for _, s := range Suggest(op.Filter, op.Sort) {
		satisfied, s := s.Against(indexes)
		switch {
		case satisfied == "":
			a.Suggestions = append(a.Suggestions, s)
		case !used[satisfied]:
			add(Info, "Existing index %s already fits this query shape, but the planner chose a different plan. The cached plan may be stale or %s less selective for these values; compare with .hint(%q).",
				satisfied, satisfied, satisfied)
		}
	}
}
