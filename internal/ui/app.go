package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tango/internal/documents"
	"tango/internal/query"
	"tango/internal/state"
)

func BuildUI() {
	s := state.S

	// k9s-inspired dark color scheme
	activeBorder := tcell.ColorDodgerBlue
	inactiveBorder := tcell.ColorDimGray
	inactiveTitle := tcell.ColorGray

	// Metrics bar
	s.MetricsBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	s.MetricsBar.SetBorder(true).SetTitle(" 🥭 tango ")
	s.MetricsBar.SetBorderColor(tcell.ColorSteelBlue)
	s.MetricsBar.SetTitleColor(tcell.ColorOrange)
	s.MetricsBar.SetBackgroundColor(tcell.ColorBlack)
	s.MetricsBar.SetText("[gray]connecting...")

	// Search bar with autocomplete
	s.SearchBar = tview.NewInputField().
		SetLabel(" ▶ ").
		SetFieldWidth(0).
		SetAutocompleteFunc(query.GetAutocompleteSuggestions).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				query.ExecuteQuery(s.SearchBar.GetText())
			}
			if key == tcell.KeyEscape {
				s.App.SetFocus(s.DocList)
				state.RefreshPanelBorders()
			}
		})
	s.SearchBar.SetBorder(true).SetTitle(" Query (db.collection.find({...})) ")
	s.SearchBar.SetBorderColor(inactiveBorder)
	s.SearchBar.SetTitleColor(tcell.ColorGold)
	s.SearchBar.SetFieldBackgroundColor(tcell.ColorBlack)
	s.SearchBar.SetChangedFunc(func(text string) {
		if strings.HasPrefix(text, ":") {
			s.SearchBar.SetTitle(" Command ")
		} else {
			s.SearchBar.SetTitle(" Query (db.collection.find({...})) ")
		}
	})
	s.SearchBar.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyUp:
			if len(s.QueryHistory) > 0 {
				if s.QueryHistoryIdx < 0 {
					s.QueryHistoryIdx = len(s.QueryHistory) - 1
				} else if s.QueryHistoryIdx > 0 {
					s.QueryHistoryIdx--
				}
				s.SearchBar.SetText(s.QueryHistory[s.QueryHistoryIdx])
			}
			return nil
		case tcell.KeyDown:
			if s.QueryHistoryIdx >= 0 {
				s.QueryHistoryIdx++
				if s.QueryHistoryIdx >= len(s.QueryHistory) {
					s.QueryHistoryIdx = -1
					s.SearchBar.SetText("")
				} else {
					s.SearchBar.SetText(s.QueryHistory[s.QueryHistoryIdx])
				}
			}
			return nil
		}
		return event
	})

	// Databases panel (left)
	s.DbList = tview.NewList().ShowSecondaryText(false)
	s.DbList.SetBorder(true).SetTitle(" Databases ")
	s.DbList.SetMainTextColor(tcell.ColorWhite)
	s.DbList.SetBorderColor(activeBorder)
	s.DbList.SetTitleColor(activeBorder)
	s.DbList.SetBackgroundColor(tcell.ColorBlack)
	s.DbList.SetSelectedFunc(func(i int, mainText string, secondary string, shortcut rune) {
		if idx := strings.Index(mainText, " [gray]"); idx != -1 {
			mainText = mainText[:idx]
		}
		documents.SelectDatabase(mainText)
	})

	// Collections panel (middle)
	s.CollList = tview.NewList().ShowSecondaryText(false)
	s.CollList.SetBorder(true).SetTitle(" Collections ")
	s.CollList.SetBorderColor(inactiveBorder)
	s.CollList.SetTitleColor(inactiveTitle)
	s.CollList.SetBackgroundColor(tcell.ColorBlack)
	s.CollList.SetSelectedFunc(func(i int, mainText string, secondary string, shortcut rune) {
		if idx := strings.Index(mainText, " [gray]"); idx != -1 {
			mainText = mainText[:idx]
		}
		documents.SelectCollection(mainText)
	})
	s.CollList.SetDoneFunc(func() {
		if s.WideMode {
			// In wide mode, Esc backs up to database list in the doc panel
			s.CurrentDB = ""
			s.CurrentColl = ""
			s.CurrentDocs = nil
			s.DocList.Clear()
			s.DocDetail.Clear()
			s.CollList.Clear()
			s.DocPages.SwitchToPage("databases")
			s.App.SetFocus(s.DbList)
		} else {
			s.CollList.Clear()
			state.ClearDocPanel()
			s.CurrentColl = ""
			s.App.SetFocus(s.DbList)
		}
		state.RefreshPanelBorders()
	})

	// Documents list (compact one-line JSON per record)
	s.DocList = tview.NewList().ShowSecondaryText(false)
	s.DocList.SetBorder(true).SetTitle(" Documents ")
	s.DocList.SetBorderColor(inactiveBorder)
	s.DocList.SetTitleColor(inactiveTitle)
	s.DocList.SetBackgroundColor(tcell.ColorBlack)
	s.DocList.SetSelectedFunc(func(i int, _ string, _ string, _ rune) {
		documents.ExpandDocument(i)
	})
	s.DocList.SetDoneFunc(func() {
		if s.WideMode {
			// In wide mode, Esc backs up to collection list in the doc panel
			s.DocPages.SwitchToPage("collections")
			s.App.SetFocus(s.CollList)
			s.CurrentColl = ""
		} else {
			s.App.SetFocus(s.CollList)
		}
		state.RefreshPanelBorders()
	})

	// Document detail view (expanded pretty-printed JSON)
	s.DocDetail = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true)
	s.DocDetail.SetBorder(true).SetTitle(" Document Detail ")
	s.DocDetail.SetBorderColor(inactiveBorder)
	s.DocDetail.SetTitleColor(inactiveTitle)
	s.DocDetail.SetBackgroundColor(tcell.ColorBlack)

	// Search bar for document detail text search
	s.DocSearchBar = tview.NewInputField().
		SetLabel(" Search: ").
		SetFieldWidth(0)
	s.DocSearchBar.SetFieldBackgroundColor(tcell.ColorBlack)
	s.DocSearchBar.SetBackgroundColor(tcell.ColorDarkSlateGray)
	s.DocSearchBar.SetLabelColor(tcell.ColorYellow)
	s.DocSearchBar.SetChangedFunc(func(text string) {
		documents.ApplyDocSearchHighlight(text)
	})
	s.DocSearchBar.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter || key == tcell.KeyEscape {
			clearSearch := key == tcell.KeyEscape
			documents.EndDocSearch(clearSearch)
		}
	})

	// Flex wrapper: docDetail + hidden search bar
	s.DocDetailFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.DocDetail, 0, 1, true)

	// Tree view for collapsible JSON document browsing
	s.DocTree = tview.NewTreeView()
	s.DocTree.SetBorder(true).SetTitle(" Document Tree ")
	s.DocTree.SetBorderColor(inactiveBorder)
	s.DocTree.SetTitleColor(inactiveTitle)
	s.DocTree.SetBackgroundColor(tcell.ColorBlack)
	s.DocTree.SetGraphicsColor(tcell.ColorDimGray)

	// Pages widget to swap between list, detail, tree, and wide-mode navigation views
	s.DocPages = tview.NewPages().
		AddPage("list", s.DocList, true, true).
		AddPage("detail", s.DocDetailFlex, true, false).
		AddPage("tree", s.DocTree, true, false).
		AddPage("databases", s.DbList, true, false).
		AddPage("collections", s.CollList, true, false)

	// Index list (shown inside docPages)
	s.IndexList = tview.NewList().ShowSecondaryText(true)
	s.IndexList.SetBorder(true).SetTitle(" Indexes ")
	s.IndexList.SetBorderColor(inactiveBorder)
	s.IndexList.SetTitleColor(inactiveTitle)
	s.IndexList.SetBackgroundColor(tcell.ColorBlack)
	s.IndexList.SetDoneFunc(func() {
		s.DocPages.SwitchToPage("list")
		s.App.SetFocus(s.CollList)
		state.RefreshPanelBorders()
	})
	s.IndexList.SetSelectedFunc(func(i int, _ string, _ string, _ rune) {
		if i < 0 || i >= len(s.CurrentIndexes) {
			return
		}
		jsonBytes, err := json.MarshalIndent(s.CurrentIndexes[i], "", "  ")
		if err != nil {
			s.DocDetail.SetText(fmt.Sprintf("[red]Error formatting index: %s", err))
		} else {
			s.DocDetail.SetText(documents.HighlightJSON(string(jsonBytes)))
			s.DocDetail.SetTitle(fmt.Sprintf(" Index Detail [yellow](%v)[white] ", s.CurrentIndexes[i]["name"]))
		}
		s.DocDetail.ScrollToBeginning()
		s.DocPages.SwitchToPage("detail")
		s.App.SetFocus(s.DocDetail)
		state.RefreshPanelBorders()
	})
	s.DocPages.AddPage("indexes", s.IndexList, true, false)

	// Server stats view
	s.StatsView = tview.NewTextView().SetDynamicColors(true).SetScrollable(true).SetWrap(true)
	s.StatsView.SetBorder(true).SetTitle(" Server Stats ")
	s.StatsView.SetBackgroundColor(tcell.ColorBlack)
	s.StatsView.SetBorderColor(tcell.ColorDimGray)
	s.StatsView.SetTitleColor(tcell.ColorGray)
	s.DocPages.AddPage("stats", s.StatsView, true, false)

	// Status bar
	s.StatusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	s.StatusBar.SetBackgroundColor(tcell.ColorDarkSlateGray)
	state.UpdateStatusHints()

	// Layout: search bar on top, 3-panel row, status bar on bottom
	s.PanelsFlex = tview.NewFlex().
		AddItem(s.DbList, 0, 1, true).
		AddItem(s.CollList, 0, 1, false).
		AddItem(s.DocPages, 0, 3, false)
	panels := s.PanelsFlex

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(s.MetricsBar, 6, 0, false).
		AddItem(s.SearchBar, 3, 0, false).
		AddItem(panels, 0, 1, true).
		AddItem(s.StatusBar, 1, 0, false)

	// Modals
	s.ConfirmModal = tview.NewModal()
	s.DropModal = tview.NewModal()

	// Help overlay
	setupHelp()

	// Sort form
	s.SortForm = tview.NewForm()
	s.SortForm.SetBorder(true).SetTitle(" Sort Documents ")
	s.SortForm.SetBorderColor(tcell.ColorDodgerBlue)
	s.SortForm.SetTitleColor(tcell.ColorDodgerBlue)
	s.SortForm.SetBackgroundColor(tcell.ColorBlack)
	s.SortForm.SetFieldBackgroundColor(tcell.ColorDarkSlateGray)
	s.SortForm.SetButtonBackgroundColor(tcell.ColorDodgerBlue)

	sortFormFlex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(s.SortForm, 13, 0, true).
			AddItem(nil, 0, 1, false), 50, 0, true).
		AddItem(nil, 0, 1, false)

	s.RootPages = tview.NewPages().
		AddPage("main", layout, true, true).
		AddPage("confirm", s.ConfirmModal, true, false).
		AddPage("drop-confirm", s.DropModal, true, false).
		AddPage("help", s.HelpView, true, false).
		AddPage("sort", sortFormFlex, true, false)

	// Wire up all keybindings (in keybindings.go)
	setupKeybindings()

	// Apply wide mode if persisted from previous session
	if s.WideMode {
		applyWideMode()
	}

	s.App.SetRoot(s.RootPages, true)
}
