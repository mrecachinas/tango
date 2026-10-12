package tui

import (
	"tango/internal/state"
	"tango/internal/tui/sidebar"
)

type sidebarModel = sidebar.Model
type sidebarSelectDB = sidebar.SelectDBMsg
type sidebarOpenColl = sidebar.OpenCollMsg
type sidebarBack = sidebar.BackMsg

func newSidebar() *sidebar.Model { return sidebar.New() }

func sidebarEntries(entries []state.ListEntry) []sidebar.Entry {
	out := make([]sidebar.Entry, len(entries))
	for i, e := range entries {
		out[i] = sidebar.Entry{Name: e.Name, Info: e.Info}
	}
	return out
}

func sidebarDatabaseEntries(entries []state.ListEntry) []sidebar.Entry {
	out := sidebarEntries(entries)
	for i := range out {
		out[i].Dim = out[i].Name == "admin" || out[i].Name == "config" || out[i].Name == "local"
	}
	return out
}
