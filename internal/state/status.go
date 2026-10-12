package state

import (
	"time"

	"tango/internal/mjson"
)

type loadingEntry struct {
	tok int
	msg string
}

type statusState struct {
	loading    []loadingEntry
	nextTok    int
	flash      string
	flashGen   int
	flashError bool
}

// BeginLoading shows a loading message until EndLoading is called with the
// returned token. Concurrent operations each keep their own entry.
func BeginLoading(msg string) int {
	st := &S.status
	st.nextTok++
	st.loading = append(st.loading, loadingEntry{tok: st.nextTok, msg: msg})
	return st.nextTok
}

// UpdateLoading replaces the message of an active loading entry.
func UpdateLoading(tok int, msg string) {
	for i := range S.status.loading {
		if S.status.loading[i].tok == tok {
			S.status.loading[i].msg = msg
		}
	}
}

func EndLoading(tok int) {
	st := &S.status
	for i, e := range st.loading {
		if e.tok == tok {
			st.loading = append(st.loading[:i], st.loading[i+1:]...)
			break
		}
	}
}

// FlashStatus shows plain text for a few seconds.
func FlashStatus(msg string) {
	flash(msg, false)
}

func flash(msg string, isError bool) {
	s := S
	st := &S.status
	st.flash = msg
	st.flashError = isError
	st.flashGen++
	gen := st.flashGen
	time.AfterFunc(3*time.Second, func() {
		Post(func() {
			if S == s && s.status.flashGen == gen {
				s.status.flash = ""
			}
		})
	})
}

// FlashError flashes an error message in the status bar.
func FlashError(msg string) {
	flash(msg, true)
}

func Status() (flash string, isError bool, loading string) {
	st := &S.status
	if len(st.loading) > 0 {
		loading = st.loading[len(st.loading)-1].msg
	}
	return st.flash, st.flashError, loading
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
	s.DetailTitle = title
	s.DetailRevision++
	s.DocSearchTerm = ""
	s.View, s.Focus = "detail", "content"
}
