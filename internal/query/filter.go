package query

import (
	"fmt"
	"strings"

	"tango/internal/state"
)

func FilterDBList() {
	s := state.S
	s.DbList.Clear()
	filter := strings.ToLower(s.DbFilterText)
	for _, item := range s.AllDBItems {
		if filter == "" || strings.Contains(strings.ToLower(state.BareName(item)), filter) {
			s.DbList.AddItem(item, "", 0, nil)
		}
	}
	if s.DbFilterText != "" {
		s.DbList.SetTitle(fmt.Sprintf(" Databases [yellow][/%s] ", s.DbFilterText))
	} else {
		s.DbList.SetTitle(" Databases ")
	}
}

func FilterCollList() {
	s := state.S
	s.CollList.Clear()
	filter := strings.ToLower(s.CollFilterText)
	for _, item := range s.AllCollItems {
		if filter == "" || strings.Contains(strings.ToLower(state.BareName(item)), filter) {
			s.CollList.AddItem(item, "", 0, nil)
		}
	}
	title := " Collections "
	if s.CurrentDB != "" {
		title = fmt.Sprintf(" Collections [yellow](%s)[white] ", s.CurrentDB)
	}
	if s.CollFilterText != "" {
		title = strings.TrimSuffix(title, " ")
		title += fmt.Sprintf("[yellow][/%s] ", s.CollFilterText)
	}
	s.CollList.SetTitle(title)
}
