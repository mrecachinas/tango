package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"tango/internal/documents"
	"tango/internal/state"
)

const maxResultCap = 10000

func ExecuteQuery(queryStr string) {
	s := state.S
	queryStr = strings.TrimSpace(queryStr)

	if queryStr == "" {
		s.CurrentFilter = nil
		s.SnapshotID = nil
		s.CurrentPage = 0
		s.CountDirty = true
		if s.CurrentDB != "" && s.CurrentColl != "" {
			documents.LoadDocuments()
		}
		return
	}

	state.AddToQueryHistory(queryStr)
	s.QueryHistoryIdx = -1

	// Command mode
	if strings.HasPrefix(queryStr, ":") {
		executeCommand(queryStr[1:])
		s.SearchBar.SetText("")
		state.RefreshPanelBorders()
		return
	}

	// Mongo shell syntax: db.collection.method(...)
	if strings.HasPrefix(queryStr, "db.") {
		runShellQuery(queryStr)
		return
	}

	// Bare JSON filter on current collection
	if s.CurrentDB == "" || s.CurrentColl == "" {
		state.ShowError("Select a database and collection first,\nor use db.collection.find({...})")
		return
	}

	filter, err := parseFilter(queryStr)
	if err != nil {
		state.ShowError(fmt.Sprintf("Invalid filter: %s\n\nTry: db.collection.find({key: \"value\"})", err))
		return
	}

	s.CurrentFilter = filter
	s.SnapshotID = nil
	s.CurrentPage = 0
	s.CountDirty = true
	documents.LoadDocuments()
}

func runShellQuery(query string) {
	s := state.S
	state.ShowLoading("Executing query")
	defer state.DoneLoading()
	if s.CurrentDB == "" {
		state.ShowError("Select a database first.")
		return
	}

	collection, method, argsStr, err := parseMongoQuery(query)
	if err != nil {
		state.ShowError(err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.QueryTimeout*3)
	defer cancel()

	coll := s.Client.Database(s.CurrentDB).Collection(collection)

	switch method {
	case "find":
		filter, err := parseFilter(argsStr)
		if err != nil {
			state.ShowError(fmt.Sprintf("Invalid filter: %s", err))
			return
		}
		// Use the normal paginated browse path
		s.CurrentColl = collection
		s.CurrentFilter = filter
		s.SnapshotID = nil
		s.CurrentPage = 0
		s.CountDirty = true
		documents.SelectCollectionInList(collection)
		documents.LoadDocuments()

	case "findOne":
		filter, err := parseFilter(argsStr)
		if err != nil {
			state.ShowError(fmt.Sprintf("Invalid filter: %s", err))
			return
		}
		var result bson.M
		if err := coll.FindOne(ctx, filter).Decode(&result); err != nil {
			if err == mongo.ErrNoDocuments {
				state.ShowError("No matching document found.")
			} else {
				state.ShowError(fmt.Sprintf("findOne error: %s", err))
			}
			return
		}
		s.CurrentDocs = []bson.M{result}
		prettyBytes, _ := json.MarshalIndent(result, "", "  ")
		s.DocDetail.SetText(documents.HighlightJSON(string(prettyBytes)))
		s.DocDetail.SetTitle(documents.DocTitle(result, "findOne"))
		s.DocDetail.ScrollToBeginning()
		s.DocPages.SwitchToPage("detail")
		s.App.SetFocus(s.DocDetail)

	case "count", "countDocuments":
		filter, err := parseFilter(argsStr)
		if err != nil {
			state.ShowError(fmt.Sprintf("Invalid filter: %s", err))
			return
		}
		count, err := coll.CountDocuments(ctx, filter)
		if err != nil {
			state.ShowError(fmt.Sprintf("count error: %s", err))
			return
		}
		s.DocDetail.SetText(fmt.Sprintf("%d", count))
		s.DocDetail.SetTitle(fmt.Sprintf(" count [yellow](%s.%s)[white] ", s.CurrentDB, collection))
		s.DocDetail.ScrollToBeginning()
		s.DocPages.SwitchToPage("detail")
		s.App.SetFocus(s.DocDetail)

	case "aggregate":
		strict := relaxedJSONToStrict(argsStr)
		var pipeline []bson.M
		if err := json.Unmarshal([]byte(strict), &pipeline); err != nil {
			state.ShowError(fmt.Sprintf("Invalid pipeline: %s", err))
			return
		}
		// Inject a $limit stage if the user hasn't already included one
		hasLimit := false
		for _, stage := range pipeline {
			if _, ok := stage["$limit"]; ok {
				hasLimit = true
				break
			}
		}
		if !hasLimit {
			pipeline = append(pipeline, bson.M{"$limit": int64(maxResultCap)})
		}
		cursor, err := coll.Aggregate(ctx, pipeline)
		if err != nil {
			state.ShowError(fmt.Sprintf("aggregate error: %s", err))
			return
		}
		defer func() { _ = cursor.Close(ctx) }()
		var docs []bson.M
		if err := cursor.All(ctx, &docs); err != nil {
			state.ShowError(fmt.Sprintf("aggregate decode error: %s", err))
			return
		}
		title := fmt.Sprintf("aggregate(%s.%s)", s.CurrentDB, collection)
		if len(docs) >= maxResultCap {
			title += " ⚠ capped at 10,000"
		}
		showResultDocs(docs, title)

	case "distinct":
		// Expect: "fieldName" or "fieldName", {filter}
		parts := strings.SplitN(argsStr, ",", 2)
		fieldStr := strings.TrimSpace(parts[0])
		fieldStr = strings.Trim(fieldStr, "\"'")
		filter := bson.M{}
		if len(parts) > 1 {
			f, err := parseFilter(parts[1])
			if err != nil {
				state.ShowError(fmt.Sprintf("Invalid filter: %s", err))
				return
			}
			filter = f
		}
		values, err := coll.Distinct(ctx, fieldStr, filter)
		if err != nil {
			state.ShowError(fmt.Sprintf("distinct error: %s", err))
			return
		}
		warning := ""
		if len(values) > maxResultCap {
			values = values[:maxResultCap]
			warning = "\n\n[yellow]⚠ Results capped at 10,000 (total exceeds limit)[white]"
		}
		jsonBytes, _ := json.MarshalIndent(values, "", "  ")
		s.DocDetail.SetText(string(jsonBytes) + warning)
		s.DocDetail.SetTitle(fmt.Sprintf(" distinct [yellow](%s.%s.%s)[white] ", s.CurrentDB, collection, fieldStr))
		s.DocDetail.ScrollToBeginning()
		s.DocPages.SwitchToPage("detail")
		s.App.SetFocus(s.DocDetail)

	default:
		state.ShowError(fmt.Sprintf("Unsupported method: %s\n\nSupported: find, findOne, count, countDocuments, aggregate, distinct", method))
	}
}

func showResultDocs(docs []bson.M, title string) {
	s := state.S
	if len(docs) == 0 {
		state.ShowError("(no results)")
		return
	}
	s.CurrentDocs = docs
	s.CurrentPage = 0
	s.TotalDocs = int64(len(docs))

	s.DocList.Clear()
	for i, doc := range docs {
		jsonBytes, err := json.Marshal(doc)
		if err != nil {
			s.DocList.AddItem(fmt.Sprintf("[red]Error marshalling record %d", i), "", 0, nil)
			continue
		}
		s.DocList.AddItem(string(jsonBytes), "", 0, nil)
	}

	s.DocPages.SwitchToPage("list")
	s.DocList.SetTitle(fmt.Sprintf(" Results [green](%d)[white] %s ", len(docs), title))
	s.App.SetFocus(s.DocList)
}

func GetAutocompleteSuggestions(currentText string) []string {
	s := state.S
	text := strings.TrimSpace(currentText)

	// Command mode autocomplete
	if strings.HasPrefix(text, ":") {
		return getCommandSuggestions(text[1:])
	}

	// Don't suggest once we're inside parentheses
	if strings.Contains(text, "(") {
		return nil
	}

	// Suggest "db." prefix
	if text == "" || (len(text) < 3 && strings.HasPrefix("db.", text)) {
		return []string{"db."}
	}

	if !strings.HasPrefix(text, "db.") {
		return nil
	}

	rest := text[3:] // after "db."

	if strings.Contains(rest, ".") {
		// db.collName.partialMethod → suggest methods
		dotIdx := strings.LastIndex(rest, ".")
		collName := rest[:dotIdx]
		partial := rest[dotIdx+1:]

		methods := []string{"find(", "findOne(", "count()", "countDocuments()", "aggregate([", "distinct(", "drop()"}
		var suggestions []string
		for _, m := range methods {
			if strings.HasPrefix(m, partial) {
				suggestions = append(suggestions, "db."+collName+"."+m)
			}
		}
		return suggestions
	}

	// db.partialCollName → suggest collection names
	var suggestions []string
	for _, c := range s.CachedCollNames {
		if strings.HasPrefix(c, rest) {
			suggestions = append(suggestions, "db."+c+".")
		}
	}
	return suggestions
}

func executeCommand(cmd string) {
	s := state.S
	cmd = strings.TrimSpace(cmd)
	parts := strings.Fields(strings.ToLower(cmd))
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "indexes", "idx":
		if s.CurrentDB == "" || s.CurrentColl == "" {
			state.ShowError("Select a collection first")
			return
		}
		documents.ShowIndexes()

	case "stats":
		documents.ShowStats()

	case "sort":
		if len(parts) < 2 {
			s.CurrentSortField = ""
			s.CurrentSortOrder = 0
			s.CurrentPage = 0
			documents.LoadDocuments()
			return
		}
		field := parts[1]
		s.CurrentSortOrder = 1
		if strings.HasPrefix(field, "-") {
			field = field[1:]
			s.CurrentSortOrder = -1
		}
		s.CurrentSortField = field
		s.CurrentPage = 0
		documents.LoadDocuments()

	case "limit":
		if len(parts) < 2 {
			state.ShowError(fmt.Sprintf("Current limit: %d", state.PageSize))
			return
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil || n < 1 || n > 10000 {
			state.ShowError("Invalid limit (1-10000)")
			return
		}
		state.PageSize = n
		s.CurrentPage = 0
		documents.LoadDocuments()

	case "export":
		if len(parts) < 2 {
			state.ShowError("Usage: :export json | :export csv")
			return
		}
		switch parts[1] {
		case "json":
			documents.ExportJSON()
		case "csv":
			documents.ExportCSV()
		default:
			state.ShowError("Usage: :export json | :export csv")
		}

	case "use":
		// Preserve original case for database name
		rawParts := strings.Fields(strings.TrimSpace(cmd))
		if len(rawParts) < 2 {
			state.ShowError("Usage: :use <database>")
			return
		}
		documents.SelectDatabase(rawParts[1])

	case "profile":
		documents.ShowStats()

	case "refresh":
		documents.Refresh()

	case "refresh!":
		documents.FullRefresh()

	case "watch":
		if len(parts) < 2 {
			if s.LiveMode {
				documents.StopLiveMode()
			} else {
				documents.StartLiveMode()
			}
			return
		}
		// Preserve original case for duration string
		rawParts := strings.Fields(strings.TrimSpace(cmd))
		dur, err := time.ParseDuration(rawParts[1])
		if err != nil {
			state.ShowError(fmt.Sprintf("Invalid duration: %s (try: 2s, 5s, 1m)", rawParts[1]))
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
	partial = strings.ToLower(partial)

	commands := []string{
		":indexes", ":idx", ":stats", ":sort ", ":limit ",
		":export json", ":export csv", ":use ", ":profile",
		":refresh", ":refresh!", ":watch ",
	}
	var suggestions []string
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, ":"+partial) {
			suggestions = append(suggestions, cmd)
		}
	}

	// For ":use ", also suggest database names
	if strings.HasPrefix(partial, "use ") {
		dbPartial := strings.TrimPrefix(partial, "use ")
		for i := 0; i < s.DbList.GetItemCount(); i++ {
			main, _ := s.DbList.GetItemText(i)
			name := state.BareName(main)
			if strings.HasPrefix(strings.ToLower(name), dbPartial) {
				suggestions = append(suggestions, ":use "+name)
			}
		}
	}

	return suggestions
}
