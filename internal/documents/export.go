package documents

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tango/internal/state"
)

func sanitizeFilename(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "..", "_")
	return s
}

func defaultExportPath(format string) string {
	s := state.S
	base := "export"
	if s.DocsNS.DB != "" && s.DocsNS.Coll != "" {
		base = sanitizeFilename(s.DocsNS.DB) + "." + sanitizeFilename(s.DocsNS.Coll)
	}
	return fmt.Sprintf("%s-%s.%s", base, time.Now().Format("20060102-150405"), format)
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// ExportJSON exports the selected documents (or the current page) as a
// canonical Extended JSON array (importable with mongoimport --jsonArray).
func ExportJSON() { Export("json", false, "") }

// ExportCSV exports the selected documents (or the current page) as CSV.
func ExportCSV() { Export("csv", false, "") }

// Export writes documents to path (a timestamped file in the working
// directory when empty). With all, the full result of the current query is
// streamed from the server rather than just the loaded page.
func Export(format string, all bool, path string) {
	s := state.S
	if path == "" {
		path = defaultExportPath(format)
	}
	path = expandPath(path)

	if !all || s.ResultLabel != "" {
		docs := s.CurrentDocs
		scope := "current page"
		if s.ResultLabel != "" {
			scope = "result set"
		}
		if len(s.SelectedDocs) > 0 {
			docs = selectedDocsSlice()
			scope = "selection"
		}
		i := 0
		next := func() (bson.D, bool, error) {
			if i >= len(docs) {
				return nil, false, nil
			}
			i++
			return docs[i-1], true, nil
		}
		n, err := writeExport(format, path, next)
		if err != nil {
			state.ShowError(fmt.Sprintf("Export failed: %s", err))
			return
		}
		state.FlashStatus(fmt.Sprintf("Exported %d docs (%s) to %s", n, scope, path))
		return
	}

	if s.CurrentDB == "" || s.CurrentColl == "" {
		state.ShowError("Select a collection first")
		return
	}
	ns := state.Namespace{DB: s.CurrentDB, Coll: s.CurrentColl}
	opts := options.Find()
	if len(s.CurrentSort) > 0 {
		opts.SetSort(s.CurrentSort)
	}
	if len(s.CurrentProjection) > 0 {
		opts.SetProjection(s.CurrentProjection)
	}
	if s.QuerySkip > 0 {
		opts.SetSkip(s.QuerySkip)
	}
	if s.QueryLimit > 0 {
		opts.SetLimit(s.QueryLimit)
	}
	filter := s.CurrentFilter
	if filter == nil {
		filter = bson.D{}
	}
	coll := s.Client.Database(ns.DB).Collection(ns.Coll)
	tok := state.BeginLoading("Exporting " + ns.String())
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cur, err := coll.Find(ctx, filter, opts)
		var n int
		if err == nil {
			last := time.Now()
			next := func() (bson.D, bool, error) {
				if !cur.Next(ctx) {
					return nil, false, cur.Err()
				}
				var d bson.D
				if err := cur.Decode(&d); err != nil {
					return nil, false, err
				}
				n++
				if time.Since(last) > 500*time.Millisecond {
					last = time.Now()
					count := n
					state.Post(func() { state.UpdateLoading(tok, fmt.Sprintf("Exporting %s: %d docs", ns, count)) })
				}
				return d, true, nil
			}
			n, err = writeExport(format, path, next)
			_ = cur.Close(ctx)
		}
		state.Post(func() {
			state.EndLoading(tok)
			if err != nil {
				state.ShowError(fmt.Sprintf("Export failed after %d docs: %s", n, err))
				return
			}
			state.FlashStatus(fmt.Sprintf("Exported %d docs (all matching) to %s", n, path))
		})
	}()
}

func writeExport(format, path string, next func() (bson.D, bool, error)) (int, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return 0, err
	}
	var n int
	if format == "csv" {
		n, err = writeCSV(f, next)
	} else {
		n, err = writeJSON(f, next)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, err
}

func writeJSON(w io.Writer, next func() (bson.D, bool, error)) (int, error) {
	bw := bufio.NewWriter(w)
	n := 0
	if _, err := bw.WriteString("["); err != nil {
		return 0, err
	}
	for {
		d, ok, err := next()
		if err != nil {
			return n, err
		}
		if !ok {
			break
		}
		b, err := bson.MarshalExtJSON(d, true, false)
		if err != nil {
			return n, err
		}
		if n > 0 {
			_, _ = bw.WriteString(",")
		}
		_, _ = bw.WriteString("\n")
		_, _ = bw.Write(b)
		n++
	}
	if _, err := bw.WriteString("\n]\n"); err != nil {
		return n, err
	}
	return n, bw.Flush()
}

// writeCSV spools flattened rows to a temp file while discovering columns,
// then writes the CSV, so memory use doesn't grow with the export size.
func writeCSV(w io.Writer, next func() (bson.D, bool, error)) (int, error) {
	spool, err := os.CreateTemp("", "tango-csv-*.jsonl")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.Remove(spool.Name()); _ = spool.Close() }()
	bw := bufio.NewWriter(spool)
	enc := json.NewEncoder(bw)
	colIdx := map[string]int{}
	var columns []string
	n := 0
	for {
		d, ok, err := next()
		if err != nil {
			return n, err
		}
		if !ok {
			break
		}
		keys, vals := flattenDoc("", d)
		for _, k := range keys {
			if _, seen := colIdx[k]; !seen {
				colIdx[k] = len(columns)
				columns = append(columns, k)
			}
		}
		if err := enc.Encode(vals); err != nil {
			return n, err
		}
		n++
	}
	if err := bw.Flush(); err != nil {
		return n, err
	}
	return n, writeCSVFromSpool(w, spool.Name(), columns, n)
}

func writeCSVFromSpool(w io.Writer, spoolPath string, columns []string, n int) error {
	// _id first, then columns in order of first appearance.
	ordered := make([]string, 0, len(columns))
	for _, c := range columns {
		if c == "_id" {
			ordered = append(ordered, c)
		}
	}
	for _, c := range columns {
		if c != "_id" {
			ordered = append(ordered, c)
		}
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(ordered); err != nil {
		return err
	}
	f, err := os.Open(spoolPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(bufio.NewReader(f))
	row := make([]string, len(ordered))
	for i := 0; i < n; i++ {
		var vals map[string]string
		if err := dec.Decode(&vals); err != nil {
			return err
		}
		for j, c := range ordered {
			row[j] = vals[c]
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// flattenDoc flattens nested documents into dotted keys, returning keys in
// document order and their CSV cell values.
func flattenDoc(prefix string, doc bson.D) ([]string, map[string]string) {
	keys := []string{}
	vals := map[string]string{}
	var walk func(prefix string, d bson.D)
	walk = func(prefix string, d bson.D) {
		for _, e := range d {
			key := prefix + e.Key
			switch v := e.Value.(type) {
			case bson.D:
				walk(key+".", v)
				continue
			case bson.M:
				walk(key+".", mapToSortedD(v))
				continue
			}
			keys = append(keys, key)
			vals[key] = csvValue(e.Value)
		}
	}
	walk(prefix, doc)
	return keys, vals
}

func mapToSortedD(m bson.M) bson.D {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	d := make(bson.D, 0, len(m))
	for _, k := range keys {
		d = append(d, bson.E{Key: k, Value: m[k]})
	}
	return d
}

func csvValue(v interface{}) string {
	switch x := v.(type) {
	case nil, primitive.Null:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case primitive.ObjectID:
		return x.Hex()
	case primitive.DateTime:
		return x.Time().UTC().Format("2006-01-02T15:04:05.000Z")
	case primitive.Decimal128:
		return x.String()
	}
	b, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: v}}, false, false)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return strings.TrimSuffix(strings.TrimPrefix(string(b), `{"v":`), "}")
}
