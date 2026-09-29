package query

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tango/internal/documents"
	"tango/internal/mjson"
	"tango/internal/state"
)

const maxResultCap = 10000

// setBrowseQuery installs a query on the paged collection view and loads it.
func setBrowseQuery(filter, projection, sort bson.D, skip, limit int64) {
	s := state.S
	s.CurrentFilter = filter
	s.CurrentProjection = projection
	s.CurrentSort = sort
	s.QuerySkip = skip
	s.QueryLimit = limit
	s.CurrentPage = 0
	s.PageStarts = nil
	s.SnapshotID = nil
	s.CountDirty = true
	documents.LoadDocuments()
}

func ExecuteQuery(queryStr string) {
	s := state.S
	queryStr = strings.TrimSpace(queryStr)

	if queryStr == "" {
		if s.CurrentDB != "" && s.CurrentColl != "" {
			setBrowseQuery(nil, nil, s.CurrentSort, 0, 0)
		}
		return
	}

	state.AddToQueryHistory(queryStr)
	s.QueryHistoryIdx = -1

	if strings.HasPrefix(queryStr, ":") {
		s.SearchBar.SetText("")
		executeCommand(queryStr[1:])
		return
	}

	if strings.HasPrefix(queryStr, "db.") {
		runShellQuery(queryStr)
		return
	}

	if s.CurrentDB == "" || s.CurrentColl == "" {
		state.ShowError("Select a database and collection first,\nor use db.collection.find({...})")
		return
	}
	filter, err := parseFilter(queryStr)
	if err != nil {
		state.ShowError(fmt.Sprintf("Invalid filter: %s\n\nTry: {key: \"value\"} or db.collection.find({key: \"value\"})", err))
		return
	}
	setBrowseQuery(filter, s.CurrentProjection, s.CurrentSort, s.QuerySkip, s.QueryLimit)
}

func docArg(args bson.A, i int, what string) (bson.D, error) {
	if i >= len(args) {
		return nil, nil
	}
	d, ok := args[i].(bson.D)
	if !ok {
		return nil, fmt.Errorf("%s must be a document", what)
	}
	return d, nil
}

func intArg(args bson.A, what string) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("%s() takes one number", what)
	}
	n, ok := state.ToFloat64(args[0])
	if !ok || n < 0 || n != float64(int64(n)) {
		return 0, fmt.Errorf("%s() takes a non-negative integer", what)
	}
	return int64(n), nil
}

func maxArgs(q shellQuery, n int) error {
	if len(q.Args) > n {
		return fmt.Errorf("%s() takes at most %d argument(s)", q.Method, n)
	}
	if len(q.Chain) > 0 {
		return fmt.Errorf("%s() doesn't support chained .%s()", q.Method, q.Chain[0].Name)
	}
	return nil
}

func runShellQuery(query string) {
	s := state.S
	if s.CurrentDB == "" {
		state.ShowError("Select a database first.")
		return
	}
	q, err := parseShellQuery(query)
	if err != nil {
		state.ShowError(err.Error())
		return
	}
	if err := runParsed(q); err != nil {
		state.ShowError(err.Error())
	}
}

func runParsed(q shellQuery) error {
	s := state.S
	ns := state.Namespace{DB: s.CurrentDB, Coll: q.Coll}
	coll := s.Client.Database(ns.DB).Collection(ns.Coll)
	label := func(m string) string { return fmt.Sprintf("%s(%s)", m, ns) }
	timeout := 3 * s.QueryTimeout

	switch q.Method {
	case "find":
		if len(q.Args) > 2 {
			return fmt.Errorf("find() takes a filter and an optional projection")
		}
		filter, err := docArg(q.Args, 0, "find() filter")
		if err != nil {
			return err
		}
		proj, err := docArg(q.Args, 1, "find() projection")
		if err != nil {
			return err
		}
		var sort bson.D
		var skip, limit int64
		for _, c := range q.Chain {
			switch c.Name {
			case "sort":
				if sort, err = docArg(c.Args, 0, "sort()"); err != nil || len(c.Args) != 1 {
					return fmt.Errorf("sort() takes one document")
				}
			case "projection":
				if proj, err = docArg(c.Args, 0, "projection()"); err != nil || len(c.Args) != 1 {
					return fmt.Errorf("projection() takes one document")
				}
			case "limit":
				if limit, err = intArg(c.Args, "limit"); err != nil {
					return err
				}
			case "skip":
				if skip, err = intArg(c.Args, "skip"); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported cursor method .%s() — supported: sort, limit, skip, projection", c.Name)
			}
		}
		documents.StopLiveMode()
		if s.CurrentColl != q.Coll {
			s.CurrentColl = q.Coll
			s.CurrentSort = nil
		}
		documents.SelectCollectionInList(q.Coll)
		setBrowseQuery(filter, proj, sort, skip, limit)

	case "findOne":
		if err := maxArgs(q, 2); err != nil {
			return err
		}
		filter, err := docArg(q.Args, 0, "findOne() filter")
		if err != nil {
			return err
		}
		proj, err := docArg(q.Args, 1, "findOne() projection")
		if err != nil {
			return err
		}
		if filter == nil {
			filter = bson.D{}
		}
		opts := options.FindOne()
		if len(proj) > 0 {
			opts.SetProjection(proj)
		}
		state.AsyncTimeout("Executing findOne", timeout, func(ctx context.Context) func() {
			var doc bson.D
			err := coll.FindOne(ctx, filter, opts).Decode(&doc)
			return func() {
				if err == mongo.ErrNoDocuments {
					state.ShowError("No matching document found.")
					return
				}
				if err != nil {
					state.ShowError("findOne error: " + err.Error())
					return
				}
				documents.ShowResultDocs([]bson.D{doc}, ns, label("findOne"), true, len(proj) == 0)
				documents.ExpandDocument(0)
			}
		})

	case "count", "countDocuments":
		if err := maxArgs(q, 1); err != nil {
			return err
		}
		filter, err := docArg(q.Args, 0, q.Method+"() filter")
		if err != nil {
			return err
		}
		if filter == nil {
			filter = bson.D{}
		}
		state.AsyncTimeout("Counting", timeout, func(ctx context.Context) func() {
			n, err := coll.CountDocuments(ctx, filter)
			return func() {
				if err != nil {
					state.ShowError("count error: " + err.Error())
					return
				}
				state.ShowMessage(fmt.Sprintf(" count [yellow](%s)[white] ", tview.Escape(ns.String())), strconv.FormatInt(n, 10))
			}
		})

	case "estimatedDocumentCount":
		if err := maxArgs(q, 0); err != nil {
			return err
		}
		state.AsyncTimeout("Counting", timeout, func(ctx context.Context) func() {
			n, err := coll.EstimatedDocumentCount(ctx)
			return func() {
				if err != nil {
					state.ShowError("estimatedDocumentCount error: " + err.Error())
					return
				}
				state.ShowMessage(fmt.Sprintf(" estimated count [yellow](%s)[white] ", tview.Escape(ns.String())), strconv.FormatInt(n, 10))
			}
		})

	case "aggregate":
		if err := maxArgs(q, 2); err != nil {
			return err
		}
		if len(q.Args) == 0 {
			return fmt.Errorf("aggregate() takes a pipeline array")
		}
		stages, ok := q.Args[0].(bson.A)
		if !ok {
			return fmt.Errorf("aggregate() pipeline must be an array of stages")
		}
		pipeline := make(mongo.Pipeline, 0, len(stages))
		for _, st := range stages {
			d, ok := st.(bson.D)
			if !ok || len(d) != 1 {
				return fmt.Errorf("each pipeline stage must be a document with one operator")
			}
			pipeline = append(pipeline, d)
		}
		aggOpts, err := docArg(q.Args, 1, "aggregate() options")
		if err != nil {
			return err
		}
		opts := options.Aggregate()
		if v, ok := mjson.Get(aggOpts, "allowDiskUse"); ok {
			if b, ok := v.(bool); ok {
				opts.SetAllowDiskUse(b)
			}
		}
		target, writes := outputTarget(pipeline, ns.DB)
		if writes && !state.CheckWrite(state.ActInsert, target) {
			return nil
		}
		state.AsyncTimeout("Running aggregation", timeout, func(ctx context.Context) func() {
			docs, capped, err := aggregate(ctx, coll, pipeline, opts)
			return func() {
				if err != nil {
					state.ShowError("aggregate error: " + err.Error())
					return
				}
				if writes {
					state.ShowMessage(" aggregate ", "Pipeline wrote its output to "+target.String())
					return
				}
				if len(docs) == 0 {
					state.ShowMessage(" aggregate ", "(no results)")
					return
				}
				l := label("aggregate")
				if capped {
					l += " ⚠ showing first 10,000"
				}
				documents.ShowResultDocs(docs, ns, l, false, false)
			}
		})

	case "distinct":
		if err := maxArgs(q, 2); err != nil {
			return err
		}
		field, ok := "", len(q.Args) > 0
		if ok {
			field, ok = q.Args[0].(string)
		}
		if !ok || field == "" {
			return fmt.Errorf("distinct() takes a field name string and an optional filter")
		}
		filter, err := docArg(q.Args, 1, "distinct() filter")
		if err != nil {
			return err
		}
		if filter == nil {
			filter = bson.D{}
		}
		state.AsyncTimeout("Running distinct", timeout, func(ctx context.Context) func() {
			values, err := coll.Distinct(ctx, field, filter)
			return func() {
				if err != nil {
					state.ShowError("distinct error: " + err.Error())
					return
				}
				total := len(values)
				if total > maxResultCap {
					values = values[:maxResultCap]
				}
				toks := mjson.Tokens(bson.A(values), true)
				if total > maxResultCap {
					toks = append(toks, mjson.Token{Text: fmt.Sprintf("\n\n⚠ Showing %d of %d values", maxResultCap, total), Kind: mjson.Error})
				}
				state.SetDetail(fmt.Sprintf(" distinct [yellow](%s.%s)[white] [green]%d[white] ", tview.Escape(ns.String()), tview.Escape(field), total), toks)
			}
		})

	default:
		return fmt.Errorf("unsupported method: %s\n\nSupported: find, findOne, count, countDocuments, estimatedDocumentCount, aggregate, distinct", q.Method)
	}
	return nil
}

// outputTarget reports the namespace written by a trailing $out/$merge stage.
func outputTarget(p mongo.Pipeline, db string) (state.Namespace, bool) {
	if len(p) == 0 {
		return state.Namespace{}, false
	}
	last := p[len(p)-1][0]
	var spec interface{}
	switch last.Key {
	case "$out":
		spec = last.Value
	case "$merge":
		spec = last.Value
		if d, ok := last.Value.(bson.D); ok {
			spec, _ = mjson.Get(d, "into")
		}
	default:
		return state.Namespace{}, false
	}
	ns := state.Namespace{DB: db}
	switch v := spec.(type) {
	case string:
		ns.Coll = v
	case bson.D:
		if d, ok := mjson.Get(v, "db"); ok {
			ns.DB, _ = d.(string)
		}
		if c, ok := mjson.Get(v, "coll"); ok {
			ns.Coll, _ = c.(string)
		}
	}
	return ns, true
}

// aggregate runs the pipeline unchanged and reads at most maxResultCap
// documents, so terminal stages like $out/$merge keep working.
func aggregate(ctx context.Context, coll *mongo.Collection, p mongo.Pipeline, opts *options.AggregateOptions) ([]bson.D, bool, error) {
	cur, err := coll.Aggregate(ctx, p, opts)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = cur.Close(context.Background()) }()
	var docs []bson.D
	for cur.Next(ctx) {
		if len(docs) == maxResultCap {
			return docs, true, nil
		}
		var d bson.D
		if err := cur.Decode(&d); err != nil {
			return nil, false, err
		}
		docs = append(docs, d)
	}
	return docs, false, cur.Err()
}

// suggestionText maps escaped autocomplete entries back to the raw text.
var suggestionText = map[string]string{}

// GetAutocompleteSuggestions returns escaped entries for the query bar's
// autocomplete list; SuggestionText maps a chosen entry back to raw text.
func GetAutocompleteSuggestions(currentText string) []string {
	raw := rawSuggestions(currentText)
	suggestionText = make(map[string]string, len(raw))
	out := make([]string, len(raw))
	for i, r := range raw {
		out[i] = tview.Escape(r)
		suggestionText[out[i]] = r
	}
	return out
}

// SuggestionText returns the raw text for an autocomplete entry.
func SuggestionText(entry string) string {
	if r, ok := suggestionText[entry]; ok {
		return r
	}
	return entry
}

func rawSuggestions(currentText string) []string {
	s := state.S
	text := strings.TrimSpace(currentText)

	if strings.HasPrefix(text, ":") {
		return getCommandSuggestions(text[1:])
	}
	if strings.Contains(text, "(") {
		return nil
	}
	if text == "" || (len(text) < 3 && strings.HasPrefix("db.", text)) {
		return []string{"db."}
	}
	if !strings.HasPrefix(text, "db.") {
		return nil
	}
	rest := text[3:]

	var suggestions []string
	for _, e := range s.AllCollItems {
		c := e.Name
		if strings.HasPrefix(rest, c+".") {
			partial := rest[len(c)+1:]
			for _, m := range []string{"find(", "findOne(", "countDocuments(", "estimatedDocumentCount()", "aggregate([", "distinct("} {
				if strings.HasPrefix(m, partial) {
					suggestions = append(suggestions, "db."+c+"."+m)
				}
			}
		} else if strings.HasPrefix(c, rest) {
			suggestions = append(suggestions, "db."+c+".")
		}
	}
	return suggestions
}

func showPanel(page string, list tview.Primitive) {
	s := state.S
	if s.WideMode {
		s.DocPages.SwitchToPage(page)
	}
	s.App.SetFocus(list)
	state.RefreshPanelBorders()
}

func executeCommand(cmd string) {
	s := state.S
	parts := strings.Fields(strings.TrimSpace(cmd))
	if len(parts) == 0 {
		return
	}
	name, args := strings.ToLower(parts[0]), parts[1:]

	switch name {
	case "indexes", "idx":
		if s.CurrentDB == "" || s.CurrentColl == "" {
			state.ShowError("Select a collection first")
			return
		}
		documents.ShowIndexes()

	case "stats", "profile":
		documents.ShowStats()

	case "killop":
		if len(args) != 1 {
			state.ShowError("Usage: :killop <opid>  (opids are listed in the stats view, S)")
			return
		}
		documents.KillOpConfirm(args[0])

	case "sort":
		if s.CurrentDB == "" || s.CurrentColl == "" {
			state.ShowError("Select a collection first")
			return
		}
		var spec bson.D
		for _, f := range args {
			order := 1
			if strings.HasPrefix(f, "-") {
				f, order = f[1:], -1
			} else {
				f = strings.TrimPrefix(f, "+")
			}
			if f != "" {
				spec = append(spec, bson.E{Key: f, Value: order})
			}
		}
		documents.SetSort(spec)

	case "limit":
		if len(args) < 1 {
			state.ShowMessage(" limit ", fmt.Sprintf("Current page size: %d", state.PageSize))
			return
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 || n > 10000 {
			state.ShowError("Invalid limit (1-10000)")
			return
		}
		state.PageSize = n
		s.CurrentPage = 0
		s.PageStarts = nil
		documents.LoadDocuments()

	case "export":
		usage := "Usage: :export json|csv [all] [path]\n\nWithout 'all', exports the selection or the current page.\nWith 'all', streams every document matching the current query."
		if len(args) < 1 {
			state.ShowError(usage)
			return
		}
		format := strings.ToLower(args[0])
		if format != "json" && format != "csv" {
			state.ShowError(usage)
			return
		}
		rest := args[1:]
		all := len(rest) > 0 && strings.EqualFold(rest[0], "all")
		if all {
			rest = rest[1:]
		}
		documents.Export(format, all, strings.Join(rest, " "))

	case "use", "database", "db":
		if len(args) < 1 {
			showPanel("databases", s.DbList)
			return
		}
		documents.SelectDatabase(args[0], nil)

	case "databases", "dbs":
		showPanel("databases", s.DbList)

	case "collection", "coll":
		if s.CurrentDB == "" {
			state.ShowError("Select a database first")
			return
		}
		if len(args) < 1 {
			showPanel("collections", s.CollList)
			return
		}
		documents.SelectCollection(args[0])

	case "collections", "colls":
		if s.CurrentDB == "" {
			state.ShowError("Select a database first")
			return
		}
		showPanel("collections", s.CollList)

	case "refresh":
		documents.Refresh()

	case "refresh!":
		documents.FullRefresh()

	case "watch":
		if len(args) < 1 {
			if s.LiveMode {
				documents.StopLiveMode()
			} else {
				documents.StartLiveMode()
			}
			return
		}
		dur, err := time.ParseDuration(args[0])
		if err != nil {
			state.ShowError(fmt.Sprintf("Invalid duration: %s (try: 2s, 5s, 1m)", args[0]))
			return
		}
		if dur < 500*time.Millisecond {
			state.ShowError("Minimum interval is 500ms")
			return
		}
		s.LiveInterval = dur
		if s.LiveMode {
			documents.StopLiveMode()
		}
		documents.StartLiveMode()

	default:
		state.ShowError(fmt.Sprintf("Unknown command: :%s", parts[0]))
	}
}

func getCommandSuggestions(partial string) []string {
	s := state.S
	lower := strings.ToLower(partial)

	commands := []string{
		":indexes", ":idx", ":stats", ":sort ", ":limit ", ":killop ",
		":export json", ":export csv", ":export json all", ":export csv all",
		":use ", ":profile", ":refresh", ":refresh!", ":watch ",
		":databases", ":dbs", ":database ", ":db ",
		":collections", ":colls", ":collection ", ":coll ",
	}
	var suggestions []string
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, ":"+lower) && cmd != ":"+partial {
			suggestions = append(suggestions, cmd)
		}
	}

	complete := func(prefixes []string, names []string) {
		for _, prefix := range prefixes {
			if !strings.HasPrefix(lower, prefix) {
				continue
			}
			p := strings.ToLower(partial[len(prefix):])
			for _, n := range names {
				if strings.HasPrefix(strings.ToLower(n), p) {
					suggestions = append(suggestions, ":"+prefix+n)
				}
			}
		}
	}
	var dbs, colls, ops []string
	for _, e := range s.AllDBItems {
		dbs = append(dbs, e.Name)
	}
	for _, e := range s.AllCollItems {
		colls = append(colls, e.Name)
	}
	for id := range s.StatsOps {
		ops = append(ops, id)
	}
	complete([]string{"use ", "database ", "db "}, dbs)
	complete([]string{"collection ", "coll "}, colls)
	complete([]string{"killop "}, ops)
	return suggestions
}
