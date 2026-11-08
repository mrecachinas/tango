package advisor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// CollectionInfo is what Analyze needs to know about one collection.
type CollectionInfo struct {
	Name       string
	Indexes    []bson.D // listIndexes
	IndexStats []bson.D // $indexStats
	IndexSizes bson.D   // $collStats storageStats.indexSizes
}

// InsightsInput is everything fetched for one database. Errors are kept so
// the report can say why a section is empty.
type InsightsInput struct {
	DB           string
	Now          time.Time
	ProfileLevel int // -1 when unknown
	SlowMs       int64
	ProfileErr   error
	Profile      []bson.D // system.profile entries, newest first
	Collections  []CollectionInfo
	Top          bson.D // top.totals
	TopErr       error
	CurrentOps   []bson.D
	OpsErr       error
}

// ShapeStat aggregates profiler entries with the same query shape.
type ShapeStat struct {
	ID          string // Q1… for the top shapes
	NS          string
	Op          Op
	Shape       string
	Count       int64 // executions; getMores add cost but not count
	TotalMs     float64
	MaxMs       float64
	Docs, Keys  int64
	Returned    int64
	PlanSummary string
	Last        time.Time
	CollScan    bool
	// Missing is a suggested index when no existing index fits; Fits names an
	// existing index that serves the shape although the profiled plan didn't
	// use it well.
	Missing *Suggestion
	Fits    string
}

// AvgMs is the mean time per execution.
func (s ShapeStat) AvgMs() float64 {
	if s.Count == 0 {
		return s.TotalMs
	}
	return s.TotalMs / float64(s.Count)
}

// Inefficient reports a shape that scans far more than it returns.
// Collection scans of tiny collections are cheap and not worth an index.
func (s ShapeStat) Inefficient() bool {
	if s.CollScan {
		return s.Docs >= 100*max(s.Count, 1)
	}
	examined := max(s.Docs, s.Keys)
	if !s.ratioMeaningful() || examined < 1000 {
		return false
	}
	return examined > 100*max(s.Returned, 1)
}

// Count, distinct and grouping aggregations legitimately examine more than
// they return, so their ratio says nothing about index fit.
func (s ShapeStat) ratioMeaningful() bool {
	switch s.Op.Kind {
	case "find", "update", "delete", "findAndModify":
		return true
	case "aggregate":
		for _, raw := range s.Op.Pipeline {
			if st := toD(raw); len(st) > 0 {
				switch st[0].Key {
				case "$group", "$count", "$bucket", "$bucketAuto", "$facet", "$sortByCount":
					return false
				}
			}
		}
		return true
	}
	return false
}

// HotColl is one collection's load from top and the profiler.
type HotColl struct {
	NS              string
	Ops             int64
	TimeMs          float64
	Reads, Writes   int64
	ReadMs, WriteMs float64
	ProfiledMs      float64
	ProfiledOps     int64
}

// IndexUsage is one index with its usage counters.
type IndexUsage struct {
	NS, Coll, Name string
	Keys           string
	Ops            int64
	HasStats       bool
	Since          time.Time
	Size           float64
	Unique, TTL    bool
	Hidden         bool
	// RedundantWith names an index whose keys this one prefixes.
	RedundantWith string
}

// Unused reports an index with no recorded use that is safe to consider
// dropping: _id, unique and TTL indexes do work beyond serving reads.
func (u IndexUsage) Unused() bool {
	return u.HasStats && u.Ops == 0 && u.Name != "_id_" && !u.Unique && !u.TTL && !u.Hidden
}

// RunningOp is an operation that has been running for at least a second.
type RunningOp struct {
	OpID     interface{}
	NS, Kind string
	Secs     float64
	Plan     string
	Command  bson.D
	Waiting  bool // waiting for a lock
}

// Recommendation is one actionable item, most severe first.
type Recommendation struct {
	Severity Severity
	Title    string
	Detail   string
	Command  string
	Refs     []string // query shapes it concerns
}

// Insights is the analysis of one database.
type Insights struct {
	DB              string
	ProfileLevel    int
	SlowMs          int64
	ProfileEntries  int
	Oldest, Newest  time.Time
	Shapes          []ShapeStat // ranked by total time; the first ones have IDs
	Recommendations []Recommendation
	Hot             []HotColl
	Indexes         []IndexUsage
	Running         []RunningOp
}

// MaxShapes is how many query shapes get IDs and recommendations.
const MaxShapes = 20

// Analyze turns profiler, index and load data into ranked findings.
func Analyze(in InsightsInput) Insights {
	out := Insights{DB: in.DB, ProfileLevel: in.ProfileLevel, SlowMs: in.SlowMs, ProfileEntries: len(in.Profile)}
	indexes := map[string][]Index{}
	for _, c := range in.Collections {
		for _, spec := range c.Indexes {
			indexes[c.Name] = append(indexes[c.Name], ParseIndex(spec))
		}
	}

	out.Shapes = aggregateShapes(in, &out)
	var recs []Recommendation
	recs = append(recs, profilerRecs(in)...)
	recs = append(recs, shapeRecs(out.Shapes, indexes)...)
	out.Running = runningOps(in)
	for _, op := range out.Running {
		if op.Secs < 10 {
			continue
		}
		rec := Recommendation{
			Severity: Warn,
			Title:    fmt.Sprintf("%s on %s has run for %s", op.Kind, op.NS, time.Duration(op.Secs)*time.Second),
			Command:  fmt.Sprintf(":killop %v", op.OpID),
		}
		switch {
		case strings.Contains(op.Plan, "COLLSCAN"):
			rec.Severity = Bad
			rec.Detail = "It is scanning the whole collection. Kill it if it isn't expected, then index its filter."
		case op.Waiting:
			rec.Detail = "It is waiting for a lock; look for the operation holding it."
		default:
			rec.Detail = "Plan: " + orDash(op.Plan)
		}
		recs = append(recs, rec)
	}
	out.Indexes = indexUsage(in)
	recs = append(recs, indexRecs(out.Indexes)...)
	out.Hot = hotCollections(in, out.Shapes)

	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Severity > recs[j].Severity })
	out.Recommendations = recs
	return out
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func aggregateShapes(in InsightsInput, out *Insights) []ShapeStat {
	byKey := map[string]*ShapeStat{}
	var order []string
	for _, e := range in.Profile {
		ns := getStr(e, "ns")
		if ns == "" || strings.Contains(ns, ".system.") {
			continue
		}
		if t, ok := timeOf(get(e, "ts")); ok {
			if out.Newest.IsZero() || t.After(out.Newest) {
				out.Newest = t
			}
			if out.Oldest.IsZero() || t.Before(out.Oldest) {
				out.Oldest = t
			}
		}
		op, ok := OpFromProfile(e)
		if !ok {
			continue
		}
		shape := op.ShapeText()
		key := ns + "\x00" + shape
		st := byKey[key]
		if st == nil {
			st = &ShapeStat{NS: ns, Op: op, Shape: shape}
			byKey[key] = st
			order = append(order, key)
		}
		ms, _ := getNum(e, "millis")
		st.TotalMs += ms
		st.MaxMs = max(st.MaxMs, ms)
		st.Docs += getInt(e, "docsExamined")
		st.Keys += getInt(e, "keysExamined")
		switch getStr(e, "op") {
		case "update":
			st.Returned += getInt(e, "nMatched")
		case "remove":
			st.Returned += getInt(e, "ndeleted")
		default:
			st.Returned += getInt(e, "nreturned")
		}
		if getStr(e, "op") != "getmore" {
			st.Count++
		}
		plan := getStr(e, "planSummary")
		if strings.Contains(plan, "COLLSCAN") {
			st.CollScan = true
		}
		if t, ok := timeOf(get(e, "ts")); ok && t.After(st.Last) {
			st.Last = t
			if plan != "" {
				st.PlanSummary = plan
			}
		} else if st.PlanSummary == "" {
			st.PlanSummary = plan
		}
	}
	shapes := make([]ShapeStat, 0, len(order))
	for _, k := range order {
		shapes = append(shapes, *byKey[k])
	}
	sort.SliceStable(shapes, func(i, j int) bool { return shapes[i].TotalMs > shapes[j].TotalMs })
	for i := range shapes {
		if i < MaxShapes {
			shapes[i].ID = fmt.Sprintf("Q%d", i+1)
		}
	}
	return shapes
}

func profilerRecs(in InsightsInput) []Recommendation {
	switch {
	case in.ProfileErr != nil:
		return []Recommendation{{
			Severity: Info,
			Title:    "Can't read the profiler",
			Detail:   in.ProfileErr.Error() + ". Query shapes need system.profile; mongos and some managed tiers don't expose it.",
		}}
	case in.ProfileLevel == 0:
		return []Recommendation{{
			Severity: Warn,
			Title:    "The profiler is off in " + in.DB,
			Detail:   "Without it there is no query history to analyze. Level 1 records only operations slower than slowms.",
			Command:  "db.setProfilingLevel(1, {slowms: 50})",
		}}
	case in.ProfileLevel == 2:
		return []Recommendation{{
			Severity: Info,
			Title:    "The profiler records every operation (level 2)",
			Detail:   "That adds a write per operation. Drop to level 1 once you have enough samples.",
			Command:  fmt.Sprintf("db.setProfilingLevel(1, {slowms: %d})", max(in.SlowMs, 50)),
		}}
	}
	return nil
}

func shapeRecs(shapes []ShapeStat, indexes map[string][]Index) []Recommendation {
	var recs []Recommendation
	missing := map[string]int{} // coll + keys -> index into recs
	for i := range shapes {
		st := &shapes[i]
		if st.ID == "" || !st.Inefficient() {
			continue
		}
		perRun := ""
		if st.Count > 0 {
			perRun = fmt.Sprintf(", %s docs examined per run", Count(st.Docs/st.Count))
		}
		why := fmt.Sprintf("%s ran %s× for %s total%s", st.ID, Count(st.Count), Millis(st.TotalMs), perRun)
		suggestions := Suggest(st.Op.Filter, st.Op.Sort)
		var fits []string
		emitted := false
		for _, s := range suggestions {
			if len(s.Keys()) == 0 {
				continue
			}
			satisfied, s := s.Against(indexes[st.Op.Coll])
			if satisfied != "" {
				fits = append(fits, satisfied)
				continue
			}
			if st.Missing == nil {
				sc := s
				st.Missing = &sc
			}
			key := st.Op.Coll + " " + KeysString(s.Keys())
			if j, ok := missing[key]; ok {
				recs[j].Refs = append(recs[j].Refs, st.ID)
				recs[j].Title = fmt.Sprintf("Add an index on %s for %s", st.Op.Coll, strings.Join(recs[j].Refs, ", "))
				recs[j].Detail += "\n" + why + "."
				emitted = true
				continue
			}
			sev := Warn
			if st.CollScan {
				sev = Bad
			}
			detail := why + ". Key order: " + s.Rationale() + "."
			if s.Extends != "" {
				detail += fmt.Sprintf(" It extends %s, which can be dropped once this is built.", s.Extends)
			}
			for _, n := range s.Notes {
				detail += " " + n + "."
			}
			missing[key] = len(recs)
			recs = append(recs, Recommendation{
				Severity: sev,
				Title:    fmt.Sprintf("Add an index on %s for %s", st.Op.Coll, st.ID),
				Detail:   detail,
				Command:  IndexCommand(st.Op.Coll, s),
				Refs:     []string{st.ID},
			})
			emitted = true
		}
		if !emitted {
			recs = append(recs, unindexableRec(st, fits, why)...)
		}
	}
	// Slow shapes nothing above explains: point at explain.
	flagged := map[string]bool{}
	for _, r := range recs {
		for _, id := range r.Refs {
			flagged[id] = true
		}
	}
	for _, st := range shapes {
		if st.ID == "" || flagged[st.ID] || st.AvgMs() < SlowShapeMs {
			continue
		}
		rec := Recommendation{
			Severity: Warn,
			Title:    fmt.Sprintf("%s averages %s per run", st.ID, Millis(st.AvgMs())),
			Detail:   fmt.Sprintf("%s ran %s× using %s.", st.Shape, Count(st.Count), orDash(st.PlanSummary)),
			Command:  ":explain " + st.ID,
			Refs:     []string{st.ID},
		}
		if js := usesJavaScript(st.Op); js != "" {
			rec.Detail += " It uses " + js + ", which runs JavaScript for every document and can't use an index; rewrite it with query or aggregation operators."
		} else {
			rec.Detail += " Explain it to see where the time goes; check for lock waits or a large in-memory sort."
		}
		recs = append(recs, rec)
	}
	return recs
}

// SlowShapeMs is the mean run time above which a shape is worth explaining
// even when its plan looks reasonable.
const SlowShapeMs = 1000

func usesJavaScript(op Op) string {
	text := compact(op.Filter, 0) + compact(op.Pipeline, 0)
	for _, js := range []string{"$where", "$function", "$accumulator"} {
		if strings.Contains(text, `"`+js+`"`) {
			return js
		}
	}
	return ""
}

func unindexableRec(st *ShapeStat, fits []string, why string) []Recommendation {
	switch {
	case len(fits) > 0:
		st.Fits = fits[0]
		return []Recommendation{{
			Severity: Info,
			Title:    fmt.Sprintf("%s ran with %s although %s fits", st.ID, orDash(st.PlanSummary), fits[0]),
			Detail:   why + ". The index may be newer than these samples, or the planner chose differently; explain shows today's plan.",
			Command:  ":explain " + st.ID,
			Refs:     []string{st.ID},
		}}
	case st.CollScan && len(st.Op.Filter) == 0:
		return []Recommendation{{
			Severity: Info,
			Title:    fmt.Sprintf("%s reads all of %s with no filter", st.ID, st.Op.Coll),
			Detail:   why + ". No index helps an unfiltered read; add a filter or a limit if the whole collection isn't needed.",
			Refs:     []string{st.ID},
		}}
	}
	return nil
}

func runningOps(in InsightsInput) []RunningOp {
	var ops []RunningOp
	for _, op := range in.CurrentOps {
		ns := getStr(op, "ns")
		secs, _ := getNum(op, "secs_running")
		kind := getStr(op, "op")
		if secs < 1 || kind == "none" || !strings.HasPrefix(ns, in.DB+".") {
			continue
		}
		cmd := getDoc(op, "command")
		if len(cmd) > 0 && (cmd[0].Key == "getMore" && getBool(op, "cursor.tailable") ||
			cmd[0].Key == "aggregate" && hasChangeStream(getArr(cmd, "pipeline"))) {
			continue
		}
		ops = append(ops, RunningOp{
			OpID: get(op, "opid"), NS: ns, Kind: kind, Secs: secs,
			Plan: getStr(op, "planSummary"), Command: stripSession(cmd), Waiting: getBool(op, "waitingForLock"),
		})
	}
	sort.SliceStable(ops, func(i, j int) bool { return ops[i].Secs > ops[j].Secs })
	return ops
}

func hasChangeStream(p bson.A) bool {
	if len(p) == 0 {
		return false
	}
	st := toD(p[0])
	return len(st) > 0 && st[0].Key == "$changeStream"
}

func indexUsage(in InsightsInput) []IndexUsage {
	var out []IndexUsage
	for _, c := range in.Collections {
		stats := map[string]bson.D{}
		for _, st := range c.IndexStats {
			stats[getStr(st, "name")] = st
		}
		parsed := make([]Index, len(c.Indexes))
		for i, spec := range c.Indexes {
			parsed[i] = ParseIndex(spec)
		}
		for i, ix := range parsed {
			u := IndexUsage{
				NS: in.DB + "." + c.Name, Coll: c.Name, Name: ix.Name, Keys: KeysString(ix.Keys),
				Unique: ix.Unique, TTL: ix.TTL, Hidden: ix.Hidden,
			}
			if sz, ok := getNum(c.IndexSizes, ix.Name); ok {
				u.Size = sz
			}
			if st, ok := stats[ix.Name]; ok {
				u.HasStats = true
				u.Ops = getInt(st, "accesses.ops")
				u.Since, _ = timeOf(get(st, "accesses.since"))
			}
			if ix.Name != "_id_" && !ix.Unique && !ix.TTL && !ix.Partial && !ix.Sparse && !ix.Collation {
				for j, other := range parsed {
					if i == j || other.Partial || other.Sparse || other.Hidden || other.Collation || !ix.IsPrefixOf(other) {
						continue
					}
					// Identical keys: flag only one of the pair.
					if len(ix.Keys) == len(other.Keys) && ix.Name < other.Name && !other.Unique && !other.TTL {
						continue
					}
					u.RedundantWith = other.Name
					break
				}
			}
			out = append(out, u)
		}
	}
	return out
}

func indexRecs(usage []IndexUsage) []Recommendation {
	var recs []Recommendation
	for _, u := range usage {
		ref := CollRef(u.Coll)
		size := ""
		if u.Size > 0 {
			size = " (" + Bytes(u.Size) + ")"
		}
		switch {
		case u.RedundantWith != "":
			recs = append(recs, Recommendation{
				Severity: Info,
				Title:    fmt.Sprintf("%s.%s%s is redundant with %s", u.Coll, u.Name, size, u.RedundantWith),
				Detail: fmt.Sprintf("%s leads with the same keys, so it serves every query %s does. "+
					"Hide %s first and watch for regressions before dropping it.", u.RedundantWith, u.Name, u.Name),
				Command: fmt.Sprintf("%s.hideIndex(%q)", ref, u.Name),
			})
		case u.Unused():
			since := ""
			if !u.Since.IsZero() {
				since = " since " + u.Since.Local().Format("2006-01-02 15:04")
			}
			recs = append(recs, Recommendation{
				Severity: Info,
				Title:    fmt.Sprintf("%s.%s%s has not been used%s", u.Coll, u.Name, size, since),
				Detail: "Every write still maintains it. Counters reset on restart and are per node, so check other members " +
					"and periodic jobs; hide it first, then drop it if nothing slows down.",
				Command: fmt.Sprintf("%s.hideIndex(%q)", ref, u.Name),
			})
		}
	}
	return recs
}

func hotCollections(in InsightsInput, shapes []ShapeStat) []HotColl {
	byNS := map[string]*HotColl{}
	entry := func(ns string) *HotColl {
		h := byNS[ns]
		if h == nil {
			h = &HotColl{NS: ns}
			byNS[ns] = h
		}
		return h
	}
	prefix := in.DB + "."
	for _, e := range in.Top {
		if !strings.HasPrefix(e.Key, prefix) || strings.Contains(e.Key, ".system.") {
			continue
		}
		d := toD(e.Value)
		h := entry(e.Key)
		h.Ops = getInt(d, "total.count")
		t, _ := getNum(d, "total.time")
		h.TimeMs = t / 1000
		for _, k := range []string{"queries", "getmore", "commands"} {
			h.Reads += getInt(d, k+".count")
			t, _ := getNum(d, k+".time")
			h.ReadMs += t / 1000
		}
		for _, k := range []string{"insert", "update", "remove"} {
			h.Writes += getInt(d, k+".count")
			t, _ := getNum(d, k+".time")
			h.WriteMs += t / 1000
		}
	}
	for _, st := range shapes {
		h := entry(st.NS)
		h.ProfiledMs += st.TotalMs
		h.ProfiledOps += st.Count
	}
	out := make([]HotColl, 0, len(byNS))
	for _, h := range byNS {
		if h.Ops > 0 || h.ProfiledOps > 0 {
			out = append(out, *h)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TimeMs != out[j].TimeMs {
			return out[i].TimeMs > out[j].TimeMs
		}
		if out[i].ProfiledMs != out[j].ProfiledMs {
			return out[i].ProfiledMs > out[j].ProfiledMs
		}
		return out[i].NS < out[j].NS
	})
	return out
}

func stripSession(cmd bson.D) bson.D {
	out := make(bson.D, 0, len(cmd))
	for _, e := range cmd {
		if !sessionKeys[e.Key] {
			out = append(out, e)
		}
	}
	return out
}
