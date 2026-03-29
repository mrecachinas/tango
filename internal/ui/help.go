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
	helpText += `[cyan]━━━ Global ━━━[white]
  Tab/Shift+Tab   Switch panels
  /               Focus query bar
  r               Refresh current view
  R               Full refresh (reload everything)
  W               Toggle wide mode (full-width documents)
  S               Server stats
  ?               This help
  q               Quit

[cyan]━━━ Query Bar ━━━[white]
  Enter           Execute query
  Tab             Accept autocomplete
  Esc             Back to panels

[cyan]━━━ Databases ━━━[white]
  Enter           Select database
  Type letters    Filter list
  Esc             Clear filter

[cyan]━━━ Collections ━━━[white]
  Enter           Select collection
  I               View indexes
  D               Drop collection
  Type letters    Filter list
  Esc             Clear filter / go back

[cyan]━━━ Documents ━━━[white]
  Enter           Expand document
  Space           Toggle selection
  a               Select all
  A               Deselect all
  n / p           Next / previous page
  d               Delete document (or selected)
  i               Insert new document
  x               Export as JSON (selected or all)
  C               Export as CSV (selected or all)
  s               Sort documents
  L               Toggle live mode (auto-refresh)

[cyan]━━━ Document Detail ━━━[white]
  t               Toggle JSON / tree view
  c               Copy to clipboard
  e               Edit in $EDITOR
  i               Insert new document
  /               Search in document
  Esc             Back to list

[cyan]━━━ Indexes ━━━[white]
  Enter           View index details
  X               Drop index
  Esc             Back

[cyan]━━━ Server Stats ━━━[white]
  r               Refresh
  Esc             Back`
	s.HelpView.SetText(helpText)
}
