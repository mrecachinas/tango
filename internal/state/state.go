package state

import (
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"os/exec"

	"tango/internal/config"
	"tango/internal/mjson"
)

var PageSize = 50

// RollingBuffer keeps the most recent Size samples.
type RollingBuffer struct {
	Data []float64
	Size int
}

func NewRollingBuffer(size int) *RollingBuffer {
	return &RollingBuffer{Size: size}
}

func (rb *RollingBuffer) Push(v float64) {
	rb.Data = append(rb.Data, v)
	if len(rb.Data) > rb.Size {
		rb.Data = rb.Data[len(rb.Data)-rb.Size:]
	}
}

func (rb *RollingBuffer) Last() float64 {
	if len(rb.Data) == 0 {
		return 0
	}
	return rb.Data[len(rb.Data)-1]
}

// Namespace identifies a collection.
type Namespace struct {
	DB   string
	Coll string
}

func (n Namespace) String() string {
	if n.Coll == "" {
		return n.DB
	}
	return n.DB + "." + n.Coll
}

// PageStart is the _id after which a keyset-paged page begins.
type PageStart struct {
	ID  interface{}
	Set bool
}

// ListEntry is a database or collection shown in a list panel.
type ListEntry struct {
	Name string
	Info string // shown in gray after the name, e.g. a count
}

// Confirmation captures the target and action before an asynchronous reload
// can change the selected document or namespace.
type Confirmation struct {
	ID, Question, Expected string
	OnYes                  func()
}

type EditorRequest struct {
	Command *exec.Cmd
	Done    func(error)
}

type StatsColumn struct {
	Name  string
	Width int
	Right bool
}

type StatsSection struct {
	Title, Note, Empty string
	Columns            []StatsColumn
	Rows               [][]string
}

// AppState is the data store. Mutate it only on the UI goroutine; background
// work hands results back through Post. Components belong to the root model.
type AppState struct {
	Client       *mongo.Client
	GlobalCfg    config.Config
	HostLabel    string
	QueryTimeout time.Duration

	View, Focus        string // list/detail/tree/indexes/stats; sidebar/content
	Cursor             int
	IndexCursor        int
	SelectedDB         string
	SelectedColl       string
	DetailTitle        string
	StatsText          string
	QueryText          string
	PromptKind         string
	PromptText         string
	Confirm            *Confirmation
	Editor             *EditorRequest
	Quit               bool
	DocsRevision       uint64
	DetailRevision     uint64
	ListRevision       uint64
	StatsRevision      uint64
	SidebarRevision    uint64
	Collections        map[string][]ListEntry
	CollectionsLoading map[string]bool
	status             statusState

	// Browse state for the current collection.
	CurrentDB         string
	CurrentColl       string
	CurrentPage       int
	CurrentFilter     bson.D
	CurrentProjection bson.D
	CurrentSort       bson.D
	QuerySkip         int64
	QueryLimit        int64       // 0 = unlimited
	PageStarts        []PageStart // keyset paging: exclusive lower-bound _id per page
	KeysetMode        bool        // current page was loaded with keyset paging
	NoKeyset          map[Namespace]bool
	SnapshotID        interface{} // max _id at first load, for stable skip paging
	CountDirty        bool        // filter changed; TotalDocs must be recounted
	TotalDocs         int64
	TotalKnown        bool
	TotalExact        bool // false when TotalDocs is an estimate
	Counting          bool

	// Loaded documents and where they came from. Writes must target DocsNS,
	// never CurrentDB/CurrentColl, and are refused unless allowed below.
	CurrentDocs   []bson.D
	DocsNS        Namespace
	DocsDeletable bool
	DocsEditable  bool   // false for projected or aggregated results
	ResultLabel   string // non-empty for ad-hoc result sets (findOne/aggregate)

	// Document currently shown in the detail/tree view.
	ExpandedDoc      bson.D
	ExpandedNS       Namespace
	ExpandedEditable bool
	DetailTokens     []mjson.Token

	CurrentIndexes []bson.D
	IndexesNS      Namespace

	// Request generations: async results are dropped when these have moved on.
	DBEpoch     uint64
	CollEpoch   uint64
	DocsEpoch   uint64
	CountEpoch  uint64
	StatsEpoch  uint64
	DocsLoading bool

	// Database/collection metadata.
	AllDBItems      []ListEntry
	AllCollItems    []ListEntry
	CachedDBCount   int
	CachedCollCount int

	// Doc search state
	DocSearchActive bool
	DocSearchTerm   string

	// Selection (indices into CurrentDocs)
	SelectedDocs map[int]bool

	// Persisted state
	LastDB          string
	LastCollection  string
	QueryHistory    []string
	QueryHistoryIdx int // for arrow-up navigation, -1 = not browsing

	// RBAC: Privileges is nil when the server doesn't enforce auth.
	ReadOnly   bool
	Privileges *Privileges

	// Wide mode (collapse db/coll panels for full-width docs)
	WideMode bool

	// Live mode
	LiveMode     bool
	LiveInterval time.Duration
	LiveStopCh   chan struct{}

	// Detail view: page to return to on Esc ("list" when empty).
	DetailBackPage string

	// Stats view
	StatsOps        map[string]interface{} // displayed opid -> raw opid, for :killop
	StatsUpdated    string
	StatsSections   []StatsSection
	RefreshInterval time.Duration // metrics/stats polling interval
	StatsStopCh     chan struct{}
}

// S is the global singleton application state.
var S *AppState

func ToFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

// ClearDocPanel resets loaded documents and cancels in-flight loads.
func ClearDocPanel() {
	s := S
	s.DocsEpoch++
	s.CountEpoch++
	s.DocsLoading = false
	s.Counting = false
	s.CurrentDocs = nil
	s.DocsNS = Namespace{}
	s.DocsDeletable = false
	s.DocsEditable = false
	s.ResultLabel = ""
	s.ExpandedDoc = nil
	s.DetailTokens = nil
	s.SelectedDocs = make(map[int]bool)
	s.TotalKnown, s.TotalExact, s.KeysetMode = false, false, false
	s.TotalDocs = 0
	s.ExpandedNS = Namespace{}
	s.ExpandedEditable = false
	s.DocSearchActive, s.DocSearchTerm = false, ""
	s.Cursor = 0
	s.DocsRevision++
	s.DetailRevision++
	s.View = "list"
}

// ResetBrowse clears paging/query state for a fresh view of a collection.
func ResetBrowse() {
	s := S
	s.CurrentPage = 0
	s.PageStarts = nil
	s.SnapshotID = nil
	s.CountDirty = true
	s.CurrentFilter = nil
	s.CurrentProjection = nil
	s.QuerySkip = 0
	s.QueryLimit = 0
}

func GetSelectedCollName() string {
	return S.SelectedColl
}

// ListLabel formats an entry without terminal markup.
func ListLabel(e ListEntry) string {
	label := e.Name
	if e.Info != "" {
		label += " (" + e.Info + ")"
	}
	return label
}

// FormatSort renders a sort spec like "a ↑ b ↓".
func FormatSort(sort bson.D) string {
	parts := make([]string, 0, len(sort))
	for _, e := range sort {
		arrow := "↑"
		if v, ok := ToFloat64(e.Value); ok && v < 0 {
			arrow = "↓"
		}
		parts = append(parts, e.Key+" "+arrow)
	}
	return strings.Join(parts, " ")
}

// TotalPages returns the page count, or -1 when the total is unknown.
func TotalPages() int64 {
	if !S.TotalKnown {
		return -1
	}
	p := (S.TotalDocs + int64(PageSize) - 1) / int64(PageSize)
	if p == 0 {
		p = 1
	}
	return p
}

func FormatDocListTitle() string {
	s := S
	if s.CurrentColl == "" && s.ResultLabel == "" && len(s.CurrentDocs) == 0 {
		return ""
	}
	var parts []string
	if s.ResultLabel != "" {
		parts = append(parts, fmt.Sprintf("%d results · %s", len(s.CurrentDocs), s.ResultLabel))
	} else {
		approx := ""
		if !s.TotalExact {
			approx = "~"
		}
		switch {
		case s.TotalKnown:
			start := s.CurrentPage*PageSize + 1
			if len(s.CurrentDocs) == 0 {
				start = 0
			}
			end := s.CurrentPage*PageSize + len(s.CurrentDocs)
			parts = append(parts, fmt.Sprintf("%d–%d of %s%d", start, end, approx, s.TotalDocs))
		case s.Counting:
			parts = append(parts, "counting…")
		default:
			parts = append(parts, fmt.Sprintf("page %d · count unavailable", s.CurrentPage+1))
		}
		if len(s.CurrentSort) > 0 {
			parts = append(parts, FormatSort(s.CurrentSort))
		}
		if len(s.CurrentFilter) > 0 || len(s.CurrentProjection) > 0 || s.QuerySkip > 0 || s.QueryLimit > 0 {
			parts = append(parts, mjson.Compact(s.CurrentFilter))
		}
		if !s.KeysetMode && int64(s.CurrentPage)*int64(PageSize)+s.QuerySkip > 100000 {
			parts = append(parts, "deep page")
		}
		if s.SnapshotID != nil && s.CurrentPage > 0 && !s.KeysetMode {
			parts = append(parts, "snapshot")
		}
	}
	if n := len(s.SelectedDocs); n > 0 {
		parts = append(parts, fmt.Sprintf("%d selected", n))
	}
	return strings.Join(parts, " · ")
}
