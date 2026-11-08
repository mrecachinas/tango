package advisor

import (
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
)

type report struct{ toks []mjson.Token }

func (r *report) emit(k mjson.Kind, format string, args ...interface{}) {
	if len(args) > 0 {
		format = fmt.Sprintf(format, args...)
	}
	r.toks = append(r.toks, mjson.Token{Text: format, Kind: k})
}

func (r *report) nl() { r.emit(mjson.Text, "\n") }

func (r *report) heading(title string) {
	if len(r.toks) > 0 {
		r.nl()
	}
	r.emit(mjson.Heading, title)
	r.nl()
}

func (r *report) row(label, value string, kind mjson.Kind) {
	r.emit(mjson.Muted, "  %-10s ", label)
	r.emit(kind, value)
	r.nl()
}

var severityMark = map[Severity]struct {
	mark string
	kind mjson.Kind
}{
	Bad:  {"✗", mjson.Bad},
	Warn: {"!", mjson.Warn},
	Info: {"•", mjson.Accent},
	Good: {"✓", mjson.Good},
}

// IndexCommand renders a createIndex call for a suggestion.
func IndexCommand(coll string, s Suggestion) string {
	return CollRef(coll) + ".createIndex(" + KeysString(s.Keys()) + ")"
}

// Report renders an analysis as styled text for the document viewer. query
// is the shell form of the explained operation; raw is the explain output.
func (a Analysis) Report(verbosity, query string, raw bson.D) []mjson.Token {
	r := &report{}
	p := a.Plan
	head := "Explain · " + verbosity
	if p.Namespace != "" {
		head += " · " + p.Namespace
	}
	if p.Version != "" {
		head += " · MongoDB " + p.Version
	}
	if p.Engine == "sbe" {
		head += " (SBE)"
	}
	r.emit(mjson.Heading, head)
	r.nl()

	if query != "" {
		r.heading("Query")
		r.emit(mjson.Text, "  "+query)
		r.nl()
	}

	r.heading("Findings")
	for _, f := range a.Findings {
		m := severityMark[f.Severity]
		r.emit(m.kind, "  "+m.mark+" ")
		r.emit(mjson.Text, f.Text)
		r.nl()
	}

	r.heading("Summary")
	if p.Winning != nil {
		r.row("Plan", p.Winning.Flow(), mjson.Text)
		used := p.Winning.IndexesUsed()
		if len(used) == 0 {
			r.row("Index", "none", mjson.Bad)
		} else {
			r.row("Index", strings.Join(used, ", "), mjson.Good)
		}
	}
	if p.HasStats {
		ret := p.Returned()
		r.row("Returned", Count(ret), mjson.Text)
		exam := fmt.Sprintf("%s keys · %s docs", Count(p.Keys), Count(p.Docs))
		kind := mjson.Text
		if worst := max(p.Docs, p.Keys); worst > 0 && p.RatioReliable() {
			exam += " (" + ratio(worst, ret) + " examined:returned)"
			if worst >= 100 && worst >= 10*max(ret, 1) {
				kind = mjson.Warn
			}
		}
		r.row("Examined", exam, kind)
		r.row("Time", Millis(float64(p.TimeMs)), mjson.Text)
	} else {
		r.row("Stats", "not executed (queryPlanner verbosity)", mjson.Muted)
	}
	r.row("Plans", fmt.Sprintf("%d considered", 1+len(p.Rejected)), mjson.Text)

	if len(a.Suggestions) > 0 && a.Op != nil {
		r.heading("Suggested index")
		for _, s := range a.Suggestions {
			r.emit(mjson.Accent, "  "+IndexCommand(a.Op.Coll, s))
			r.nl()
			if why := s.Rationale(); why != "" {
				r.emit(mjson.Muted, "    "+why+" (ESR: equality, sort, range)")
				r.nl()
			}
			if s.Extends != "" {
				r.emit(mjson.Muted, "    extends %s, which becomes redundant once this is built", s.Extends)
				r.nl()
			}
			for _, n := range s.Notes {
				r.emit(mjson.Muted, "    note: "+n)
				r.nl()
			}
		}
		r.emit(mjson.Muted, "  Re-run :explain after building it to confirm the planner picks it.")
		r.nl()
	}

	if p.Winning != nil {
		r.heading("Winning plan")
		r.tree(p.Winning, "  ", "  ")
	}
	if len(p.Rejected) > 0 {
		r.heading(fmt.Sprintf("Rejected plans (%d)", len(p.Rejected)))
		for i, st := range p.Rejected {
			r.emit(mjson.Muted, "  %d. ", i+1)
			flow := st.Flow()
			if st.Shard != "" {
				flow += " [" + st.Shard + "]"
			}
			r.emit(mjson.Text, flow)
			r.nl()
		}
	}
	if len(p.Pipeline) > 0 {
		r.heading("Pipeline stages")
		for _, ps := range p.Pipeline {
			r.emit(mjson.Key, "  "+ps.Name)
			if ps.Stats {
				r.emit(mjson.Muted, "  returned %s · %s", Count(ps.NReturned), Millis(float64(ps.TimeMs)))
			}
			if ps.UsedDisk {
				r.emit(mjson.Warn, "  used disk")
			}
			r.nl()
		}
	}
	if raw != nil {
		r.heading("Raw explain")
		r.toks = append(r.toks, mjson.Tokens(raw, true)...)
		r.nl()
	}
	return r.toks
}

func (r *report) tree(st *Stage, first, rest string) {
	r.emit(mjson.Muted, first)
	kind := mjson.Key
	switch {
	case st.Name == "COLLSCAN" || st.Name == "SORT":
		kind = mjson.Warn
	case isIndexStage(st.Name):
		kind = mjson.Good
	}
	r.emit(kind, st.Name)
	if st.Shard != "" {
		r.emit(mjson.Accent, " ["+st.Shard+"]")
	}
	if d := stageDetail(st); d != "" {
		r.emit(mjson.Text, " "+d)
	}
	if st.Stats {
		parts := []string{"returned " + Count(st.NReturned)}
		if st.Keys > 0 {
			parts = append(parts, "keys "+Count(st.Keys))
		}
		if st.Docs > 0 {
			parts = append(parts, "docs "+Count(st.Docs))
		}
		if st.TimeMs > 0 {
			parts = append(parts, Millis(float64(st.TimeMs)))
		}
		r.emit(mjson.Muted, "  · "+strings.Join(parts, " · "))
	}
	r.nl()
	for i, c := range st.Children {
		if i == len(st.Children)-1 {
			r.tree(c, rest+"└─ ", rest+"   ")
		} else {
			r.tree(c, rest+"├─ ", rest+"│  ")
		}
	}
}

func stageDetail(st *Stage) string {
	s := st.Spec
	var parts []string
	add := func(label string, v interface{}) {
		if v == nil {
			return
		}
		if d, ok := v.(bson.D); ok && len(d) == 0 {
			return
		}
		parts = append(parts, label+compact(v, 160))
	}
	switch {
	case isIndexStage(st.Name):
		if name := st.indexName(); name != "" {
			parts = append(parts, name)
		}
		if kp, ok := get(s, "keyPattern").(string); ok {
			parts = append(parts, kp)
		} else {
			add("", get(s, "keyPattern"))
		}
		if b := boundsText(getDoc(s, "indexBounds")); b != "" {
			parts = append(parts, "bounds "+b)
		}
		if getStr(s, "direction") == "backward" {
			parts = append(parts, "backward")
		}
	case st.Name == "COLLSCAN" || st.Name == "FETCH":
		add("filter ", get(s, "filter"))
	case st.Name == "SORT" || st.Name == "SORT_KEY_GENERATOR":
		add("", get(s, "sortPattern"))
		if n := getInt(s, "limitAmount"); n > 0 {
			parts = append(parts, "top "+Count(n))
		}
		if getBool(s, "usedDisk") {
			parts = append(parts, "spilled to disk")
		}
	case st.Name == "LIMIT":
		parts = append(parts, Count(getInt(s, "limitAmount")))
	case st.Name == "SKIP":
		parts = append(parts, Count(getInt(s, "skipAmount")))
	case strings.HasPrefix(st.Name, "PROJECTION"):
		add("", get(s, "transformBy"))
	case st.Name == "EQ_LOOKUP":
		parts = append(parts, fmt.Sprintf("from %s on %s = %s · %s",
			getStr(s, "foreignCollection"), getStr(s, "localField"), getStr(s, "foreignField"), getStr(s, "strategy")))
	case st.Name == "SHARD_MERGE" || st.Name == "SINGLE_SHARD":
		if mt := getStr(s, "mergeType"); mt != "" {
			parts = append(parts, mt)
		}
	}
	return strings.Join(parts, " ")
}

// boundsText renders indexBounds as `status ["A", "A"], qty (5, inf.0]`.
func boundsText(bounds bson.D) string {
	if start := get(bounds, "startKey"); start != nil {
		return compact(start, 80) + " → " + compact(get(bounds, "endKey"), 80)
	}
	var parts []string
	for _, e := range bounds {
		var ivs []string
		for _, iv := range toArr(e.Value) {
			if s, ok := iv.(string); ok {
				ivs = append(ivs, s)
			}
		}
		if len(ivs) > 4 {
			ivs = append(ivs[:4], fmt.Sprintf("… %d more", len(ivs)-4))
		}
		parts = append(parts, e.Key+" "+strings.Join(ivs, " ∪ "))
	}
	return strings.Join(parts, ", ")
}
