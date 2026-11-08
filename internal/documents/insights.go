package documents

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tango/internal/advisor"
	"tango/internal/mjson"
	"tango/internal/state"
)

const (
	insightsProfileLimit = 2000
	insightsMaxColls     = 100
)

// ShowInsights opens the insights view for db and loads it.
func ShowInsights(db string) {
	s := state.S
	if s.InsightsDB != db {
		s.InsightsSections, s.InsightsQueries = nil, nil
	}
	s.InsightsDB = db
	if len(s.InsightsSections) == 0 {
		s.InsightsText = "Analyzing " + db + "…"
		s.InsightsRevision++
	}
	s.View, s.Focus = "insights", "content"
	RefreshInsights()
}

// RefreshInsights re-reads the profiler, index statistics and current load.
func RefreshInsights() {
	s := state.S
	s.InsightsEpoch++
	epoch, db, client := s.InsightsEpoch, s.InsightsDB, s.Client
	timeout := s.QueryTimeout
	if timeout <= 0 {
		timeout = defaultExplainTimeout
	}
	state.AsyncTimeout("Analyzing "+db, 2*timeout, func(ctx context.Context) func() {
		in := fetchInsights(ctx, client, db)
		return func() {
			if s.InsightsEpoch != epoch {
				return
			}
			renderInsights(advisor.Analyze(in), in)
		}
	})
}

func fetchInsights(ctx context.Context, client *mongo.Client, db string) advisor.InsightsInput {
	in := advisor.InsightsInput{DB: db, Now: time.Now(), ProfileLevel: -1}
	database, admin := client.Database(db), client.Database("admin")
	var level bson.D
	if in.ProfileErr = database.RunCommand(ctx, bson.D{{Key: "profile", Value: -1}}).Decode(&level); in.ProfileErr == nil {
		was, _ := mjson.Get(level, "was")
		slow, _ := mjson.Get(level, "slowms")
		w, _ := state.ToFloat64(was)
		ms, _ := state.ToFloat64(slow)
		in.ProfileLevel, in.SlowMs = int(w), int64(ms)
		in.ProfileErr = readAll(ctx, &in.Profile, func(c context.Context) (*mongo.Cursor, error) {
			ops := bson.A{"query", "getmore", "update", "remove", "command"}
			return database.Collection("system.profile").Find(c, bson.D{{Key: "op", Value: bson.D{{Key: "$in", Value: ops}}}},
				options.Find().SetSort(bson.D{{Key: "$natural", Value: -1}}).SetLimit(insightsProfileLimit))
		})
	}

	names, _ := database.ListCollectionNames(ctx, bson.D{{Key: "type", Value: "collection"}},
		options.ListCollections().SetAuthorizedCollections(true))
	names = slicesFilter(names, func(n string) bool { return !strings.HasPrefix(n, "system.") })
	sort.Strings(names)
	if len(names) > insightsMaxColls {
		names = names[:insightsMaxColls]
	}
	in.Collections = make([]advisor.CollectionInfo, len(names))
	forEachLimit(len(names), listConcurrency, func(i int) {
		c, cancel := context.WithTimeout(ctx, perItemTimeout)
		defer cancel()
		coll := database.Collection(names[i])
		info := advisor.CollectionInfo{Name: names[i]}
		_ = readAll(c, &info.Indexes, func(c context.Context) (*mongo.Cursor, error) { return coll.Indexes().List(c) })
		_ = readAll(c, &info.IndexStats, func(c context.Context) (*mongo.Cursor, error) {
			return coll.Aggregate(c, bson.A{bson.D{{Key: "$indexStats", Value: bson.D{}}}})
		})
		var cs []bson.D
		_ = readAll(c, &cs, func(c context.Context) (*mongo.Cursor, error) {
			return coll.Aggregate(c, bson.A{bson.D{{Key: "$collStats", Value: bson.D{{Key: "storageStats", Value: bson.D{}}}}}})
		})
		if len(cs) > 0 {
			if ss, ok := mjson.Get(cs[0], "storageStats"); ok {
				if d, ok := ss.(bson.D); ok {
					sizes, _ := mjson.Get(d, "indexSizes")
					info.IndexSizes, _ = sizes.(bson.D)
				}
			}
		}
		in.Collections[i] = info
	})

	var top bson.D
	if in.TopErr = admin.RunCommand(ctx, bson.D{{Key: "top", Value: 1}}).Decode(&top); in.TopErr == nil {
		totals, _ := mjson.Get(top, "totals")
		in.Top, _ = totals.(bson.D)
	}
	var cur bson.D
	in.OpsErr = admin.RunCommand(ctx, bson.D{{Key: "currentOp", Value: 1}, {Key: "active", Value: true},
		{Key: "secs_running", Value: bson.D{{Key: "$gte", Value: 1}}}}).Decode(&cur)
	if inprog, ok := mjson.Get(cur, "inprog"); ok {
		if arr, ok := inprog.(bson.A); ok {
			for _, op := range arr {
				if d, ok := op.(bson.D); ok {
					in.CurrentOps = append(in.CurrentOps, d)
				}
			}
		}
	}
	return in
}

func readAll(ctx context.Context, out *[]bson.D, open func(context.Context) (*mongo.Cursor, error)) error {
	cur, err := open(ctx)
	if err != nil {
		return err
	}
	return cur.All(ctx, out)
}

func slicesFilter(in []string, keep func(string) bool) []string {
	out := in[:0]
	for _, v := range in {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

var severityLine = map[advisor.Severity]struct{ mark, tone string }{
	advisor.Bad:  {"✗", "bad"},
	advisor.Warn: {"!", "warn"},
	advisor.Info: {"•", "info"},
	advisor.Good: {"✓", "good"},
}

func renderInsights(ins advisor.Insights, in advisor.InsightsInput) {
	s := state.S
	s.InsightsUpdated = fmt.Sprintf("%s · updated %s · r refresh · :explain Q1 or E to explain a query shape",
		ins.DB, in.Now.Format("15:04:05"))
	s.InsightsQueries = map[string]state.InsightQuery{}
	for _, st := range ins.Shapes {
		if st.ID != "" {
			db, coll, _ := strings.Cut(st.NS, ".")
			s.InsightsQueries[st.ID] = state.InsightQuery{NS: state.Namespace{DB: db, Coll: coll}, Command: st.Op.Command, Shape: st.Shape}
		}
	}

	recs := state.StatsSection{Title: "Recommendations", Note: fmt.Sprintf("(%d)", len(ins.Recommendations))}
	for i, r := range ins.Recommendations {
		if i > 0 {
			recs.Lines = append(recs.Lines, state.StatsLine{})
		}
		mark := severityLine[r.Severity]
		recs.Lines = append(recs.Lines, state.StatsLine{Text: mark.mark + " " + r.Title, Tone: mark.tone})
		for _, d := range strings.Split(r.Detail, "\n") {
			recs.Lines = append(recs.Lines, state.StatsLine{Text: d, Tone: "muted", Indent: 2})
		}
		if r.Command != "" {
			recs.Lines = append(recs.Lines, state.StatsLine{Text: "→ " + r.Command, Tone: "accent", Indent: 2})
		}
	}
	if len(recs.Lines) == 0 {
		recs.Empty = fmt.Sprintf("Nothing to recommend from %d profiled operations and %d collections.", ins.ProfileEntries, len(in.Collections))
	}

	sections := []state.StatsSection{recs, shapesSection(ins, in), hotSection(ins, in), indexSection(ins), runningSection(ins, in)}
	s.InsightsSections = sections
	s.InsightsText = ""
	s.InsightsRevision++
}

func shapesSection(ins advisor.Insights, in advisor.InsightsInput) state.StatsSection {
	sec := state.StatsSection{
		Title: "Slow query shapes",
		Columns: []state.StatsColumn{
			{Name: "ID", Width: 4}, {Name: "COLLECTION", Width: 16}, {Name: "RUNS", Width: 6, Right: true},
			{Name: "TOTAL", Width: 8, Right: true}, {Name: "AVG", Width: 8, Right: true}, {Name: "MAX", Width: 8, Right: true},
			{Name: "EXAMINED", Width: 9, Right: true}, {Name: "RETURNED", Width: 9, Right: true},
			{Name: "PLAN", Width: 24}, {Name: "SHAPE"},
		},
	}
	switch {
	case in.ProfileErr != nil:
		sec.Empty = "Can't read system.profile: " + in.ProfileErr.Error()
		return sec
	case ins.ProfileEntries == 0 && ins.ProfileLevel == 0:
		sec.Empty = "The profiler is off. Enable it with db.setProfilingLevel(1, {slowms: 50}), let the app run, then press r."
		return sec
	case len(ins.Shapes) == 0:
		sec.Empty = fmt.Sprintf("No queries slower than %dms recorded yet.", ins.SlowMs)
		return sec
	}
	n := 0
	for _, st := range ins.Shapes {
		if st.ID == "" {
			break
		}
		n++
		_, coll, _ := strings.Cut(st.NS, ".")
		examined := max(st.Docs, st.Keys)
		sec.Rows = append(sec.Rows, []string{
			st.ID, coll, advisor.Count(st.Count), advisor.Millis(st.TotalMs), advisor.Millis(st.AvgMs()), advisor.Millis(st.MaxMs),
			advisor.Count(examined), advisor.Count(st.Returned), st.PlanSummary, st.Shape,
		})
	}
	sec.Note = fmt.Sprintf("(%d of %d) · %d profiler entries", n, len(ins.Shapes), ins.ProfileEntries)
	if !ins.Oldest.IsZero() {
		sec.Note += " since " + ins.Oldest.Local().Format("Jan 2 15:04")
	}
	sec.Note += fmt.Sprintf(" · level %d, slowms %d", ins.ProfileLevel, ins.SlowMs)
	return sec
}

func hotSection(ins advisor.Insights, in advisor.InsightsInput) state.StatsSection {
	sec := state.StatsSection{
		Title: "Hot collections", Note: "· top since server start, plus profiled time",
		Columns: []state.StatsColumn{
			{Name: "COLLECTION", Width: 24}, {Name: "OPS", Width: 10, Right: true}, {Name: "TIME", Width: 9, Right: true},
			{Name: "AVG", Width: 8, Right: true}, {Name: "READS", Width: 10, Right: true}, {Name: "WRITES", Width: 10, Right: true},
			{Name: "PROFILED", Width: 9, Right: true},
		},
	}
	if in.TopErr != nil {
		sec.Note = "· profiled time only (top: " + in.TopErr.Error() + ")"
	}
	if len(ins.Hot) == 0 {
		sec.Empty = "No collection activity recorded."
		return sec
	}
	for i, h := range ins.Hot {
		if i == 10 {
			break
		}
		_, coll, _ := strings.Cut(h.NS, ".")
		avg := "—"
		if h.Ops > 0 {
			avg = advisor.Millis(h.TimeMs / float64(h.Ops))
		}
		sec.Rows = append(sec.Rows, []string{coll, advisor.Count(h.Ops), advisor.Millis(h.TimeMs), avg,
			advisor.Count(h.Reads), advisor.Count(h.Writes), advisor.Millis(h.ProfiledMs)})
	}
	sec.Note = fmt.Sprintf("(%d) ", len(sec.Rows)) + sec.Note
	return sec
}

func indexSection(ins advisor.Insights) state.StatsSection {
	sec := state.StatsSection{
		Title: "Index usage", Note: fmt.Sprintf("(%d) · $indexStats on this node, reset on restart", len(ins.Indexes)),
		Columns: []state.StatsColumn{
			{Name: "COLLECTION", Width: 16}, {Name: "INDEX", Width: 22}, {Name: "KEYS", Width: 26},
			{Name: "OPS", Width: 10, Right: true}, {Name: "SIZE", Width: 9, Right: true}, {Name: "NOTE"},
		},
	}
	if len(ins.Indexes) == 0 {
		sec.Empty = "No indexes found."
		return sec
	}
	for _, u := range ins.Indexes {
		ops, size := "—", "—"
		if u.HasStats {
			ops = advisor.Count(u.Ops)
		}
		if u.Size > 0 {
			size = advisor.Bytes(u.Size)
		}
		var notes []string
		switch {
		case u.Unused():
			notes = append(notes, "unused")
		case u.Hidden:
			notes = append(notes, "hidden")
		}
		if u.RedundantWith != "" {
			notes = append(notes, "redundant with "+u.RedundantWith)
		}
		if u.Unique && u.Name != "_id_" {
			notes = append(notes, "unique")
		}
		if u.TTL {
			notes = append(notes, "ttl")
		}
		sec.Rows = append(sec.Rows, []string{u.Coll, u.Name, u.Keys, ops, size, strings.Join(notes, ", ")})
	}
	return sec
}

func runningSection(ins advisor.Insights, in advisor.InsightsInput) state.StatsSection {
	sec := state.StatsSection{
		Title: "Running now", Note: fmt.Sprintf("(%d) · operations in %s running 1s or more", len(ins.Running), ins.DB),
		Columns: []state.StatsColumn{
			{Name: "OPID", Width: 8, Right: true}, {Name: "TYPE", Width: 9}, {Name: "COLLECTION", Width: 16},
			{Name: "SECS", Width: 6, Right: true}, {Name: "PLAN", Width: 24}, {Name: "COMMAND"},
		},
	}
	switch {
	case in.OpsErr != nil:
		sec.Empty = fmt.Sprintf("Error: %s (may require the inprog privilege)", in.OpsErr)
	case len(ins.Running) == 0:
		sec.Empty = "Nothing has been running for a second or more."
	}
	for _, op := range ins.Running {
		_, coll, _ := strings.Cut(op.NS, ".")
		plan := op.Plan
		if op.Waiting {
			plan = "waiting for lock"
		}
		if state.S.StatsOps == nil {
			state.S.StatsOps = map[string]interface{}{}
		}
		state.S.StatsOps[fmt.Sprint(op.OpID)] = op.OpID
		sec.Rows = append(sec.Rows, []string{fmt.Sprint(op.OpID), op.Kind, coll, fmt.Sprintf("%.0f", op.Secs), plan, compactCmd(op.Command, 2000)})
	}
	return sec
}
