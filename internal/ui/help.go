package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tango/internal/state"
)

func setupHelp() {
	s := state.S

	s.HelpView = tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	s.HelpView.SetBorder(true).SetTitle(" Help (? to close) ")
	s.HelpView.SetBackgroundColor(tcell.ColorBlack)
	s.HelpView.SetBorderColor(tcell.ColorDodgerBlue)
	s.HelpView.SetTitleColor(tcell.ColorDodgerBlue)
	helpText := ""
	if s.ReadOnly {
		helpText = "[red]⊘ READ-ONLY MODE — write operations disabled[white]\n\n"
	}
	helpText += `[aqua]━━━ Global ━━━[white]
  Tab/Shift+Tab   Switch panels
  /               Focus query bar (in lists: filter)
  :               Command mode
  r               Refresh current view
  R               Full refresh (reload everything)
  W               Toggle wide mode (full-width documents)
  S               Server stats (auto-refreshing)
  L               Toggle live mode (newest first, auto-refresh)
  ?               This help
  q               Quit

[aqua]━━━ Query Bar ━━━[white]
  {field: value}                      Filter the current collection
  db.c.find({..}, {proj}).sort({..}).limit(n).skip(n)
  db.c.findOne({..})  db.c.countDocuments({..})  db.c.distinct("f", {..})
  db.c.aggregate([..])                Results capped at 10,000; $out/$merge supported
  Shell helpers: ObjectId("…") ISODate("…") NumberLong(…) NumberDecimal("…")
                 UUID("…") /regex/i Timestamp(t, i) MinKey MaxKey
  Enter / Tab / ↑↓                    Execute / autocomplete / history
  Esc                                 Back to panels

[aqua]━━━ Commands ━━━[white]
  :use <db>  :coll <name>  :dbs  :colls   Navigate
  :sort f1 -f2           Sort (prefix - for descending; no fields clears)
  :limit <n>             Page size
  :export json|csv [all] [path]   Export selection/page, or all matching docs
  :watch [interval]      Live mode        :stats   Server stats
  :killop <opid>         Kill an operation (opids listed in stats)
  :indexes  :refresh  :refresh!

[aqua]━━━ Databases / Collections ━━━[white]
  Enter           Select
  / or type       Filter list (all keys go to the filter until Esc/Enter)
  Esc             Clear filter / go back
  I               View indexes (collections)
  D               Drop collection

[aqua]━━━ Documents ━━━[white]
  Enter           Expand document
  Space           Toggle selection      a / A   Select all / none
  n / p           Next / previous page
  d               Delete document (or selected)
  e               Edit in $EDITOR       i       Insert new document
  x / C           Export JSON / CSV (selected or current page)
  s               Sort documents

[aqua]━━━ Document Detail ━━━[white]
  t               Toggle JSON / tree view
  c               Copy to clipboard (shell syntax)
  e               Edit in $EDITOR (shell syntax; types are preserved)
  /               Search in document
  Esc             Back

[aqua]━━━ Indexes ━━━[white]
  Enter           View index details
  X               Drop index
  Esc             Back

[aqua]━━━ Server Stats ━━━[white]
  r               Refresh (including profiler across all databases)
  Esc             Back`
	s.HelpView.SetText(helpText)
}
