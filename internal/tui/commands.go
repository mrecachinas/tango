package tui

import (
	"tango/internal/state"
	"tango/internal/tui/overlay"
	"tango/internal/tui/palette"
)

func (m *Model) commands() []palette.Item {
	items := []palette.Item{
		{Kind: palette.Command, Title: "Databases", Hint: ":dbs", Value: ":dbs"},
		{Kind: palette.Command, Title: "Collections", Hint: ":colls", Value: ":colls"},
		{Kind: palette.Command, Title: "Indexes", Hint: "I", Value: ":indexes"},
		{Kind: palette.Command, Title: "Server stats", Hint: "S", Value: ":stats"},
		{Kind: palette.Command, Title: "Sort", Hint: "s · field -other / field -1", Value: ":sort "},
		{Kind: palette.Command, Title: "Page size", Hint: ":limit n", Value: ":limit "},
		{Kind: palette.Command, Title: "Export JSON", Hint: "x · [all] [path]", Value: ":export json"},
		{Kind: palette.Command, Title: "Export CSV", Hint: "C · [all] [path]", Value: ":export csv"},
		{Kind: palette.Command, Title: "Export all JSON", Value: ":export json all"},
		{Kind: palette.Command, Title: "Export all CSV", Value: ":export csv all"},
		{Kind: palette.Command, Title: "Live watch", Hint: "L · [interval]", Value: ":watch"},
		{Kind: palette.Command, Title: "Kill operation", Value: ":killop "},
		{Kind: palette.Command, Title: "Use database", Value: ":db "},
		{Kind: palette.Command, Title: "Use collection", Value: ":coll "},
		{Kind: palette.Command, Title: "Refresh", Hint: "r", Value: ":refresh"},
		{Kind: palette.Command, Title: "Full refresh", Hint: "R", Value: ":refresh!"},
		{Kind: palette.Command, Title: "Drop database", Value: ":drop database"},
		{Kind: palette.Command, Title: "Quit", Hint: "q · ctrl+c", Value: ":q"},
	}
	for _, item := range []palette.Item{
		{Title: "Query", Hint: "/", Value: "query"},
		{Title: "Help", Hint: "?", Value: "help"},
		{Title: "Toggle sidebar", Hint: "W", Value: "sidebar"},
		{Title: "Expand metrics", Hint: "M", Value: "header"},
		{Title: "Table / JSON rows", Hint: "v", Value: "table"},
		{Title: "Delete document", Hint: "d", Value: "delete"},
		{Title: "Edit document", Hint: "e", Value: "edit"},
		{Title: "Insert document", Hint: "i", Value: "insert"},
		{Title: "Drop collection", Hint: "D", Value: "drop-collection"},
		{Title: "Drop index", Hint: "X", Value: "drop-index"},
		{Title: "Copy document", Hint: "c", Value: "copy"},
		{Title: "Select all", Hint: "a", Value: "all"},
		{Title: "Clear selection", Hint: "A", Value: "none"},
		{Title: "Next page", Hint: "n", Value: "next"},
		{Title: "Previous page", Hint: "p", Value: "previous"},
		{Title: "JSON / tree detail", Hint: "t", Value: "tree"},
	} {
		item.Kind = palette.Action
		items = append(items, item)
	}
	for _, e := range state.S.AllDBItems {
		items = append(items, palette.Item{Kind: palette.Database, Title: e.Name, Hint: "database", Value: ":db " + e.Name})
	}
	for _, e := range state.S.AllCollItems {
		items = append(items, palette.Item{Kind: palette.Collection, Title: e.Name, Hint: state.S.CurrentDB, Value: ":coll " + e.Name})
	}
	return items
}

func helpSections() []overlay.Section {
	return []overlay.Section{
		{Title: "Navigation", Bindings: []overlay.Binding{
			{Keys: "Tab / Shift+Tab", Desc: "Sidebar ↔ content"},
			{Keys: "j/k · ↑/↓ · g/G", Desc: "Navigate · first/last"},
			{Keys: "ctrl+d/u · pgdown/up", Desc: "Half/full page"},
			{Keys: "h/l · ←/→", Desc: "Tree parent/open · table columns"},
			{Keys: "Enter / Esc", Desc: "Open / back"},
			{Keys: "/ or type (sidebar)", Desc: "Fuzzy filter; Esc clears"},
			{Keys: "I / D (sidebar)", Desc: "Indexes / type name to drop"},
		}},
		{Title: "Global", Bindings: []overlay.Binding{
			{Keys: ": / ?", Desc: "Command palette / help"},
			{Keys: "r / R", Desc: "Refresh / full refresh"},
			{Keys: "W / M", Desc: "Toggle sidebar / expanded metrics"},
			{Keys: "S / L", Desc: "Server stats / live watch"},
			{Keys: "q / ctrl+c", Desc: "Quit (ctrl+c always quits)"},
		}},
		{Title: "Documents", Bindings: []overlay.Binding{
			{Keys: "/ (list)", Desc: "Query: {field: value} or db.coll.find(…)"},
			{Keys: "Enter · v", Desc: "Browse document tree · table/JSON rows"},
			{Keys: "Space · a/A", Desc: "Select + advance · select all/none"},
			{Keys: "n / p · s", Desc: "Next/previous page · :sort prompt"},
			{Keys: "d / e / i", Desc: "Delete (y/N) / $EDITOR edit / insert"},
			{Keys: "x / C", Desc: "Export selection/page as JSON / CSV"},
			{Keys: "t / c / / (detail)", Desc: "JSON/tree · copy · search"},
			{Keys: "n / N (JSON search)", Desc: "Next / previous match"},
			{Keys: "Enter / X (indexes)", Desc: "Details / drop index"},
		}},
		{Title: "Commands & queries", Bindings: []overlay.Binding{
			{Keys: ":db name · :coll name", Desc: "Navigate (also :use, :dbs, :colls)"},
			{Keys: ":sort field -other", Desc: "Sort; field -1 also works; no fields clears"},
			{Keys: ":limit n", Desc: "Page size (1–10,000)"},
			{Keys: ":export json|csv [all] [path]", Desc: "Export selection/page or stream matching docs"},
			{Keys: ":watch [2s] · :stats", Desc: "Live watch / server stats"},
			{Keys: ":killop opid", Desc: "Kill operation listed in stats"},
			{Keys: ":drop database", Desc: "Type name to confirm; cannot be undone"},
			{Keys: "db.c.find({}).sort({}).limit(n).skip(n)", Desc: "Mongo shell syntax + autocomplete/history"},
			{Keys: "findOne / countDocuments / distinct", Desc: "Ad-hoc results (writes target original namespace)"},
			{Keys: "aggregate([…])", Desc: "10,000 result cap; $out/$merge supported"},
			{Keys: "ObjectId / ISODate / NumberLong / UUID", Desc: "Shell types preserved when editing"},
		}},
	}
}
