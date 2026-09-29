package documents

import (
	"fmt"
	"strings"
	"sync"

	"github.com/rivo/tview"

	"tango/internal/state"
)

// forEachLimit calls fn for every index in [0,n) with at most limit
// concurrent calls, and waits for all of them.
func forEachLimit(n, limit int, fn func(i int)) {
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

func renderList(list *tview.List, entries []state.ListEntry, filter string, visible *[]string) {
	cur := ""
	if idx := list.GetCurrentItem(); idx >= 0 && idx < len(*visible) {
		cur = (*visible)[idx]
	}
	list.Clear()
	names := (*visible)[:0]
	f := strings.ToLower(filter)
	sel := 0
	for _, e := range entries {
		if f != "" && !strings.Contains(strings.ToLower(e.Name), f) {
			continue
		}
		if e.Name == cur {
			sel = len(names)
		}
		list.AddItem(state.ListLabel(e), "", 0, nil)
		names = append(names, e.Name)
	}
	*visible = names
	if len(names) > 0 {
		list.SetCurrentItem(sel)
	}
}

// RenderDBList redraws the database list applying the current filter.
func RenderDBList() {
	s := state.S
	renderList(s.DbList, s.AllDBItems, s.DbFilterText, &s.DbVisible)
	title := " Databases "
	if s.DbFilterActive || s.DbFilterText != "" {
		title = fmt.Sprintf(" Databases [yellow][/%s[] ", tview.Escape(s.DbFilterText))
	}
	s.DbList.SetTitle(title)
}

// RenderCollList redraws the collection list applying the current filter.
func RenderCollList() {
	s := state.S
	renderList(s.CollList, s.AllCollItems, s.CollFilterText, &s.CollVisible)
	title := " Collections "
	if s.CurrentDB != "" {
		title = fmt.Sprintf(" Collections [yellow](%s)[white] ", tview.Escape(s.CurrentDB))
	}
	if s.CollFilterActive || s.CollFilterText != "" {
		title = strings.TrimSuffix(title, " ") + fmt.Sprintf("[yellow][/%s[] ", tview.Escape(s.CollFilterText))
	}
	s.CollList.SetTitle(title)
}

func hasEntry(entries []state.ListEntry, name string) bool {
	for _, e := range entries {
		if e.Name == name {
			return true
		}
	}
	return false
}

// SelectCollectionInList moves the collection list cursor to name.
func SelectCollectionInList(name string) {
	s := state.S
	for i, n := range s.CollVisible {
		if n == name {
			s.CollList.SetCurrentItem(i)
			return
		}
	}
}
