package documents

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tango/internal/mjson"
	"tango/internal/state"
)

const (
	listConcurrency = 8
	perItemTimeout  = 5 * time.Second
	maxListItemLen  = 1000
)

// LoadDatabases (re)loads the database list. then, if non-nil, runs on the
// UI goroutine once the list is populated.
func LoadDatabases(then func()) {
	s := state.S
	s.DBEpoch++
	epoch := s.DBEpoch
	client := s.Client
	state.Async("Loading databases", func(ctx context.Context) func() {
		res, err := client.ListDatabases(ctx, bson.D{},
			options.ListDatabases().SetNameOnly(true).SetAuthorizedDatabases(true))
		if err != nil {
			return func() {
				if s.DBEpoch == epoch {
					state.ShowError("Error listing databases: " + err.Error())
				}
			}
		}
		names := make([]string, 0, len(res.Databases))
		for _, d := range res.Databases {
			names = append(names, d.Name)
		}
		sort.Strings(names)
		return func() {
			if s.DBEpoch != epoch {
				return
			}
			s.AllDBItems = make([]state.ListEntry, len(names))
			for i, n := range names {
				s.AllDBItems[i] = state.ListEntry{Name: n, Info: "…"}
			}
			RenderDBList()
			s.CachedDBCount = len(names)
			loadDBCollectionCounts(names, epoch)
			if then != nil {
				then()
			}
		}
	})
}

func loadDBCollectionCounts(names []string, epoch uint64) {
	s := state.S
	client := s.Client
	go func() {
		counts := make([]int, len(names))
		forEachLimit(len(names), listConcurrency, func(i int) {
			ctx, cancel := context.WithTimeout(context.Background(), perItemTimeout)
			defer cancel()
			colls, err := client.Database(names[i]).ListCollectionNames(ctx, bson.D{},
				options.ListCollections().SetAuthorizedCollections(true))
			if err != nil {
				counts[i] = -1
				return
			}
			counts[i] = len(colls)
		})
		state.Post(func() {
			if s.DBEpoch != epoch {
				return
			}
			total := 0
			for i, n := range names {
				info := "?"
				if counts[i] >= 0 {
					info = strconv.Itoa(counts[i])
					total += counts[i]
				}
				for j := range s.AllDBItems {
					if s.AllDBItems[j].Name == n {
						s.AllDBItems[j].Info = info
					}
				}
			}
			s.CachedCollCount = total
			RenderDBList()
		})
	}()
}

// SelectDatabase switches to dbName and loads its collections. then, if
// non-nil, runs on the UI goroutine once the collection list is populated.
func SelectDatabase(dbName string, then func()) {
	s := state.S
	stopLive(false)
	s.CurrentDB = dbName
	s.CurrentColl = ""
	s.SelectedDB, s.SelectedColl = dbName, ""
	state.ResetBrowse()
	s.CurrentSort = nil
	state.ClearDocPanel()
	s.QueryText = ""
	s.CollEpoch++
	epoch := s.CollEpoch
	s.AllCollItems = nil
	if s.CollectionsLoading == nil {
		s.CollectionsLoading = map[string]bool{}
	}
	for db := range s.CollectionsLoading {
		s.CollectionsLoading[db] = false
	}
	s.CollectionsLoading[dbName] = true
	RenderCollList()

	client := s.Client
	state.Async("Loading collections", func(ctx context.Context) func() {
		specs, err := client.Database(dbName).ListCollectionSpecifications(ctx, bson.D{},
			options.ListCollections().SetNameOnly(true).SetAuthorizedCollections(true))
		if err != nil {
			return func() {
				if s.CollEpoch == epoch {
					s.CollectionsLoading[dbName] = false
					s.SidebarRevision++
					state.ShowError("Error listing collections: " + err.Error())
				}
			}
		}
		sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
		return func() {
			if s.CollEpoch != epoch {
				return
			}
			s.AllCollItems = make([]state.ListEntry, len(specs))
			var toCount []string
			for i, sp := range specs {
				info := "…"
				if sp.Type == "view" {
					info = "view"
				} else {
					toCount = append(toCount, sp.Name)
				}
				s.AllCollItems[i] = state.ListEntry{Name: sp.Name, Info: info}
			}
			RenderCollList()
			s.CollectionsLoading[dbName] = false
			s.SidebarRevision++
			s.Focus = "sidebar"
			loadCollectionCounts(dbName, toCount, epoch)
			if then != nil {
				then()
			}
		}
	})
}

func loadCollectionCounts(dbName string, names []string, epoch uint64) {
	s := state.S
	client := s.Client
	go func() {
		counts := make([]int64, len(names))
		forEachLimit(len(names), listConcurrency, func(i int) {
			ctx, cancel := context.WithTimeout(context.Background(), perItemTimeout)
			defer cancel()
			n, err := client.Database(dbName).Collection(names[i]).EstimatedDocumentCount(ctx)
			if err != nil {
				n = -1
			}
			counts[i] = n
		})
		state.Post(func() {
			if s.CollEpoch != epoch {
				return
			}
			byName := make(map[string]int64, len(names))
			for i, n := range names {
				byName[n] = counts[i]
			}
			for i := range s.AllCollItems {
				if c, ok := byName[s.AllCollItems[i].Name]; ok {
					if c < 0 {
						s.AllCollItems[i].Info = "?"
					} else {
						s.AllCollItems[i].Info = strconv.FormatInt(c, 10)
					}
				}
			}
			RenderCollList()
		})
	}()
}

// RestoreSession selects db and then coll, if they still exist.
func RestoreSession(db, coll string) {
	s := state.S
	if db == "" || !hasEntry(s.AllDBItems, db) {
		return
	}
	SelectDatabase(db, func() {
		if coll != "" && hasEntry(s.AllCollItems, coll) {
			SelectCollection(coll)
		}
	})
}

func SelectCollection(collName string) {
	s := state.S
	stopLive(false)
	s.CurrentColl = collName
	state.ResetBrowse()
	s.CurrentSort = nil
	ClearSelection()
	state.ClearDocPanel()
	s.QueryText = ""
	SelectCollectionInList(collName)
	LoadDocuments()
}

// LoadDocuments loads the current page and shows the document list.
func LoadDocuments() { loadDocuments(true) }

// ReloadDocuments reloads the current page in place, keeping the cursor,
// selection and whichever panel is focused.
func ReloadDocuments() { loadDocuments(false) }

type pageRequest struct {
	ns         state.Namespace
	page       int
	pageSize   int
	filter     bson.D
	projection bson.D
	sort       bson.D
	skip       int64
	limit      int64
	snapshot   interface{}
	live       bool
	keyset     bool
	start      state.PageStart
}

type pageResult struct {
	docs       []bson.D
	snapshot   interface{}
	keysetUsed bool
	noKeyset   bool
	effFilter  bson.D
	total      int64
	totalKnown bool
	err        error
}

func loadDocuments(navigate bool) {
	s := state.S
	if s.CurrentDB == "" || s.CurrentColl == "" {
		return
	}
	if navigate {
		s.View, s.Focus = "list", "content"
	}
	if s.NoKeyset == nil {
		s.NoKeyset = map[state.Namespace]bool{}
	}
	s.DocsEpoch++
	epoch := s.DocsEpoch
	req := currentPageRequest()
	countDirty := s.CountDirty
	if countDirty {
		// Any count still running belongs to the previous query.
		s.CountEpoch++
		s.Counting = false
	}
	client := s.Client
	timeout := s.QueryTimeout
	s.DocsLoading = true
	tok := state.BeginLoading("Loading documents")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		res := fetchPage(ctx, client.Database(req.ns.DB).Collection(req.ns.Coll), req)
		state.Post(func() {
			state.EndLoading(tok)
			if s.DocsEpoch != epoch {
				return
			}
			s.DocsLoading = false
			if res.err != nil {
				state.ShowError(fmt.Sprintf("Error loading %s: %s", req.ns, res.err))
				return
			}
			applyPage(req, res, navigate, countDirty)
		})
	}()
}

// currentPageRequest describes the browse list page the state asks for.
func currentPageRequest() pageRequest {
	s := state.S
	req := pageRequest{
		ns:         state.Namespace{DB: s.CurrentDB, Coll: s.CurrentColl},
		page:       s.CurrentPage,
		pageSize:   state.PageSize,
		filter:     s.CurrentFilter,
		projection: s.CurrentProjection,
		sort:       s.CurrentSort,
		skip:       s.QuerySkip,
		limit:      s.QueryLimit,
		snapshot:   s.SnapshotID,
		live:       s.LiveMode,
	}
	if req.live && len(req.sort) == 0 {
		req.sort = bson.D{{Key: "_id", Value: -1}}
	}
	req.keyset = len(req.filter) == 0 && len(req.sort) == 0 && req.skip == 0 && req.limit == 0 && !s.NoKeyset[req.ns]
	if req.keyset && req.page > 0 && req.page < len(s.PageStarts) {
		req.start = s.PageStarts[req.page]
	}
	return req
}

// FindSpec is a find's filter and options.
type FindSpec struct {
	Filter, Sort, Projection, Hint, Min bson.D
	Skip, Limit                         int64
}

func (f FindSpec) options() *options.FindOptions {
	o := options.Find()
	if len(f.Sort) > 0 {
		o.SetSort(f.Sort)
	}
	if len(f.Projection) > 0 {
		o.SetProjection(f.Projection)
	}
	if len(f.Hint) > 0 {
		o.SetHint(f.Hint)
	}
	if len(f.Min) > 0 {
		o.SetMin(f.Min)
	}
	if f.Skip > 0 {
		o.SetSkip(f.Skip)
	}
	if f.Limit > 0 {
		o.SetLimit(f.Limit)
	}
	return o
}

// Command is the find command f sends to coll.
func (f FindSpec) Command(coll string) bson.D {
	filter := f.Filter
	if filter == nil {
		filter = bson.D{}
	}
	cmd := bson.D{{Key: "find", Value: coll}, {Key: "filter", Value: filter}}
	for _, e := range []bson.E{{Key: "sort", Value: f.Sort}, {Key: "projection", Value: f.Projection},
		{Key: "hint", Value: f.Hint}, {Key: "min", Value: f.Min}} {
		if d := e.Value.(bson.D); len(d) > 0 {
			cmd = append(cmd, e)
		}
	}
	if f.Skip > 0 {
		cmd = append(cmd, bson.E{Key: "skip", Value: f.Skip})
	}
	if f.Limit > 0 {
		cmd = append(cmd, bson.E{Key: "limit", Value: f.Limit})
	}
	return cmd
}

// pageFind is the find that loads req's page. Keyset pages hint the _id
// index and start after the previous page's last _id; other pages cap the
// filter at the snapshot _id (outside live mode) and page with skip. ok is
// false when the page lies past the query's limit, so nothing is fetched.
func pageFind(req pageRequest, snapshot interface{}) (spec FindSpec, ok bool) {
	if req.keyset {
		spec = FindSpec{Filter: bson.D{}, Projection: req.projection,
			Hint: bson.D{{Key: "_id", Value: 1}}, Limit: int64(req.pageSize)}
		if req.start.Set {
			spec.Min = bson.D{{Key: "_id", Value: req.start.ID}}
			spec.Filter = bson.D{{Key: "_id", Value: bson.D{{Key: "$ne", Value: req.start.ID}}}}
		} else if req.page > 0 {
			spec.Skip = int64(req.page) * int64(req.pageSize)
		}
		return spec, true
	}
	filter := req.filter
	if filter == nil {
		filter = bson.D{}
	}
	if snapshot != nil && !req.live {
		cond := bson.D{{Key: "_id", Value: bson.D{{Key: "$not", Value: bson.D{{Key: "$gt", Value: snapshot}}}}}}
		if len(filter) == 0 {
			filter = cond
		} else {
			filter = bson.D{{Key: "$and", Value: bson.A{filter, cond}}}
		}
	}
	limit := int64(req.pageSize)
	offset := int64(req.page) * int64(req.pageSize)
	if req.limit > 0 {
		if remaining := req.limit - offset; remaining < limit {
			limit = remaining
		}
	}
	spec = FindSpec{Filter: filter, Sort: req.sort, Projection: req.projection, Skip: req.skip + offset, Limit: limit}
	return spec, limit > 0
}

// CurrentPageFind returns the find that loaded the browse list's current
// page, so explain sees the same hint, keyset bounds and snapshot cap.
func CurrentPageFind() (FindSpec, bool) {
	s := state.S
	req := currentPageRequest()
	req.keyset = req.keyset && s.KeysetMode
	return pageFind(req, s.SnapshotID)
}

func fetchPage(ctx context.Context, coll *mongo.Collection, req pageRequest) pageResult {
	var res pageResult
	filter := req.filter
	if filter == nil {
		filter = bson.D{}
	}
	if req.keyset {
		spec, _ := pageFind(req, nil)
		docs, err := findAll(ctx, coll, spec.Filter, spec.options())
		if err == nil {
			res.docs = docs
			res.keysetUsed = true
			res.effFilter = filter
		} else {
			// No usable _id index (views, time series, …): fall back to skip.
			res.noKeyset = true
			req.keyset = false
		}
	}
	if !req.keyset {
		snapshot := req.snapshot
		if !req.live && snapshot == nil {
			var top bson.D
			err := coll.FindOne(ctx, bson.D{}, options.FindOne().
				SetSort(bson.D{{Key: "_id", Value: -1}}).
				SetProjection(bson.D{{Key: "_id", Value: 1}})).Decode(&top)
			if err == nil {
				snapshot, _ = mjson.Get(top, "_id")
				res.snapshot = snapshot
			}
		}
		spec, ok := pageFind(req, snapshot)
		res.effFilter = spec.Filter
		if ok {
			docs, err := findAll(ctx, coll, spec.Filter, spec.options())
			if err != nil {
				res.err = err
				return res
			}
			res.docs = docs
		}
	}

	if len(req.filter) == 0 && req.skip == 0 && req.limit == 0 {
		if n, err := coll.EstimatedDocumentCount(ctx); err == nil {
			res.total = n
			res.totalKnown = true
		}
	}
	return res
}

func findAll(ctx context.Context, coll *mongo.Collection, filter bson.D, opts *options.FindOptions) ([]bson.D, error) {
	cur, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	docs := []bson.D{}
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// docListItem renders a document as a single truncated plain-text row.
func docListItem(d bson.D) string {
	text := mjson.Compact(d)
	if r := []rune(text); len(r) > maxListItemLen {
		text = string(r[:maxListItemLen]) + "…"
	}
	return text
}

func idKey(d bson.D) (string, bool) {
	id, ok := mjson.Get(d, "_id")
	if !ok {
		return "", false
	}
	return mjson.Compact(id), true
}

func applyPage(req pageRequest, res pageResult, navigate, countDirty bool) {
	s := state.S
	prevSelected := map[string]bool{}
	if !navigate {
		for _, d := range selectedDocsSlice() {
			if k, ok := idKey(d); ok {
				prevSelected[k] = true
			}
		}
	}
	cursor := s.Cursor

	s.CurrentDocs = res.docs
	s.DocsNS = req.ns
	s.DocsDeletable = true
	s.DocsEditable = len(req.projection) == 0
	s.ResultLabel = ""
	s.KeysetMode = res.keysetUsed
	if res.noKeyset {
		s.NoKeyset[req.ns] = true
	}
	if res.snapshot != nil {
		s.SnapshotID = res.snapshot
	}
	if res.totalKnown {
		s.TotalDocs = res.total
		s.TotalKnown = true
		s.TotalExact = false
		s.CountDirty = false
	} else if countDirty {
		s.CountDirty = false
		startCount(req, res.effFilter)
	}

	s.SelectedDocs = make(map[int]bool)
	if len(prevSelected) > 0 {
		for i, d := range s.CurrentDocs {
			if k, ok := idKey(d); ok && prevSelected[k] {
				s.SelectedDocs[i] = true
			}
		}
	}
	s.DocsRevision++
	if navigate {
		s.Cursor = 0
		s.View, s.Focus = "list", "content"
	} else if n := len(s.CurrentDocs); n > 0 {
		if cursor >= n {
			cursor = n - 1
		}
		if cursor < 0 {
			cursor = 0
		}
		s.Cursor = cursor
	}
}

// startCount counts matching documents in the background.
func startCount(req pageRequest, filter bson.D) {
	s := state.S
	s.CountEpoch++
	epoch := s.CountEpoch
	s.Counting = true
	s.TotalKnown = false
	coll := s.Client.Database(req.ns.DB).Collection(req.ns.Coll)
	timeout := s.QueryTimeout
	if filter == nil {
		filter = bson.D{}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		opts := options.Count()
		if req.skip > 0 {
			opts.SetSkip(req.skip)
		}
		if req.limit > 0 {
			opts.SetLimit(req.limit)
		}
		n, err := coll.CountDocuments(ctx, filter, opts)
		state.Post(func() {
			if s.CountEpoch != epoch {
				return
			}
			s.Counting = false
			if err != nil {
				s.TotalKnown = false
				state.FlashError("Count failed: " + err.Error())
			} else {
				s.TotalDocs = n
				s.TotalKnown = true
				s.TotalExact = true
			}
		})
	}()
}

func NextPage() {
	s := state.S
	if s.ResultLabel != "" || s.LiveMode || len(s.CurrentDocs) < state.PageSize {
		return
	}
	if s.TotalKnown && s.TotalExact && int64(s.CurrentPage+1)*int64(state.PageSize) >= s.TotalDocs {
		return
	}
	if s.KeysetMode {
		for len(s.PageStarts) <= s.CurrentPage {
			s.PageStarts = append(s.PageStarts, state.PageStart{})
		}
		s.PageStarts = s.PageStarts[:s.CurrentPage+1]
		id, ok := mjson.Get(s.CurrentDocs[len(s.CurrentDocs)-1], "_id")
		s.PageStarts = append(s.PageStarts, state.PageStart{ID: id, Set: ok})
	}
	s.CurrentPage++
	LoadDocuments()
}

func PrevPage() {
	s := state.S
	if s.ResultLabel != "" || s.LiveMode || s.CurrentPage == 0 {
		return
	}
	s.CurrentPage--
	LoadDocuments()
}

func Refresh() {
	s := state.S
	switch {
	case s.CurrentDB != "" && s.CurrentColl != "":
		s.SnapshotID = nil
		s.CountDirty = true
		if s.ResultLabel != "" {
			s.CurrentPage = 0
			s.PageStarts = nil
		}
		LoadDocuments()
	case s.CurrentDB != "":
		SelectDatabase(s.CurrentDB, nil)
	default:
		LoadDatabases(nil)
	}
}

func FullRefresh() {
	s := state.S
	stopLive(false)
	savedDB, savedColl := s.CurrentDB, s.CurrentColl
	s.CurrentDB, s.CurrentColl = "", ""
	s.CollEpoch++
	s.AllCollItems = nil
	s.Collections = nil
	s.CollectionsLoading = nil
	RenderCollList()
	state.ClearDocPanel()
	LoadDatabases(func() { RestoreSession(savedDB, savedColl) })
}

func StartLiveMode() {
	s := state.S
	if s.LiveMode {
		return
	}
	if s.CurrentDB == "" || s.CurrentColl == "" {
		state.ShowError("Select a collection first")
		return
	}
	s.LiveMode = true
	interval := s.LiveInterval
	if interval == 0 {
		interval = 2 * time.Second
	}
	stop := make(chan struct{})
	s.LiveStopCh = stop
	liveReload()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				state.Post(func() {
					if s.LiveMode && s.LiveStopCh == stop && !s.DocsLoading {
						liveReload()
					}
				})
			case <-stop:
				return
			}
		}
	}()
}

func liveReload() {
	s := state.S
	s.CurrentPage = 0
	s.PageStarts = nil
	s.SnapshotID = nil
	s.CountDirty = !s.Counting
	ReloadDocuments()
}

// StopLiveMode leaves live mode and reloads the page in normal order.
func StopLiveMode() { stopLive(true) }

func stopLive(reload bool) {
	s := state.S
	if !s.LiveMode {
		return
	}
	s.LiveMode = false
	if s.LiveStopCh != nil {
		close(s.LiveStopCh)
		s.LiveStopCh = nil
	}
	if reload && s.CurrentDB != "" && s.CurrentColl != "" {
		s.CurrentPage = 0
		s.PageStarts = nil
		ReloadDocuments()
	}
}

// ShowResultDocs shows an ad-hoc result set (not a paged collection view).
// deletable/editable control whether d/e may act on these documents in ns.
func ShowResultDocs(docs []bson.D, ns state.Namespace, label string, deletable, editable bool) {
	s := state.S
	stopLive(false)
	s.DocsEpoch++
	s.CountEpoch++
	s.DocsLoading = false
	s.Counting = false
	s.CurrentDocs = docs
	s.DocsNS = ns
	s.DocsDeletable = deletable
	s.DocsEditable = editable
	s.ResultLabel = label
	ClearSelection()
	s.DocsRevision++
	s.Cursor = 0
	s.View, s.Focus = "list", "content"
}

// removeDocsByID drops documents whose _id is in keys from an ad-hoc result set.
func removeDocsByID(keys map[string]bool) {
	s := state.S
	kept := s.CurrentDocs[:0]
	for _, d := range s.CurrentDocs {
		if k, ok := idKey(d); ok && keys[k] {
			continue
		}
		kept = append(kept, d)
	}
	ShowResultDocs(kept, s.DocsNS, s.ResultLabel, s.DocsDeletable, s.DocsEditable)
}
