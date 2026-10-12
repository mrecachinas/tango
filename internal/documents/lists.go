package documents

import (
	"sync"

	"tango/internal/state"
)

func forEachLimit(n, limit int, fn func(int)) {
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

func RenderDBList() { state.S.SidebarRevision++ }

func RenderCollList() {
	s := state.S
	if s.Collections == nil {
		s.Collections = map[string][]state.ListEntry{}
	}
	if s.CurrentDB != "" {
		s.Collections[s.CurrentDB] = append([]state.ListEntry(nil), s.AllCollItems...)
	}
	s.SidebarRevision++
}

func hasEntry(entries []state.ListEntry, name string) bool {
	for _, e := range entries {
		if e.Name == name {
			return true
		}
	}
	return false
}

func SelectCollectionInList(name string) {
	state.S.SelectedDB, state.S.SelectedColl = state.S.CurrentDB, name
}
