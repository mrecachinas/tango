package tui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/mjson"
	"tango/internal/state"
	"tango/internal/theme"
	"tango/internal/tui/chrome"
	"tango/internal/tui/overlay"
)

func (m *Model) showSidebar() bool {
	if m.sidebarOverride != nil {
		return *m.sidebarOverride
	}
	return !state.S.WideMode && m.width >= 90
}

func (m *Model) resize() {
	m.prompt.SetSize(m.width, 1)
	headerHeight := chrome.HeaderHeight(m.expandedHeader)
	m.bodyHeight = max(0, m.height-headerHeight-1-m.prompt.Height())
	m.sidebarWidth = 0
	if m.showSidebar() {
		m.sidebarWidth = min(max(24, m.width/5), 30, max(0, m.width-16))
	}
	if m.sized && m.sidebarWidth == 0 && state.S.Focus == "sidebar" {
		state.S.Focus = "content"
	}
	w, h := max(0, m.width-m.sidebarWidth-2), max(0, m.bodyHeight-2)
	m.sidebar.SetSize(max(0, m.sidebarWidth-2), h)
	m.table.SetSize(w, h)
	m.tree.SetSize(w, h)
	m.json.SetSize(w, h)
	m.indexes.SetSize(w, h)
	m.stats.SetSize(w, h)
	m.palette.SetSize(m.width, m.height)
	m.help.SetSize(m.width, m.height)
	if m.confirm != nil {
		m.confirm.SetSize(m.width, m.height)
	}
	if m.typeConfirm != nil {
		m.typeConfirm.SetSize(m.width, m.height)
	}
}

func (m *Model) View() tea.View {
	s := state.S
	content := ""
	if m.width < 40 || m.height < 8 || m.bodyHeight < 3 {
		content = fitScreen(theme.Current().Warn.Render("Terminal too small — resize to at least 40×8"), m.width, m.height)
	} else {
		snapshot := m.snapshot
		snapshot.HostLabel, snapshot.ReadOnly = s.HostLabel, s.ReadOnly
		snapshot.DBCount, snapshot.CollectionCount = s.CachedDBCount, s.CachedCollCount
		header := chrome.Header(snapshot, nil, m.width, m.expandedHeader)
		title, info, body := m.content()
		right := theme.Pane(title, info, body, m.width-m.sidebarWidth, m.bodyHeight, s.Focus == "content")
		if m.sidebarWidth > 0 {
			m.sidebar.SetFocused(s.Focus == "sidebar" && m.overlay == "" && !m.prompt.Active())
			left := theme.Pane("Databases", "", m.sidebar.View(), m.sidebarWidth, m.bodyHeight, s.Focus == "sidebar")
			right = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
		}
		flash, isError, loading := state.Status()
		kind := chrome.Info
		if isError {
			kind = chrome.Err
		}
		footer := chrome.Footer(chrome.FooterState{
			Mode: m.mode(), Hints: m.hints(), Flash: flash, FlashKind: kind,
			Loading: loading, Spinner: m.spinner.View(),
		}, m.width)
		parts := []string{header, right}
		if m.prompt.Active() {
			parts = append(parts, m.prompt.View())
		}
		parts = append(parts, footer)
		content = fitScreen(strings.Join(parts, "\n"), m.width, m.height)
		switch m.overlay {
		case "palette":
			content = overlay.Place(content, m.palette.View(), m.width, m.height)
		case "help":
			content = overlay.Place(content, m.help.View(), m.width, m.height)
		case "confirm":
			if m.typeConfirm != nil {
				content = overlay.Place(content, m.typeConfirm.View(), m.width, m.height)
			} else {
				content = overlay.Place(content, m.confirm.View(), m.width, m.height)
			}
		}
	}
	v := tea.NewView(fitScreen(content, m.width, m.height))
	v.AltScreen, v.WindowTitle = true, "tango"
	return v
}

func fitScreen(text string, width, height int) string {
	if height <= 0 {
		return ""
	}
	lines := strings.Split(text, "\n")
	out := make([]string, height)
	for i := range out {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out[i] = theme.FitWidth(line, width)
	}
	return strings.Join(out, "\n")
}

func (m *Model) content() (title, info, body string) {
	s := state.S
	title = "Documents"
	if s.CurrentDB != "" {
		title = s.CurrentDB
	}
	if s.CurrentColl != "" {
		title += " › " + s.CurrentColl
	}
	switch s.View {
	case "tree":
		title = m.detailTitle(title)
		info, body = m.tree.Path(), m.tree.View()
	case "detail":
		title = m.detailTitle(title)
		info, body = m.json.MatchInfo(), m.json.View()
		if s.DocSearchTerm == "" {
			info = fmt.Sprintf("%d lines · %.0f%%", m.json.LineCount(), m.json.ScrollPercent()*100)
		}
	case "indexes":
		title = s.IndexesNS.DB + " › " + s.IndexesNS.Coll + " › Indexes"
		label := "indexes"
		if len(s.CurrentIndexes) == 1 {
			label = "index"
		}
		info, body = fmt.Sprintf("%d %s", len(s.CurrentIndexes), label), m.indexes.View()
		if columns := m.indexes.ColumnInfo(); columns != "" {
			info += " · " + columns
		}
	case "stats":
		title, body = "Server Stats", m.stats.View()
	default:
		info, body = documentInfo(), m.table.View()
		if columns := m.table.ColumnInfo(); columns != "" {
			info += " · " + columns
		}
	}
	title = lineText(title)
	if s.View != "list" {
		info = lineText(info)
	}
	info = ansi.Truncate(info, max(0, (m.width-m.sidebarWidth)/2), "…")
	return title, info, body
}

func (m *Model) detailTitle(title string) string {
	s := state.S
	if s.ExpandedDoc != nil {
		ns := s.ExpandedNS
		if ns.DB != "" && ns.Coll != "" {
			title = ns.DB + " › " + ns.Coll
		}
		id := lineText(s.DetailTitle)
		if value, ok := mjson.Get(s.ExpandedDoc, "_id"); ok {
			if oid, ok := value.(primitive.ObjectID); ok {
				id = "…" + oid.Hex()[18:]
			}
		}
		return title + " › " + id
	}
	if s.DetailBackPage == "indexes" {
		title = s.IndexesNS.DB + " › " + s.IndexesNS.Coll
	}
	return title + " › " + s.DetailTitle
}

func documentInfo() string {
	s := state.S
	info := lineText(state.FormatDocListTitle())
	if info == "" {
		return ""
	}
	t := theme.Current()
	if len(s.CurrentFilter) > 0 || len(s.CurrentProjection) > 0 || s.QuerySkip > 0 || s.QueryLimit > 0 {
		query := lineText(mjson.Compact(s.CurrentFilter))
		if start := strings.Index(info, query); start >= 0 {
			end := start + len(query)
			return t.Muted.Render(info[:start]) + t.Info.Render(query) + t.Muted.Render(info[end:])
		}
	}
	return t.Muted.Render(info)
}

func lineText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(text))
}

func (m *Model) mode() string {
	s := state.S
	if m.overlay == "confirm" {
		return "CONFIRM"
	}
	if m.overlay == "palette" {
		return "COMMAND"
	}
	if m.overlay == "help" {
		return "HELP"
	}
	if m.prompt.Active() {
		return strings.ToUpper(string(m.prompt.Kind()))
	}
	if s.Focus == "sidebar" && m.sidebar.Filtering() {
		return "FILTER"
	}
	if s.LiveMode {
		return "LIVE"
	}
	if s.ReadOnly {
		return "READ-ONLY"
	}
	return "NORMAL"
}

func (m *Model) hints() []chrome.Hint {
	hints := []chrome.Hint{}
	switch {
	case m.overlay == "confirm":
		key := "y/N"
		if m.typeConfirm != nil {
			key = "enter"
		}
		hints = append(hints, chrome.Hint{Key: key, Desc: "confirm"}, chrome.Hint{Key: "esc", Desc: "cancel"})
		return hints
	case m.overlay == "help":
		if m.help.Filtering() || m.help.Filter() != "" {
			return []chrome.Hint{{Key: "↑↓", Desc: "scroll"}, {Key: "enter", Desc: "browse"}, {Key: "esc", Desc: "clear filter"}}
		}
		return []chrome.Hint{{Key: "/", Desc: "filter"}, {Key: "↑↓", Desc: "scroll"}, {Key: "esc", Desc: "close"}}
	case m.overlay == "palette":
		return []chrome.Hint{{Key: "enter", Desc: "run"}, {Key: "↑↓", Desc: "choose"}, {Key: "esc", Desc: "close"}}
	case m.prompt.Active():
		desc := "submit"
		if m.prompt.Kind() == "search" {
			desc = "keep matches"
		}
		hints = append(hints, chrome.Hint{Key: "enter", Desc: desc}, chrome.Hint{Key: "esc", Desc: "cancel"})
		if m.prompt.Kind() != "search" {
			hints = append(hints, chrome.Hint{Key: "tab", Desc: "complete"}, chrome.Hint{Key: "↑↓", Desc: "history"})
		}
		return hints
	case state.S.Focus == "sidebar" && m.sidebar.Filtering():
		return []chrome.Hint{{Key: "enter", Desc: "open"}, {Key: "esc", Desc: "clear"}}
	case state.S.Focus == "sidebar":
		hints = append(hints, chrome.Hint{Key: "enter", Desc: "open"}, chrome.Hint{Key: "/", Desc: "filter"}, chrome.Hint{Key: "tab", Desc: "content"}, chrome.Hint{Key: "I", Desc: "indexes"})
	case state.S.View == "tree" || state.S.View == "detail":
		hints = append(hints, chrome.Hint{Key: "/", Desc: "search"})
		if state.S.ExpandedDoc != nil {
			hints = append(hints, chrome.Hint{Key: "t", Desc: "json/tree"}, chrome.Hint{Key: "c", Desc: "copy"})
		}
		hints = append(hints, chrome.Hint{Key: "esc", Desc: "back"})
	case state.S.View == "indexes":
		hints = append(hints, chrome.Hint{Key: "enter", Desc: "details"}, chrome.Hint{Key: "X", Desc: "drop"}, chrome.Hint{Key: "esc", Desc: "back"})
	case state.S.View == "stats":
		hints = append(hints, chrome.Hint{Key: "↑↓", Desc: "scroll"}, chrome.Hint{Key: "r", Desc: "refresh"}, chrome.Hint{Key: ":", Desc: "commands"}, chrome.Hint{Key: "esc", Desc: "back"})
	default:
		hints = append(hints, chrome.Hint{Key: "enter", Desc: "open"}, chrome.Hint{Key: "space", Desc: "select"}, chrome.Hint{Key: "/", Desc: "query"}, chrome.Hint{Key: ":", Desc: "cmd"}, chrome.Hint{Key: "v", Desc: "table/json"})
	}
	if state.S.LiveMode {
		hints = append([]chrome.Hint{{Key: "L", Desc: "stop live"}}, hints...)
	}
	return append(hints, chrome.Hint{Key: "?", Desc: "more"})
}
