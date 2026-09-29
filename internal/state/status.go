package state

import (
	"time"

	"github.com/rivo/tview"

	"tango/internal/mjson"
)

type loadingEntry struct {
	tok int
	msg string
}

type statusState struct {
	loading  []loadingEntry
	nextTok  int
	flash    string
	flashGen int
}

// BeginLoading shows a loading message until EndLoading is called with the
// returned token. Concurrent operations each keep their own entry.
func BeginLoading(msg string) int {
	st := &S.status
	st.nextTok++
	st.loading = append(st.loading, loadingEntry{tok: st.nextTok, msg: msg})
	renderStatus()
	return st.nextTok
}

// UpdateLoading replaces the message of an active loading entry.
func UpdateLoading(tok int, msg string) {
	for i := range S.status.loading {
		if S.status.loading[i].tok == tok {
			S.status.loading[i].msg = msg
		}
	}
	renderStatus()
}

func EndLoading(tok int) {
	st := &S.status
	for i, e := range st.loading {
		if e.tok == tok {
			st.loading = append(st.loading[:i], st.loading[i+1:]...)
			break
		}
	}
	renderStatus()
}

// FlashStatus shows msg (tview markup allowed) for a few seconds.
func FlashStatus(msg string) {
	st := &S.status
	st.flash = msg
	st.flashGen++
	gen := st.flashGen
	renderStatus()
	go func() {
		time.Sleep(3 * time.Second)
		S.App.QueueUpdateDraw(func() {
			if S.status.flashGen == gen {
				S.status.flash = ""
				renderStatus()
			}
		})
	}()
}

// FlashError flashes an error message in the status bar.
func FlashError(msg string) {
	FlashStatus(" [red]" + tview.Escape(msg))
}

func renderStatus() {
	if S.StatusBar == nil {
		return
	}
	st := &S.status
	switch {
	case st.flash != "":
		S.StatusBar.SetText(st.flash)
	case len(st.loading) > 0:
		S.StatusBar.SetText(" [yellow]⏳ " + tview.Escape(st.loading[len(st.loading)-1].msg) + "...")
	default:
		S.StatusBar.SetText(S.DefaultStatusText)
	}
}

// ShowError displays msg in the detail panel.
func ShowError(msg string) {
	SetDetail(" Error ", []mjson.Token{{Text: msg, Kind: mjson.Error}})
}

// ShowMessage displays plain informational text in the detail panel.
func ShowMessage(title, msg string) {
	SetDetail(title, []mjson.Token{{Text: msg, Kind: mjson.Text}})
}

// SetDetail shows highlighted tokens in the detail panel and focuses it.
// It clears the expanded document; callers showing a document set it after.
func SetDetail(title string, toks []mjson.Token) {
	s := S
	s.ExpandedDoc = nil
	s.DetailBackPage = ""
	s.DetailTokens = toks
	s.DocSearchTerm = ""
	text, _ := RenderTokens(toks, "")
	s.DocDetail.SetText(text)
	s.DocDetail.SetTitle(title)
	s.DocDetail.ScrollToBeginning()
	s.DocPages.SwitchToPage("detail")
	s.App.SetFocus(s.DocDetail)
	RefreshPanelBorders()
}
