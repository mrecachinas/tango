package state

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"tango/internal/config"
)

var PageSize = 50

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

type AppState struct {
	App          *tview.Application
	Client       *mongo.Client
	GlobalCfg    config.Config
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

	// Navigation state
	CurrentDB      string
	CurrentColl    string
	CurrentPage    int
	TotalDocs      int64
	CurrentFilter  bson.M
	CurrentDocs    []bson.M
	CurrentIndexes []bson.M
	CountDirty     bool        // when true, TotalDocs must be recounted
	SnapshotID     interface{} // max _id at time of first load, nil = no snapshot

	// Cached data
	CachedCollNames []string
	CachedDBCount   int
	CachedCollCount int

	// Filter state
	DbFilterText   string
	CollFilterText string
	AllDBItems     []string
	AllCollItems   []string

	// Doc search state
	DocSearchOriginal string
	DocSearchActive   bool

	// Selection
	SelectedDocs map[int]bool

	// Persisted state
	LastDB          string // loaded from persisted state, used for auto-restore
	LastCollection  string
	QueryHistory    []string
	QueryHistoryIdx int // for arrow-up navigation, -1 = not browsing

	// Sort state
	CurrentSortField string
	CurrentSortOrder int

	// RBAC
	ReadOnly bool

	// Live mode
	LiveMode     bool
	LiveInterval time.Duration
	LiveTicker   *time.Ticker
	LiveStopCh   chan struct{}

	// Metrics state
	PrevOpsTotal   int64
	OpsHistory     *RollingBuffer
	ConnHistory    *RollingBuffer
	MemHistory     *RollingBuffer
	PingHistory    *RollingBuffer
	QueueHistory   *RollingBuffer
	NetInHistory   *RollingBuffer
	CursorsHistory *RollingBuffer
	FaultsHistory  *RollingBuffer
	PrevNetIn      int64
	PrevNetOut     int64
	IsReplicaSet   bool
	ReplLagMs      float64
}

// S is the global singleton application state.
var S *AppState

func NewCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), S.QueryTimeout)
}

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

func BareName(item string) string {
	if idx := strings.Index(item, " [gray]"); idx != -1 {
		return item[:idx]
	}
	return item
}

func CheckWriteAccess() bool {
	if S.ReadOnly {
		ShowError("Read-only connection — write operations disabled")
		return false
	}
	return true
}

func ShowError(msg string) {
	S.DocDetail.SetText("[red]" + msg)
	S.DocDetail.SetTitle(" Error ")
	S.DocDetail.ScrollToBeginning()
	S.DocPages.SwitchToPage("detail")
	S.App.SetFocus(S.DocDetail)
}

func ShowLoading(msg string) {
	S.StatusBar.SetText(" [yellow]⏳ " + msg + "...")
}

func DoneLoading() {
	S.StatusBar.SetText(S.DefaultStatusText)
}

func ClearDocPanel() {
	S.CurrentDocs = nil
	S.DocList.Clear()
	S.DocDetail.Clear()
	S.DocTree.SetRoot(nil)
	S.DocPages.SwitchToPage("list")
	S.DocList.SetTitle(" Documents ")
}

func ActiveDocPanel() tview.Primitive {
	name, _ := S.DocPages.GetFrontPage()
	if name == "detail" {
		return S.DocDetail
	}
	if name == "tree" {
		return S.DocTree
	}
	if name == "indexes" {
		return S.IndexList
	}
	if name == "stats" {
		return S.StatsView
	}
	return S.DocList
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
	common := liveHint + "[yellow]Tab[white]:panels  [yellow]/[white]:query  [yellow]r[white]:refresh  [yellow]Esc[white]:back  [yellow]q[white]:quit"

	focused := S.App.GetFocus()
	var ctxHint string
	switch focused {
	case S.DbList:
		ctxHint = "[gray]type to filter"
	case S.CollList:
		if S.ReadOnly {
			ctxHint = "[yellow]I[white]:indexes  [gray]type to filter"
		} else {
			ctxHint = "[yellow]I[white]:indexes  [yellow]D[white]:drop  [gray]type to filter"
		}
	case S.DocList:
		if S.ReadOnly {
			if len(S.SelectedDocs) > 0 {
				ctxHint = "[yellow]Space[white]:select  [yellow]a[white]:all  [yellow]A[white]:none  [yellow]x[white]:export"
			} else {
				ctxHint = "[yellow]Enter[white]:expand  [yellow]Space[white]:select  [yellow]a[white]:all  [yellow]n[white]/[yellow]p[white]:page  [yellow]x[white]:export"
			}
		} else {
			if len(S.SelectedDocs) > 0 {
				ctxHint = "[yellow]Space[white]:select  [yellow]a[white]:all  [yellow]A[white]:none  [yellow]d[white]:delete  [yellow]x[white]:export"
			} else {
				ctxHint = "[yellow]Enter[white]:expand  [yellow]Space[white]:select  [yellow]a[white]:all  [yellow]n[white]/[yellow]p[white]:page  [yellow]d[white]:delete  [yellow]i[white]:insert  [yellow]x[white]:export"
			}
		}
	case S.DocDetail:
		if S.ReadOnly {
			ctxHint = "[yellow]/[white]:search  [yellow]c[white]:copy"
		} else {
			ctxHint = "[yellow]/[white]:search  [yellow]c[white]:copy  [yellow]e[white]:edit  [yellow]i[white]:insert"
		}
	case S.DocTree:
		if S.ReadOnly {
			ctxHint = "[yellow]c[white]:copy"
		} else {
			ctxHint = "[yellow]c[white]:copy  [yellow]e[white]:edit  [yellow]i[white]:insert"
		}
	case S.IndexList:
		if S.ReadOnly {
			ctxHint = "[yellow]Enter[white]:details"
		} else {
			ctxHint = "[yellow]Enter[white]:details  [yellow]X[white]:drop index"
		}
	case S.StatsView:
		ctxHint = "[yellow]r[white]:refresh  [yellow]Esc[white]:back"
	default:
		ctxHint = ""
	}

	if ctxHint != "" {
		S.DefaultStatusText = " " + ctxHint + "  [gray]│[white]  " + common
	} else {
		S.DefaultStatusText = " " + common
	}
	S.StatusBar.SetText(S.DefaultStatusText)
}

func CycleFocus(forward bool) {
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
	if idx < 0 || idx >= S.CollList.GetItemCount() {
		return ""
	}
	main, _ := S.CollList.GetItemText(idx)
	return BareName(main)
}

func FlashStatus(msg string) {
	original := S.StatusBar.GetText(false)
	S.StatusBar.SetText(msg)
	go func() {
		time.Sleep(2 * time.Second)
		S.App.QueueUpdateDraw(func() {
			S.StatusBar.SetText(original)
		})
	}()
}

func FlashStatusKeep(msg string) {
	prev := S.StatusBar.GetText(false)
	_ = prev
	S.StatusBar.SetText(msg)
	go func() {
		time.Sleep(2 * time.Second)
		S.App.QueueUpdateDraw(func() {
			S.StatusBar.SetText(S.DefaultStatusText)
		})
	}()
}

func FormatDocListTitle() string {
	s := S
	totalPages := (s.TotalDocs + int64(PageSize) - 1) / int64(PageSize)
	if totalPages == 0 {
		totalPages = 1
	}
	title := fmt.Sprintf(" Documents [yellow](%s.%s)[white] [green][%d/%d][white] %d total",
		s.CurrentDB, s.CurrentColl, s.CurrentPage+1, totalPages, s.TotalDocs)
	if s.CurrentSortField != "" {
		arrow := "↑"
		if s.CurrentSortOrder == -1 {
			arrow = "↓"
		}
		title += fmt.Sprintf(" [aqua]⇅ %s %s[white]", s.CurrentSortField, arrow)
	}
	if skip := int64(s.CurrentPage * PageSize); skip > 100000 {
		title += " [red]⚠ deep page — may be slow[white]"
	}
	if s.SnapshotID != nil && s.CurrentPage > 0 {
		title += " 📌"
	}
	if s.LiveMode {
		title = fmt.Sprintf(" [red]◉ LIVE[white] %s", title)
	}
	if n := len(s.SelectedDocs); n > 0 {
		title += fmt.Sprintf(" [blue]%d selected[white]", n)
	}
	title += " "
	return title
}
