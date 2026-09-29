package state

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

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

func (n Namespace) String() string { return n.DB + "." + n.Coll }

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

// AppState holds all UI and navigation state. Unless noted otherwise, fields
// must only be read or written on the tview UI goroutine; background work
// hands results back via App.QueueUpdateDraw (see Async).
type AppState struct {
	App          *tview.Application
	Client       *mongo.Client
	GlobalCfg    config.Config
	HostLabel    string
	QueryTimeout time.Duration

	// UI widgets
	MetricsBar    *tview.TextView
	SearchBar     *tview.InputField
	DbList        *tview.List
	CollList      *tview.List
	DocList       *tview.List
	DocDetail     *tview.TextView
	DocTree       *tview.TreeView
	DocPages      *tview.Pages
	IndexList     *tview.List
	StatsView     *tview.TextView
	StatusBar     *tview.TextView
	HelpView      *tview.TextView
	ConfirmModal  *tview.Modal
	DropModal     *tview.Modal
	RootPages     *tview.Pages
	DocSearchBar  *tview.InputField
	DocDetailFlex *tview.Flex
	SortForm      *tview.Form

	DefaultStatusText string
	status            statusState

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

	// List panels
	AllDBItems       []ListEntry
	AllCollItems     []ListEntry
	DbVisible        []string // names in DbList row order
	CollVisible      []string // names in CollList row order
	DbFilterText     string
	CollFilterText   string
	DbFilterActive   bool
	CollFilterActive bool
	CachedDBCount    int
	CachedCollCount  int

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
	WideMode   bool
	PanelsFlex *tview.Flex

	// Live mode
	LiveMode     bool
	LiveInterval time.Duration
	LiveStopCh   chan struct{}

	// Detail view: page to return to on Esc ("list" when empty).
	DetailBackPage string

	// Stats view
	StatsOps        map[string]interface{} // displayed opid -> raw opid, for :killop
	RefreshInterval time.Duration          // metrics/stats polling interval
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
	s.DocList.Clear()
	s.DocDetail.Clear()
	s.DocTree.SetRoot(nil)
	s.DocPages.SwitchToPage("list")
	s.DocList.SetTitle(" Documents ")
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

func ActiveDocPanel() tview.Primitive {
	name, _ := S.DocPages.GetFrontPage()
	switch name {
	case "detail":
		return S.DocDetail
	case "tree":
		return S.DocTree
	case "indexes":
		return S.IndexList
	case "stats":
		return S.StatsView
	case "databases":
		return S.DbList
	case "collections":
		return S.CollList
	default:
		return S.DocList
	}
}

func RefreshPanelBorders() {
	focused := S.App.GetFocus()
	panels := []tview.Primitive{S.DbList, S.CollList, S.DocList, S.DocDetail, S.DocTree, S.IndexList, S.StatsView}
	for _, p := range panels {
		active := (p == focused)
		var bc, tc tcell.Color
		if active {
			bc = tcell.ColorDodgerBlue
			tc = tcell.ColorDodgerBlue
		} else {
			bc = tcell.ColorDimGray
			tc = tcell.ColorGray
		}
		switch w := p.(type) {
		case *tview.List:
			w.SetBorderColor(bc)
			w.SetTitleColor(tc)
		case *tview.TextView:
			w.SetBorderColor(bc)
			w.SetTitleColor(tc)
		case *tview.TreeView:
			w.SetBorderColor(bc)
			w.SetTitleColor(tc)
		}
	}
	UpdateStatusHints()
}

func UpdateStatusHints() {
	liveHint := ""
	if S.LiveMode {
		liveHint = "[red]◉ LIVE[white]  [yellow]L[white]:pause  "
	}
	wideHint := "[yellow]W[white]:wide  "
	if S.WideMode {
		wideHint = "[yellow]W[white]:panels  "
	}
	common := liveHint + wideHint + "[yellow]Tab[white]:panels  [yellow]/[white]:query  [yellow]r[white]:refresh  [yellow]Esc[white]:back  [yellow]q[white]:quit"

	focused := S.App.GetFocus()
	var ctxHint string
	switch focused {
	case S.DbList:
		ctxHint = "[yellow]/[white]:filter"
	case S.CollList:
		if S.ReadOnly {
			ctxHint = "[yellow]I[white]:indexes  [yellow]/[white]:filter"
		} else {
			ctxHint = "[yellow]I[white]:indexes  [yellow]D[white]:drop  [yellow]/[white]:filter"
		}
	case S.DocList:
		sel := "[yellow]Space[white]:select  [yellow]a[white]:all  [yellow]A[white]:none  "
		if len(S.SelectedDocs) == 0 {
			sel = "[yellow]Enter[white]:expand  [yellow]Space[white]:select  [yellow]n[white]/[yellow]p[white]:page  [yellow]s[white]:sort  "
		}
		if S.ReadOnly {
			ctxHint = sel + "[yellow]x[white]:export"
		} else {
			ctxHint = sel + "[yellow]d[white]:delete  [yellow]i[white]:insert  [yellow]x[white]:export"
		}
	case S.DocDetail:
		if S.ReadOnly {
			ctxHint = "[yellow]/[white]:search  [yellow]c[white]:copy  [yellow]t[white]:tree"
		} else {
			ctxHint = "[yellow]/[white]:search  [yellow]c[white]:copy  [yellow]t[white]:tree  [yellow]e[white]:edit  [yellow]i[white]:insert"
		}
	case S.DocTree:
		if S.ReadOnly {
			ctxHint = "[yellow]c[white]:copy  [yellow]t[white]:json"
		} else {
			ctxHint = "[yellow]c[white]:copy  [yellow]t[white]:json  [yellow]e[white]:edit  [yellow]i[white]:insert"
		}
	case S.IndexList:
		if S.ReadOnly {
			ctxHint = "[yellow]Enter[white]:details"
		} else {
			ctxHint = "[yellow]Enter[white]:details  [yellow]X[white]:drop index"
		}
	case S.StatsView:
		ctxHint = "[yellow]r[white]:refresh  [yellow]:killop[white] <opid>  [yellow]Esc[white]:back"
	}

	if ctxHint != "" {
		S.DefaultStatusText = " " + ctxHint + "  [gray]│[white]  " + common
	} else {
		S.DefaultStatusText = " " + common
	}
	renderStatus()
}

func CycleFocus(forward bool) {
	if S.WideMode {
		S.App.SetFocus(ActiveDocPanel())
		RefreshPanelBorders()
		return
	}
	focusable := []tview.Primitive{S.DbList, S.CollList, ActiveDocPanel()}
	focused := S.App.GetFocus()
	for i, p := range focusable {
		if p == focused {
			var next int
			if forward {
				next = (i + 1) % len(focusable)
			} else {
				next = (i - 1 + len(focusable)) % len(focusable)
			}
			S.App.SetFocus(focusable[next])
			RefreshPanelBorders()
			return
		}
	}
}

func GetSelectedCollName() string {
	idx := S.CollList.GetCurrentItem()
	if idx < 0 || idx >= len(S.CollVisible) {
		return ""
	}
	return S.CollVisible[idx]
}

// ListLabel formats a list entry with its (escaped) name and gray info.
func ListLabel(e ListEntry) string {
	label := tview.Escape(e.Name)
	if e.Info != "" {
		label += " [gray](" + tview.Escape(e.Info) + ")[-]"
	}
	return label
}

// FormatSort renders a sort spec like "a↑ b↓".
func FormatSort(sort bson.D) string {
	parts := make([]string, 0, len(sort))
	for _, e := range sort {
		arrow := "↑"
		if v, ok := ToFloat64(e.Value); ok && v < 0 {
			arrow = "↓"
		}
		parts = append(parts, e.Key+arrow)
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
	var title string
	if s.ResultLabel != "" {
		title = fmt.Sprintf(" Results [green](%d)[white] %s", len(s.CurrentDocs), tview.Escape(s.ResultLabel))
	} else {
		ns := tview.Escape(s.CurrentDB + "." + s.CurrentColl)
		approx := ""
		if !s.TotalExact {
			approx = "~"
		}
		switch {
		case s.TotalKnown:
			title = fmt.Sprintf(" Documents [yellow](%s)[white] [green][%d/%s%d][white] %s%d total",
				ns, s.CurrentPage+1, approx, TotalPages(), approx, s.TotalDocs)
		case s.Counting:
			title = fmt.Sprintf(" Documents [yellow](%s)[white] [green][%d/?][white] counting…", ns, s.CurrentPage+1)
		default:
			title = fmt.Sprintf(" Documents [yellow](%s)[white] [green][%d/?][white] count unavailable", ns, s.CurrentPage+1)
		}
		if len(s.CurrentFilter) > 0 || len(s.CurrentProjection) > 0 || s.QuerySkip > 0 || s.QueryLimit > 0 {
			title += " [aqua]⧩ query[white]"
		}
		if len(s.CurrentSort) > 0 {
			title += " [aqua]⇅ " + tview.Escape(FormatSort(s.CurrentSort)) + "[white]"
		} else if s.LiveMode {
			title += " [aqua]newest first[white]"
		}
		if !s.KeysetMode && int64(s.CurrentPage)*int64(PageSize)+s.QuerySkip > 100000 {
			title += " [red]⚠ deep page — may be slow[white]"
		}
		if s.SnapshotID != nil && s.CurrentPage > 0 && !s.KeysetMode {
			title += " 📌"
		}
	}
	if s.LiveMode {
		title = " [red]◉ LIVE[white]" + title
	}
	if n := len(s.SelectedDocs); n > 0 {
		title += fmt.Sprintf(" [blue]%d selected[white]", n)
	}
	if s.WideMode {
		breadcrumb := tview.Escape(s.CurrentDB)
		if s.CurrentColl != "" {
			breadcrumb += " > " + tview.Escape(s.CurrentColl)
		}
		if breadcrumb != "" {
			title = fmt.Sprintf(" [gray]%s[white] │%s", breadcrumb, title)
		}
	}
	return title + " "
}
