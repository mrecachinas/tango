package query

import (
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/advisor"
	"tango/internal/documents"
	"tango/internal/mjson"
	"tango/internal/state"
)

var verbosities = map[string]string{
	"queryplanner":      "queryPlanner",
	"executionstats":    "executionStats",
	"allplansexecution": "allPlansExecution",
	"true":              "allPlansExecution",
	"false":             "queryPlanner",
}

func parseVerbosity(v interface{}) (string, error) {
	if v == nil {
		return "executionStats", nil
	}
	if s, ok := verbosities[strings.ToLower(strings.Trim(fmt.Sprint(v), `"'`))]; ok {
		return s, nil
	}
	return "", fmt.Errorf("unknown explain verbosity %v — use queryPlanner, executionStats or allPlansExecution", v)
}

// splitExplain detects db.c.find(…).explain(v) and db.c.explain(v).find(…),
// returning the query without the explain call.
func splitExplain(q shellQuery) (shellQuery, string, bool, error) {
	if n := len(q.Chain); n > 0 && q.Chain[n-1].Name == "explain" {
		v, err := parseVerbosity(first(q.Chain[n-1].Args))
		q.Chain = q.Chain[:n-1]
		return q, v, true, err
	}
	if q.Method == "explain" {
		v, err := parseVerbosity(first(q.Args))
		if len(q.Chain) == 0 {
			return q, v, true, fmt.Errorf("explain() must be followed by a query, e.g. db.c.explain().find({...})")
		}
		q.Method, q.Args, q.Chain = q.Chain[0].Name, q.Chain[0].Args, q.Chain[1:]
		return q, v, true, err
	}
	return q, "", false, nil
}

func first(a bson.A) interface{} {
	if len(a) == 0 {
		return nil
	}
	return a[0]
}

// explainCommand builds the server command that q runs, for explain.
func explainCommand(q shellQuery) (bson.D, error) {
	c := q.Coll
	opt := func(cmd bson.D, key string, v interface{}) bson.D {
		switch x := v.(type) {
		case nil:
			return cmd
		case bson.D:
			if len(x) == 0 {
				return cmd
			}
		case int64:
			if x == 0 {
				return cmd
			}
		}
		return append(cmd, bson.E{Key: key, Value: v})
	}
	filterArg := func(i int, what string) (bson.D, error) {
		f, err := docArg(q.Args, i, what)
		if f == nil {
			f = bson.D{}
		}
		return f, err
	}
	switch q.Method {
	case "find", "findOne":
		filter, err := filterArg(0, q.Method+"() filter")
		if err != nil {
			return nil, err
		}
		proj, err := docArg(q.Args, 1, q.Method+"() projection")
		if err != nil {
			return nil, err
		}
		cmd := bson.D{{Key: "find", Value: c}, {Key: "filter", Value: filter}}
		var sort, hint, collation bson.D
		var skip, limit int64
		for _, ch := range q.Chain {
			switch ch.Name {
			case "sort":
				sort, err = docArg(ch.Args, 0, "sort()")
			case "projection":
				proj, err = docArg(ch.Args, 0, "projection()")
			case "hint":
				if name, ok := first(ch.Args).(string); ok {
					cmd = append(cmd, bson.E{Key: "hint", Value: name})
				} else {
					hint, err = docArg(ch.Args, 0, "hint()")
				}
			case "collation":
				collation, err = docArg(ch.Args, 0, "collation()")
			case "limit":
				limit, err = intArg(ch.Args, "limit")
			case "skip":
				skip, err = intArg(ch.Args, "skip")
			default:
				err = fmt.Errorf("unsupported cursor method .%s() — supported: sort, limit, skip, projection, hint, collation", ch.Name)
			}
			if err != nil {
				return nil, err
			}
		}
		if q.Method == "findOne" {
			limit = 1
			cmd = append(cmd, bson.E{Key: "singleBatch", Value: true})
		}
		cmd = opt(cmd, "projection", proj)
		cmd = opt(cmd, "sort", sort)
		cmd = opt(cmd, "hint", hint)
		cmd = opt(cmd, "collation", collation)
		cmd = opt(cmd, "skip", skip)
		return opt(cmd, "limit", limit), nil
	case "count", "countDocuments":
		if err := maxArgs(q, 1); err != nil {
			return nil, err
		}
		filter, err := filterArg(0, q.Method+"() filter")
		return bson.D{{Key: "count", Value: c}, {Key: "query", Value: filter}}, err
	case "estimatedDocumentCount":
		return bson.D{{Key: "count", Value: c}}, maxArgs(q, 0)
	case "distinct":
		if err := maxArgs(q, 2); err != nil {
			return nil, err
		}
		field, _ := first(q.Args).(string)
		if field == "" {
			return nil, fmt.Errorf("distinct() takes a field name string and an optional filter")
		}
		filter, err := filterArg(1, "distinct() filter")
		return bson.D{{Key: "distinct", Value: c}, {Key: "key", Value: field}, {Key: "query", Value: filter}}, err
	case "aggregate":
		if err := maxArgs(q, 2); err != nil {
			return nil, err
		}
		pipeline, ok := first(q.Args).(bson.A)
		if !ok {
			return nil, fmt.Errorf("aggregate() takes a pipeline array")
		}
		cmd := bson.D{{Key: "aggregate", Value: c}, {Key: "pipeline", Value: pipeline}, {Key: "cursor", Value: bson.D{}}}
		aggOpts, err := docArg(q.Args, 1, "aggregate() options")
		if v, ok := mjson.Get(aggOpts, "allowDiskUse"); ok {
			cmd = append(cmd, bson.E{Key: "allowDiskUse", Value: v})
		}
		return cmd, err
	case "updateOne", "updateMany", "replaceOne":
		if len(q.Args) < 2 || len(q.Chain) > 0 {
			return nil, fmt.Errorf("%s() takes a filter and an update", q.Method)
		}
		filter, err := filterArg(0, q.Method+"() filter")
		stmt := bson.D{{Key: "q", Value: filter}, {Key: "u", Value: q.Args[1]}, {Key: "multi", Value: q.Method == "updateMany"}}
		return bson.D{{Key: "update", Value: c}, {Key: "updates", Value: bson.A{stmt}}}, err
	case "deleteOne", "deleteMany":
		if err := maxArgs(q, 1); err != nil {
			return nil, err
		}
		filter, err := filterArg(0, q.Method+"() filter")
		limit := int32(0)
		if q.Method == "deleteOne" {
			limit = 1
		}
		stmt := bson.D{{Key: "q", Value: filter}, {Key: "limit", Value: limit}}
		return bson.D{{Key: "delete", Value: c}, {Key: "deletes", Value: bson.A{stmt}}}, err
	}
	return nil, fmt.Errorf("can't explain %s() — supported: find, findOne, count, distinct, aggregate, update*, delete*", q.Method)
}

func explainShell(q shellQuery, verbosity, text string) error {
	cmd, err := explainCommand(q)
	if err != nil {
		return err
	}
	op, ok := advisor.OpFromCommand(cmd)
	if !ok {
		return fmt.Errorf("can't explain this query")
	}
	documents.Explain(state.S.CurrentDB, op, verbosity, text)
	return nil
}

// runExplainCommand handles ":explain [verbosity] [db.query | {filter} | Qn]".
// With no query it explains what the content pane is showing.
func runExplainCommand(args string) {
	s := state.S
	if s.CurrentDB == "" {
		state.ShowError("Select a database first.")
		return
	}
	verbosity := "executionStats"
	if word, rest, _ := strings.Cut(strings.TrimSpace(args), " "); word != "" {
		if v, ok := verbosities[strings.ToLower(word)]; ok {
			verbosity, args = v, rest
		}
	}
	args = strings.TrimSpace(args)
	var err error
	switch {
	case args == "":
		err = explainCurrent(verbosity)
	case strings.HasPrefix(args, "db."):
		var q shellQuery
		if q, err = parseShellQuery(args); err == nil {
			var v string
			var explicit bool
			if q, v, explicit, err = splitExplain(q); explicit && err == nil {
				verbosity = v
			}
			if err == nil {
				err = explainShell(q, verbosity, args)
			}
		}
	case strings.HasPrefix(args, "{"):
		if s.CurrentColl == "" {
			err = fmt.Errorf("select a collection first, or pass db.collection.find({...})")
			break
		}
		var filter bson.D
		if filter, err = parseFilter(args); err == nil {
			err = explainBrowse(filter, s.CurrentSort, s.CurrentProjection, 0, 0, verbosity)
		}
	case len(args) > 1 && (args[0] == 'Q' || args[0] == 'q'):
		err = documents.ExplainInsightQuery(strings.ToUpper(args), verbosity)
	default:
		err = fmt.Errorf("usage: :explain [queryPlanner|executionStats|allPlansExecution] [db.coll.find({...}) | {filter} | Q1]")
	}
	if err != nil {
		state.ShowError(err.Error())
	}
}

// explainCurrent explains the ad-hoc query behind the current results, or
// the browse query for the current page.
func explainCurrent(verbosity string) error {
	s := state.S
	if s.ResultLabel != "" && strings.HasPrefix(s.QueryText, "db.") {
		q, err := parseShellQuery(s.QueryText)
		if err != nil {
			return err
		}
		if q, _, _, err = splitExplain(q); err != nil {
			return err
		}
		return explainShell(q, verbosity, s.QueryText)
	}
	if s.CurrentColl == "" {
		return fmt.Errorf("select a collection to explain its query, or pass one: :explain db.coll.find({...})")
	}
	limit := int64(state.PageSize)
	offset := int64(s.CurrentPage) * limit
	if s.QueryLimit > 0 {
		limit = min(limit, max(s.QueryLimit-offset, 1))
	}
	return explainBrowse(s.CurrentFilter, s.CurrentSort, s.CurrentProjection, s.QuerySkip+offset, limit, verbosity)
}

func explainBrowse(filter, sort, proj bson.D, skip, limit int64, verbosity string) error {
	s := state.S
	if filter == nil {
		filter = bson.D{}
	}
	q := shellQuery{Coll: s.CurrentColl, Method: "find", Args: bson.A{filter}}
	if len(proj) > 0 {
		q.Args = append(q.Args, proj)
	}
	if len(sort) > 0 {
		q.Chain = append(q.Chain, call{Name: "sort", Args: bson.A{sort}})
	}
	if skip > 0 {
		q.Chain = append(q.Chain, call{Name: "skip", Args: bson.A{skip}})
	}
	if limit > 0 {
		q.Chain = append(q.Chain, call{Name: "limit", Args: bson.A{limit}})
	}
	return explainShell(q, verbosity, shellText(q))
}

// shellText renders a find as the shell query a user would type.
func shellText(q shellQuery) string {
	var b strings.Builder
	b.WriteString(advisor.CollRef(q.Coll) + "." + q.Method + "(")
	for i, a := range q.Args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(mjson.Compact(a))
	}
	b.WriteString(")")
	for _, c := range q.Chain {
		b.WriteString("." + c.Name + "(")
		for i, a := range c.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(mjson.Compact(a))
		}
		b.WriteString(")")
	}
	return b.String()
}
