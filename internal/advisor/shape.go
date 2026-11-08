package advisor

import (
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
)

// Op is a read or write described by a server command, reduced to what plan
// analysis needs. Command is the original command (or a reconstruction) used
// to re-run it through explain.
type Op struct {
	Kind     string // find, aggregate, count, distinct, update, delete, findAndModify
	Coll     string
	Filter   bson.D
	Sort     bson.D
	Pipeline bson.A
	Command  bson.D
}

// sessionKeys are command fields that would make a captured command fail or
// misbehave when re-sent through explain.
var sessionKeys = map[string]bool{
	"$db": true, "lsid": true, "$clusterTime": true, "txnNumber": true, "autocommit": true,
	"startTransaction": true, "$readPreference": true, "apiVersion": true, "apiStrict": true,
	"apiDeprecationErrors": true, "readConcern": true, "writeConcern": true, "$audit": true,
	"$client": true, "mayBypassWriteBlocking": true, "maxTimeMS": true, "$configTime": true,
	"$topologyTime": true, "$configServerState": true, "shardVersion": true, "databaseVersion": true,
	"$queryOptions": true, "cursor": true, "batchSize": true, "singleBatch": true,
}

// OpFromCommand recognizes find, aggregate, count, distinct, findAndModify,
// and update/delete commands (their first statement).
func OpFromCommand(cmd bson.D) (Op, bool) {
	if len(cmd) == 0 || get(cmd, "$truncated") != nil {
		return Op{}, false
	}
	name := cmd[0].Key
	coll, _ := cmd[0].Value.(string)
	op := Op{Coll: coll}
	switch strings.ToLower(name) {
	case "find":
		op.Kind, op.Filter, op.Sort = "find", getDoc(cmd, "filter"), getDoc(cmd, "sort")
	case "count":
		op.Kind, op.Filter = "count", getDoc(cmd, "query")
	case "distinct":
		op.Kind, op.Filter = "distinct", getDoc(cmd, "query")
	case "findandmodify":
		op.Kind, op.Filter, op.Sort = "findAndModify", getDoc(cmd, "query"), getDoc(cmd, "sort")
	case "aggregate":
		op.Kind, op.Pipeline = "aggregate", getArr(cmd, "pipeline")
		op.Filter, op.Sort = pipelinePrefix(op.Pipeline)
	case "update", "delete":
		list := "updates"
		if name == "delete" {
			list = "deletes"
		}
		stmts := asDocs(getArr(cmd, list))
		if len(stmts) == 0 {
			return Op{}, false
		}
		op.Kind, op.Filter = name, getDoc(stmts[0], "q")
		cmd = append(clean(cmd, list), bson.E{Key: list, Value: bson.A{stmts[0]}})
	default:
		return Op{}, false
	}
	if op.Coll == "" {
		return Op{}, false
	}
	op.Command = clean(cmd, "")
	return op, true
}

func clean(cmd bson.D, drop string) bson.D {
	out := make(bson.D, 0, len(cmd))
	for _, e := range cmd {
		if sessionKeys[e.Key] || e.Key == drop {
			continue
		}
		out = append(out, e)
	}
	if len(out) > 0 && out[0].Key == "aggregate" {
		out = append(out, bson.E{Key: "cursor", Value: bson.D{}})
	}
	return out
}

// pipelinePrefix merges the leading $match stages and an immediately
// following $sort: the part of a pipeline the query planner can index.
func pipelinePrefix(p bson.A) (filter, sort bson.D) {
	var ands bson.A
	for _, raw := range p {
		st := toD(raw)
		if len(st) == 0 {
			break
		}
		switch st[0].Key {
		case "$match":
			if m := toD(st[0].Value); len(m) > 0 {
				ands = append(ands, m)
			}
			continue
		case "$sort":
			sort = toD(st[0].Value)
		}
		break
	}
	switch len(ands) {
	case 0:
	case 1:
		filter = ands[0].(bson.D)
	default:
		filter = bson.D{{Key: "$and", Value: ands}}
	}
	return filter, sort
}

// OpFromProfile reads a system.profile entry. getMore entries resolve to
// their originating command so their cost is charged to the same shape.
func OpFromProfile(entry bson.D) (Op, bool) {
	ns := getStr(entry, "ns")
	_, coll, _ := strings.Cut(ns, ".")
	cmd := getDoc(entry, "command")
	switch getStr(entry, "op") {
	case "getmore":
		cmd = getDoc(entry, "originatingCommand")
	case "update", "remove":
		list, name := "updates", "update"
		if getStr(entry, "op") == "remove" {
			list, name = "deletes", "delete"
		}
		if len(cmd) == 0 || get(cmd, "q") == nil {
			return Op{}, false
		}
		cmd = bson.D{{Key: name, Value: coll}, {Key: list, Value: bson.A{cmd}}}
	case "query", "command":
	default:
		return Op{}, false
	}
	op, ok := OpFromCommand(cmd)
	if ok && op.Coll != coll && coll != "" && op.Kind != "aggregate" {
		op.Coll = coll
	}
	return op, ok
}

// ExplainCommand wraps op in an explain command. A positive maxTimeMS goes on
// the explain itself: the server ignores it inside the explained command, and
// on expiry returns the plan with executionSuccess false and partial stats.
// Explaining $out/$merge never writes, even with executionStats.
func (op Op) ExplainCommand(verbosity string, maxTimeMS int64) bson.D {
	if verbosity == "" {
		verbosity = "executionStats"
	}
	cmd := bson.D{{Key: "explain", Value: op.Command}, {Key: "verbosity", Value: verbosity}}
	if maxTimeMS > 0 {
		cmd = append(cmd, bson.E{Key: "maxTimeMS", Value: maxTimeMS})
	}
	return cmd
}

// Shape replaces literal values in a filter with "?" so queries that differ
// only in their constants group together. Field names, operators and logical
// structure are kept; fields are sorted so their order doesn't matter.
func Shape(filter bson.D) bson.D {
	out := make(bson.D, 0, len(filter))
	for _, e := range filter {
		switch {
		case e.Key == "$and" || e.Key == "$or" || e.Key == "$nor":
			var arr bson.A
			for _, sub := range toArr(e.Value) {
				arr = append(arr, Shape(toD(sub)))
			}
			out = append(out, bson.E{Key: e.Key, Value: arr})
		case strings.HasPrefix(e.Key, "$"):
			out = append(out, bson.E{Key: e.Key, Value: "?"})
		default:
			out = append(out, bson.E{Key: e.Key, Value: shapeValue(e.Value)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func isOps(d bson.D) bool { return len(d) > 0 && strings.HasPrefix(d[0].Key, "$") }

func shapeValue(v interface{}) interface{} {
	d := toD(v)
	if !isOps(d) {
		return "?"
	}
	out := make(bson.D, 0, len(d))
	for _, op := range d {
		val := interface{}("?")
		switch op.Key {
		case "$not":
			val = shapeValue(op.Value)
		case "$elemMatch":
			if sub := toD(op.Value); isOps(sub) {
				val = shapeValue(sub)
			} else if sub != nil {
				val = Shape(sub)
			}
		}
		out = append(out, bson.E{Key: op.Key, Value: val})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ShapeText is a compact human-readable shape, e.g.
// find {status: "?"} sort {created: -1}.
func (op Op) ShapeText() string {
	var b strings.Builder
	b.WriteString(op.Kind)
	if op.Kind == "aggregate" {
		var names []string
		for _, raw := range op.Pipeline {
			if st := toD(raw); len(st) > 0 {
				names = append(names, st[0].Key)
			}
		}
		b.WriteString(" [" + strings.Join(names, ", ") + "]")
	}
	if len(op.Filter) > 0 {
		b.WriteString(" " + compact(Shape(op.Filter), 0))
	}
	if len(op.Sort) > 0 {
		b.WriteString(" sort " + compact(op.Sort, 0))
	}
	if op.Kind == "distinct" {
		b.WriteString(" key " + getStr(op.Command, "key"))
	}
	return b.String()
}
