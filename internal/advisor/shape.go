package advisor

import (
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
// structure are kept; fields are sorted so their order doesn't matter. Regex
// predicates keep a regex marker (see regexShape) so they never group with
// equality on the same field.
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
		case e.Key == "$expr":
			out = append(out, bson.E{Key: e.Key, Value: shapeExpr(e.Value)})
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
	if re, ok := v.(primitive.Regex); ok {
		return regexShape(re.Pattern, re.Options)
	}
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
		case "$regex":
			switch re := op.Value.(type) {
			case primitive.Regex:
				val = regexShape(re.Pattern, re.Options)
			case string:
				val = regexShape(re, "")
			}
		case "$options":
			if s, ok := op.Value.(string); ok {
				val = s
			}
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

// regexShape hides a regex pattern but keeps its flags and whether it is
// anchored with ^, since both decide whether an index can bound the scan.
func regexShape(pattern, options string) primitive.Regex {
	p := "?"
	if strings.HasPrefix(pattern, "^") || strings.HasPrefix(pattern, `\A`) {
		p = "^?"
	}
	return primitive.Regex{Pattern: p, Options: options}
}

// stageNameKeys are stage options whose values name collections, fields or
// merge modes rather than carry data, so they are kept verbatim.
var stageNameKeys = map[string]bool{
	"from": true, "coll": true, "db": true, "into": true, "on": true, "as": true,
	"localField": true, "foreignField": true, "connectFromField": true, "connectToField": true,
	"depthField": true, "distanceField": true, "includeLocs": true, "includeArrayIndex": true,
	"path": true, "key": true, "field": true, "whenMatched": true, "whenNotMatched": true,
}

// ShapePipeline normalizes every stage of an aggregation pipeline: field
// paths, operators, output names and collection names are kept, literal
// values become "?".
func ShapePipeline(p bson.A) bson.A {
	out := make(bson.A, 0, len(p))
	for _, raw := range p {
		st := toD(raw)
		if len(st) == 0 {
			out = append(out, "?")
			continue
		}
		shaped := make(bson.D, 0, len(st))
		for _, e := range st {
			shaped = append(shaped, bson.E{Key: e.Key, Value: shapeStage(e.Key, e.Value)})
		}
		out = append(out, shaped)
	}
	return out
}

func shapeStage(name string, arg interface{}) interface{} {
	switch name {
	case "$match":
		return Shape(toD(arg))
	case "$sort", "$unset", "$count":
		return arg
	case "$facet":
		d := toD(arg)
		out := make(bson.D, 0, len(d))
		for _, e := range d {
			out = append(out, bson.E{Key: e.Key, Value: ShapePipeline(toArr(e.Value))})
		}
		return out
	case "$project":
		d := toD(arg)
		out := make(bson.D, 0, len(d))
		for _, e := range d {
			val := e.Value
			if _, ok := num(val); !ok {
				if _, ok := val.(bool); !ok {
					val = shapeExpr(val)
				}
			}
			out = append(out, bson.E{Key: e.Key, Value: val})
		}
		return out
	case "$addFields", "$set", "$group", "$replaceRoot", "$replaceWith", "$redact", "$sortByCount", "$bucket", "$bucketAuto", "$setWindowFields":
		return shapeExpr(arg)
	}
	// $out/$merge/$unionWith/$unwind take a bare collection or field name.
	if s, ok := arg.(string); ok {
		return s
	}
	d := toD(arg)
	if d == nil {
		return shapeExpr(arg)
	}
	out := make(bson.D, 0, len(d))
	for _, e := range d {
		val := e.Value
		switch {
		case stageNameKeys[e.Key]:
		case e.Key == "pipeline":
			val = ShapePipeline(toArr(e.Value))
		case e.Key == "query" || e.Key == "restrictSearchWithMatch":
			val = Shape(toD(e.Value))
		default:
			if _, ok := val.(bool); !ok {
				val = shapeExpr(val)
			}
		}
		out = append(out, bson.E{Key: e.Key, Value: val})
	}
	return out
}

// shapeExpr normalizes an aggregation expression: "$field" paths and
// "$$variables", operators and document keys are kept; literals become "?".
// An array made only of literals collapses to a single "?".
func shapeExpr(v interface{}) interface{} {
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "$") {
			return x
		}
		return "?"
	case primitive.Regex:
		return regexShape(x.Pattern, x.Options)
	case bson.A, []interface{}:
		arr := toArr(x)
		out := make(bson.A, 0, len(arr))
		literal := true
		for _, el := range arr {
			s := shapeExpr(el)
			if s != "?" {
				literal = false
			}
			out = append(out, s)
		}
		if literal {
			return "?"
		}
		return out
	}
	d := toD(v)
	if d == nil {
		return "?"
	}
	out := make(bson.D, 0, len(d))
	for _, e := range d {
		if e.Key == "$literal" {
			out = append(out, bson.E{Key: e.Key, Value: "?"})
			continue
		}
		out = append(out, bson.E{Key: e.Key, Value: shapeExpr(e.Value)})
	}
	return out
}

// ShapeText is a compact human-readable shape, e.g.
// find {status: "?"} sort {created: -1}. Aggregations list every normalized
// stage: aggregate [$match {...}, $group {...}].
func (op Op) ShapeText() string {
	var b strings.Builder
	b.WriteString(op.Kind)
	if op.Kind == "aggregate" {
		var stages []string
		for _, raw := range ShapePipeline(op.Pipeline) {
			st := toD(raw)
			if len(st) == 0 {
				stages = append(stages, "?")
				continue
			}
			parts := make([]string, 0, len(st))
			for _, e := range st {
				parts = append(parts, e.Key+" "+compact(e.Value, 0))
			}
			stages = append(stages, strings.Join(parts, " "))
		}
		b.WriteString(" [" + strings.Join(stages, ", ") + "]")
		return b.String()
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
