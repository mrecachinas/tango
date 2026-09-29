// Package e2e drives the real tango UI on a simulated terminal against a live
// MongoDB. It is skipped unless TANGO_E2E_MONGO_URI is set, e.g.
//
//	docker run -d --rm -p 27017:27017 mongo:7 --replSet rs0
//	docker exec <id> mongosh --eval 'rs.initiate()'
//	TANGO_E2E_MONGO_URI='mongodb://localhost:27017/?directConnection=true' go test -race ./internal/e2e/
package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"tango/internal/config"
	"tango/internal/db"
	"tango/internal/documents"
	"tango/internal/metrics"
	"tango/internal/mjson"
	"tango/internal/query"
	"tango/internal/state"
	"tango/internal/ui"
)

type env struct {
	keys   atomic.Int64 // key events seen by the app
	sent   int64
	t      *testing.T
	sim    tcell.SimulationScreen
	client *mongo.Client
	dbName string
	db     *mongo.Database
}

func (e *env) onUI(f func()) {
	e.t.Helper()
	done := make(chan struct{})
	state.S.App.QueueUpdateDraw(func() { f(); close(done) })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		e.t.Fatal("UI goroutine did not respond")
	}
}

func (e *env) waitFor(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var ok bool
		e.onUI(func() { ok = cond() })
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for %s", what)
}

// idle waits until no loads are in flight.
func (e *env) idle() {
	e.t.Helper()
	time.Sleep(50 * time.Millisecond)
	e.waitFor("idle", func() bool { return !state.S.DocsLoading && !state.S.Counting })
}

// view is a copy of app state taken on the UI goroutine.
type view struct {
	CurrentDocs                        []bson.D
	TotalDocs                          int64
	TotalKnown, TotalExact, KeysetMode bool
	CurrentPage                        int
	DocsNS                             state.Namespace
	CurrentColl                        string
	CurrentSort                        bson.D
	Metrics                            string
}

func (e *env) snap() view {
	var v view
	e.onUI(func() {
		s := state.S
		v = view{
			CurrentDocs: append([]bson.D(nil), s.CurrentDocs...), TotalDocs: s.TotalDocs,
			TotalKnown: s.TotalKnown, TotalExact: s.TotalExact, KeysetMode: s.KeysetMode,
			CurrentPage: s.CurrentPage, DocsNS: s.DocsNS, CurrentColl: s.CurrentColl,
			CurrentSort: s.CurrentSort, Metrics: s.MetricsBar.GetText(true),
		}
	})
	return v
}

func (e *env) screen() string {
	e.onUI(func() {})
	cells, w, h := e.sim.GetContents()
	var lines []string
	for y := 0; y < h; y++ {
		var b strings.Builder
		for x := 0; x < w; x++ {
			if c := cells[y*w+x]; len(c.Runes) > 0 {
				b.WriteRune(c.Runes[0])
			} else {
				b.WriteByte(' ')
			}
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}

// key injects a key and waits until the app has processed it.
func (e *env) key(k tcell.Key, r rune) {
	e.t.Helper()
	e.sent++
	e.sim.InjectKey(k, r, 0)
	deadline := time.Now().Add(10 * time.Second)
	for e.keys.Load() < e.sent {
		if time.Now().After(deadline) {
			e.t.Fatal("key event not processed")
		}
		time.Sleep(time.Millisecond)
	}
	e.onUI(func() {})
}

func (e *env) typeText(s string) {
	for _, r := range s {
		e.key(tcell.KeyRune, r)
	}
}

func (e *env) openColl(name string) {
	e.t.Helper()
	e.onUI(func() { documents.SelectCollection(name) })
	e.waitFor("collection "+name, func() bool {
		return state.S.DocsNS.Coll == name && !state.S.DocsLoading && state.S.ResultLabel == ""
	})
	e.idle()
}

func (e *env) query(q string) {
	e.t.Helper()
	e.onUI(func() { query.ExecuteQuery(q) })
	e.idle()
}

func (e *env) count(coll string, filter bson.D) int64 {
	e.t.Helper()
	n, err := e.db.Collection(coll).CountDocuments(context.Background(), filter)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func setup(t *testing.T) *env {
	uri := os.Getenv("TANGO_E2E_MONGO_URI")
	if uri == "" {
		t.Skip("set TANGO_E2E_MONGO_URI to run end-to-end tests")
	}
	cfg := config.Config{URI: uri}
	client, label, err := db.Connect(cfg, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, client: client, dbName: fmt.Sprintf("tango_e2e_%d", time.Now().UnixNano())}
	e.db = client.Database(e.dbName)
	t.Cleanup(func() {
		_ = e.db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	e.seed()

	privs, _ := db.DetectPrivileges(client)
	state.S = &state.AppState{
		Client: client, GlobalCfg: cfg, HostLabel: label,
		QueryTimeout: 10 * time.Second, RefreshInterval: 500 * time.Millisecond,
		SelectedDocs: map[int]bool{}, QueryHistoryIdx: -1,
		Privileges: privs, ReadOnly: !privs.AnyWrite(), NoKeyset: map[state.Namespace]bool{},
	}
	e.sim = tcell.NewSimulationScreen("UTF-8")
	if err := e.sim.Init(); err != nil {
		t.Fatal(err)
	}
	state.S.App = tview.NewApplication().SetScreen(e.sim)
	ui.BuildUI()
	capture := state.S.App.GetInputCapture()
	state.S.App.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		defer e.keys.Add(1)
		return capture(ev)
	})
	go func() { _ = state.S.App.Run() }()
	t.Cleanup(func() { state.S.App.Stop() })
	time.Sleep(100 * time.Millisecond)
	// Run re-initialises the screen, so resize afterwards.
	e.sim.SetSize(320, 50)
	_ = e.sim.PostEvent(tcell.NewEventResize(320, 50))

	e.onUI(func() { documents.LoadDatabases(nil) })
	metrics.Start(500 * time.Millisecond)
	e.waitFor("database list", func() bool {
		for _, n := range state.S.DbVisible {
			if n == e.dbName {
				return true
			}
		}
		return false
	})
	e.onUI(func() { documents.SelectDatabase(e.dbName, nil) })
	e.waitFor("collections", func() bool { return len(state.S.AllCollItems) >= 7 })
	return e
}

func (e *env) seed() {
	ctx := context.Background()
	ins := func(coll string, docs ...interface{}) {
		if _, err := e.db.Collection(coll).InsertMany(ctx, docs); err != nil {
			e.t.Fatal(err)
		}
	}
	for i := int32(1); i <= 5; i++ {
		ins("a", bson.D{{Key: "_id", Value: i}, {Key: "src", Value: "a"}})
		ins("b", bson.D{{Key: "_id", Value: i}, {Key: "src", Value: "b"}})
	}
	ins("items", bson.D{
		{Key: "_id", Value: primitive.NewObjectID()},
		{Key: "n", Value: int32(0)},
		{Key: "big", Value: int64(9007199254740993)},
		{Key: "when", Value: primitive.NewDateTimeFromTime(time.Date(2024, 1, 2, 3, 4, 5, 6e6, time.UTC))},
		{Key: "ref", Value: mustOID("65f000000000000000000000")},
		{Key: "tags", Value: bson.A{"x"}},
		{Key: "label", Value: "[red]hi"},
		{Key: "z", Value: int32(1)},
		{Key: "a", Value: 2.5},
	})
	for i := int32(0); i < 20; i++ {
		ins("comp", bson.D{{Key: "_id", Value: bson.D{{Key: "a", Value: i}, {Key: "b", Value: i}, {Key: "c", Value: i}}}, {Key: "v", Value: i}})
	}
	var many []interface{}
	for i := 0; i < 20000; i++ {
		many = append(many, bson.D{{Key: "i", Value: int32(i)}})
	}
	ins("big", many...)
	var mixed []interface{}
	for i := int32(0); i < 40; i++ {
		mixed = append(mixed,
			bson.D{{Key: "_id", Value: i}, {Key: "v", Value: i}},
			bson.D{{Key: "_id", Value: fmt.Sprintf("s%02d", i)}, {Key: "v", Value: i}},
			bson.D{{Key: "_id", Value: primitive.NewObjectID()}, {Key: "v", Value: i}})
	}
	ins("mixed", mixed...)
	ins("live", bson.D{{Key: "_id", Value: primitive.NewObjectID()}, {Key: "k", Value: int32(1)}})
}

func mustOID(h string) primitive.ObjectID {
	o, _ := primitive.ObjectIDFromHex(h)
	return o
}

func TestEndToEnd(t *testing.T) {
	e := setup(t)
	s := state.S

	t.Run("collection list has no duplicates", func(t *testing.T) {
		var before, after int
		e.onUI(func() { before = s.CollList.GetItemCount() })
		e.onUI(func() { documents.SelectDatabase(e.dbName, nil) })
		e.waitFor("collections", func() bool { return len(s.AllCollItems) == before })
		e.onUI(func() { documents.Refresh() })
		e.idle()
		e.waitFor("collections", func() bool { return len(s.AllCollItems) == before })
		e.onUI(func() { after = s.CollList.GetItemCount() })
		if after != before {
			t.Errorf("collection list grew from %d to %d", before, after)
		}
	})

	t.Run("_id filters are not clobbered by the snapshot", func(t *testing.T) {
		e.openColl("a")
		e.query(`{_id: 3}`)
		v := e.snap()
		if len(v.CurrentDocs) != 1 || v.TotalDocs != 1 {
			t.Errorf("{_id: 3}: %d docs, total %d", len(v.CurrentDocs), v.TotalDocs)
		}
		e.query(`{_id: {$in: [1, 5]}}`)
		v = e.snap()
		if len(v.CurrentDocs) != 2 || v.TotalDocs != 2 {
			t.Errorf("$in: %d docs, total %d", len(v.CurrentDocs), v.TotalDocs)
		}
		e.query("")
	})

	t.Run("shell helpers in filters", func(t *testing.T) {
		e.openColl("items")
		e.query(`{ref: ObjectId("65f000000000000000000000"), when: {$gte: ISODate("2024-01-01")}, big: NumberLong("9007199254740993")}`)
		v := e.snap()
		if len(v.CurrentDocs) != 1 {
			t.Errorf("got %d docs", len(v.CurrentDocs))
		}
		e.query("")
	})

	t.Run(":sort keeps field case", func(t *testing.T) {
		e.openColl("a")
		e.query(":sort -createdAt src")
		v := e.snap()
		if got := state.FormatSort(v.CurrentSort); got != "createdAt↓ src↑" {
			t.Errorf("sort = %q", got)
		}
		e.query(":sort")
	})

	t.Run("findOne then delete targets the queried collection", func(t *testing.T) {
		e.openColl("a")
		e.query(`db.b.findOne({_id: 1})`)
		e.waitFor("findOne", func() bool { return s.ResultLabel != "" })
		e.onUI(func() { documents.CollapseDocument(); documents.DeleteDocumentConfirm() })
		e.key(tcell.KeyEnter, 0)
		e.waitFor("delete", func() bool { return len(s.CurrentDocs) == 0 })
		if e.count("a", bson.D{{Key: "_id", Value: 1}}) != 1 || e.count("b", bson.D{{Key: "_id", Value: 1}}) != 0 {
			t.Error("delete hit the wrong collection")
		}
	})

	t.Run("aggregate results and projections are not writable", func(t *testing.T) {
		e.openColl("a")
		e.query(`db.a.aggregate([{$group: {_id: "$src", n: {$sum: 1}}}])`)
		e.waitFor("aggregate", func() bool { return s.ResultLabel != "" })
		e.onUI(func() { documents.DeleteDocumentConfirm() })
		var front string
		e.onUI(func() { front, _ = s.RootPages.GetFrontPage() })
		if front == "confirm" {
			t.Error("delete prompt offered for aggregate output")
		}
		e.query(`db.a.find({}, {src: 1})`)
		e.waitFor("projected find", func() bool { return len(s.CurrentDocs) > 0 && !s.DocsEditable })
	})

	t.Run("edit round-trips types exactly", func(t *testing.T) {
		e.openColl("items")
		var before bson.Raw
		_ = e.db.Collection("items").FindOne(context.Background(), bson.D{}).Decode(&before)

		t.Setenv("VISUAL", "true") // no-op editor
		e.onUI(func() { documents.ExpandDocument(0); documents.EditDocument() })
		e.idle()
		var after bson.Raw
		_ = e.db.Collection("items").FindOne(context.Background(), bson.D{}).Decode(&after)
		if !bytesEqual(before, after) {
			t.Errorf("no-op edit changed the document:\n%s\n%s", before, after)
		}

		script := filepath.Join(t.TempDir(), "ed.sh")
		_ = os.WriteFile(script, []byte("#!/bin/sh\nsed -i.bak 's/\"z\": 1,/\"z\": 2,/' \"$1\"\n"), 0700)
		t.Setenv("VISUAL", script)
		e.onUI(func() { documents.EditDocument() })
		e.waitFor("saved", func() bool {
			v, _ := mjson.Get(s.ExpandedDoc, "z")
			return v == int32(2)
		})
		var got bson.D
		_ = e.db.Collection("items").FindOne(context.Background(), bson.D{}).Decode(&got)
		want := []string{"_id:primitive.ObjectID", "n:int32", "big:int64", "when:primitive.DateTime", "ref:primitive.ObjectID", "tags:primitive.A", "label:string", "z:int32", "a:float64"}
		var types []string
		for _, el := range got {
			types = append(types, fmt.Sprintf("%s:%T", el.Key, el.Value))
		}
		if strings.Join(types, " ") != strings.Join(want, " ") {
			t.Errorf("types/order after edit:\n got %v\nwant %v", types, want)
		}
		if v, _ := mjson.Get(got, "big"); v != int64(9007199254740993) {
			t.Errorf("int64 precision lost: %v", v)
		}
		e.onUI(func() { documents.CollapseDocument() })
	})

	t.Run("document text is not interpreted as markup", func(t *testing.T) {
		e.openColl("items")
		e.onUI(func() { ui.ToggleWideMode() })
		defer e.onUI(func() { ui.ToggleWideMode() })
		scr := e.screen()
		if !strings.Contains(scr, `"tags": ["x"]`) || !strings.Contains(scr, `"label": "[red]hi"`) {
			t.Errorf("list row rendered incorrectly:\n%s", scr)
		}
	})

	t.Run("doc search keeps syntax highlighting", func(t *testing.T) {
		e.openColl("items")
		e.onUI(func() { documents.ExpandDocument(0) })
		e.key(tcell.KeyRune, '/')
		e.typeText("hi")
		var label, text string
		e.onUI(func() { label = s.DocSearchBar.GetLabel() })
		if !strings.Contains(label, "(1)") {
			t.Errorf("search label = %q", label)
		}
		e.key(tcell.KeyEscape, 0)
		e.onUI(func() { text = s.DocDetail.GetText(false) })
		if !strings.Contains(text, "[aqua:-]") {
			t.Error("highlighting lost after search")
		}
		e.onUI(func() { documents.CollapseDocument() })
	})

	t.Run("sort form receives typed keys", func(t *testing.T) {
		e.openColl("a")
		e.onUI(func() { documents.PromptSort() })
		e.typeText("status q")
		var got string
		// onUI would time out if 'q' had stopped the application.
		e.onUI(func() { got = s.SortForm.GetFormItemByLabel("Sort field").(*tview.InputField).GetText() })
		if got != "status q" {
			t.Errorf("sort field = %q", got)
		}
		e.key(tcell.KeyEscape, 0)
	})

	t.Run("compound _id deletes and modal buttons", func(t *testing.T) {
		e.openColl("comp")
		for i := 0; i < 10; i++ {
			e.onUI(func() { s.DocList.SetCurrentItem(0); documents.DeleteDocumentConfirm() })
			if i == 3 {
				if n := strings.Count(e.screen(), "Cancel"); n != 1 {
					t.Errorf("confirm modal shows %d Cancel buttons, want 1", n)
				}
			}
			e.key(tcell.KeyEnter, 0)
			e.idle()
		}
		if n := e.count("comp", bson.D{}); n != 10 {
			t.Errorf("remaining = %d, want 10", n)
		}
	})

	t.Run("delete targets the prompted document even if the page reloads", func(t *testing.T) {
		e.openColl("a")
		var prompted interface{}
		e.onUI(func() {
			s.DocList.SetCurrentItem(0)
			prompted, _ = mjson.Get(s.CurrentDocs[0], "_id")
			documents.DeleteDocumentConfirm()
		})
		_, _ = e.db.Collection("a").InsertOne(context.Background(), bson.D{{Key: "_id", Value: int32(0)}})
		e.onUI(func() { documents.ReloadDocuments() })
		e.idle()
		e.key(tcell.KeyEnter, 0)
		e.idle()
		if e.count("a", bson.D{{Key: "_id", Value: prompted}}) != 0 || e.count("a", bson.D{{Key: "_id", Value: 0}}) != 1 {
			t.Error("deleted a different document than the one prompted")
		}
	})

	t.Run("live mode shows inserts without stealing focus", func(t *testing.T) {
		e.openColl("live")
		e.onUI(func() { s.LiveInterval = 300 * time.Millisecond; documents.StartLiveMode() })
		e.idle()
		e.onUI(func() { documents.ExpandDocument(0) })
		id := primitive.NewObjectID()
		_, _ = e.db.Collection("live").InsertOne(context.Background(), bson.D{{Key: "_id", Value: id}})
		e.waitFor("insert visible", func() bool {
			if len(s.CurrentDocs) == 0 {
				return false
			}
			v, _ := mjson.Get(s.CurrentDocs[0], "_id")
			return v == id
		})
		var page string
		e.onUI(func() { page, _ = s.DocPages.GetFrontPage() })
		if page != "detail" {
			t.Errorf("live reload switched view to %q", page)
		}
		e.onUI(func() { documents.StopLiveMode() })
		e.idle()
	})

	t.Run("aggregate with $out", func(t *testing.T) {
		e.openColl("b")
		e.query(`db.b.aggregate([{$match: {}}, {$out: "b_copy"}])`)
		e.waitFor("aggregate", func() bool { return strings.Contains(state.S.DocDetail.GetText(true), "b_copy") })
		if n := e.count("b_copy", bson.D{}); n == 0 {
			t.Error("$out wrote nothing")
		}
	})

	t.Run("stale responses are discarded", func(t *testing.T) {
		e.onUI(func() { documents.SelectCollection("big"); documents.SelectCollection("a") })
		e.idle()
		time.Sleep(300 * time.Millisecond)
		e.idle()
		v := e.snap()
		if v.DocsNS.Coll != "a" || v.CurrentColl != "a" {
			t.Errorf("showing %s while current is %s", v.DocsNS.Coll, v.CurrentColl)
		}
		for _, d := range v.CurrentDocs {
			if _, ok := mjson.Get(d, "i"); ok {
				t.Fatal("documents from big shown under a")
			}
		}
	})

	t.Run("keyset paging and estimated counts", func(t *testing.T) {
		e.openColl("big")
		v := e.snap()
		if !v.KeysetMode || !v.TotalKnown || v.TotalExact || v.TotalDocs != 20000 {
			t.Errorf("keyset=%v known=%v exact=%v total=%d", v.KeysetMode, v.TotalKnown, v.TotalExact, v.TotalDocs)
		}
		seen := map[int32]bool{}
		for p := 0; p < 3; p++ {
			v := e.snap()
			for _, d := range v.CurrentDocs {
				i, _ := mjson.Get(d, "i")
				if seen[i.(int32)] {
					t.Fatalf("doc %d repeated across pages", i)
				}
				seen[i.(int32)] = true
			}
			e.onUI(func() { documents.NextPage() })
			e.idle()
		}
		if len(seen) != 3*state.PageSize {
			t.Errorf("saw %d distinct docs over 3 pages", len(seen))
		}
		e.onUI(func() { documents.PrevPage() })
		e.idle()
		v = e.snap()
		if v.CurrentPage != 2 {
			t.Errorf("page = %d", v.CurrentPage)
		}
	})

	t.Run("skip paging over mixed _id types sees every document", func(t *testing.T) {
		e.openColl("mixed")
		e.query(`{v: {$gte: 0}}`)
		seen := map[string]bool{}
		for p := 0; p < 4; p++ {
			for _, d := range e.snap().CurrentDocs {
				id, _ := mjson.Get(d, "_id")
				seen[mjson.Compact(id)] = true
			}
			e.onUI(func() { documents.NextPage() })
			e.idle()
		}
		if len(seen) != 120 {
			t.Errorf("saw %d of 120 documents", len(seen))
		}
		if v := e.snap(); v.KeysetMode || v.TotalDocs != 120 {
			t.Errorf("keyset=%v total=%d", v.KeysetMode, v.TotalDocs)
		}
		e.query("")
	})

	t.Run("projection applies on the keyset path", func(t *testing.T) {
		e.openColl("big")
		e.query(`db.big.find({}, {i: 0})`)
		v := e.snap()
		if !v.KeysetMode || len(v.CurrentDocs) == 0 {
			t.Fatalf("keyset=%v docs=%d", v.KeysetMode, len(v.CurrentDocs))
		}
		if _, ok := mjson.Get(v.CurrentDocs[0], "i"); ok {
			t.Error("projected-out field was returned")
		}
		e.query("")
	})

	t.Run("a previous query's count doesn't land on the next view", func(t *testing.T) {
		e.openColl("big")
		e.onUI(func() {
			query.ExecuteQuery(`{i: {$gte: 0}}`)
			documents.SelectCollection("a")
		})
		e.idle()
		time.Sleep(500 * time.Millisecond)
		want, _ := e.db.Collection("a").EstimatedDocumentCount(context.Background())
		if v := e.snap(); v.DocsNS.Coll != "a" || v.TotalDocs != want || v.TotalExact {
			t.Errorf("view %s total=%d exact=%v, want a total=%d estimated", v.DocsNS.Coll, v.TotalDocs, v.TotalExact, want)
		}
	})

	t.Run("viewing indexes doesn't change the browsed collection", func(t *testing.T) {
		e.openColl("a")
		e.onUI(func() {
			s.App.SetFocus(s.CollList)
			documents.SelectCollectionInList("b")
		})
		e.key(tcell.KeyRune, 'I')
		e.waitFor("indexes", func() bool { return s.IndexesNS.Coll == "b" })
		if v := e.snap(); v.CurrentColl != "a" || v.DocsNS.Coll != "a" {
			t.Errorf("current=%s docs=%s, want a", v.CurrentColl, v.DocsNS.Coll)
		}
		e.key(tcell.KeyEscape, 0)
	})

	t.Run("filtered counts are exact and async", func(t *testing.T) {
		e.openColl("big")
		e.query(`{i: {$lt: 123}}`)
		e.waitFor("count", func() bool { return s.TotalKnown })
		v := e.snap()
		if !v.TotalExact || v.TotalDocs != 123 {
			t.Errorf("count exact=%v total=%d", v.TotalExact, v.TotalDocs)
		}
		e.query(`db.big.find({i: {$gte: 100}}).sort({i: -1}).skip(5).limit(7)`)
		e.waitFor("count", func() bool { return s.TotalKnown })
		v = e.snap()
		first, _ := mjson.Get(v.CurrentDocs[0], "i")
		if len(v.CurrentDocs) != 7 || first != int32(19994) || v.TotalDocs != 7 {
			t.Errorf("chained find: %d docs, first %v, total %d", len(v.CurrentDocs), first, v.TotalDocs)
		}
		e.query("")
	})

	t.Run("export all", func(t *testing.T) {
		e.openColl("big")
		path := filepath.Join(t.TempDir(), "out.json")
		e.query(":export json all " + path)
		e.waitFor("export", func() bool {
			b, err := os.ReadFile(path)
			return err == nil && strings.HasSuffix(string(b), "]\n")
		})
		b, _ := os.ReadFile(path)
		var arr bson.A
		if err := bson.UnmarshalExtJSON(b, true, &arr); err != nil || len(arr) != 20000 {
			t.Errorf("export: %d docs, err %v", len(arr), err)
		}
	})

	t.Run("stats and metrics", func(t *testing.T) {
		e.onUI(func() { documents.ShowStats() })
		e.waitFor("stats", func() bool { return strings.Contains(s.StatsView.GetText(true), "Active Operations") })
		e.waitFor("metrics", func() bool { return strings.Contains(s.MetricsBar.GetText(true), "MongoDB v") })
		if strings.Contains(e.snap().Metrics, "disconnected") {
			t.Error("metrics bar reports disconnected")
		}
		e.key(tcell.KeyEscape, 0)
	})

	t.Run("filter mode lets global keys be typed", func(t *testing.T) {
		e.onUI(func() { s.App.SetFocus(s.CollList) })
		e.typeText("/qW")
		var text string
		e.onUI(func() { text = s.CollFilterText })
		if text != "qW" {
			t.Errorf("filter text = %q", text)
		}
		e.key(tcell.KeyEscape, 0)
	})
}

func bytesEqual(a, b bson.Raw) bool { return string(a) == string(b) }

func TestReadOnlyUserCannotWrite(t *testing.T) {
	uri := os.Getenv("TANGO_E2E_MONGO_URI")
	if uri == "" {
		t.Skip("set TANGO_E2E_MONGO_URI to run end-to-end tests")
	}
	// Privilege parsing against a live server: with auth disabled (the
	// usual test setup) everything is allowed.
	client, _, err := db.Connect(config.Config{URI: uri}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	p, err := db.DetectPrivileges(client)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Can(state.ActRemove, "x", "y") {
		t.Error("expected unrestricted privileges without auth")
	}
}
