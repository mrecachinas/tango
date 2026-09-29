package documents

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/mjson"
	"tango/internal/state"
)

// DocTitle returns an (escaped) panel title identifying doc by its _id.
func DocTitle(doc bson.D, ns state.Namespace, viewLabel string) string {
	idVal, ok := mjson.Get(doc, "_id")
	if !ok {
		if ns.Coll != "" {
			return fmt.Sprintf(" %s [yellow](%s)[white] ", tview.Escape(viewLabel), tview.Escape(ns.Coll))
		}
		return fmt.Sprintf(" %s ", tview.Escape(viewLabel))
	}
	var idStr string
	switch v := idVal.(type) {
	case primitive.ObjectID:
		idStr = v.Hex()
	case string:
		idStr = v
	default:
		idStr = mjson.Compact(v)
	}
	idStr = tview.Escape(truncate(idStr, 80))
	if ns.Coll != "" {
		return fmt.Sprintf(" %s • %s ", tview.Escape(ns.Coll), idStr)
	}
	return fmt.Sprintf(" %s ", idStr)
}

func ExpandDocument(index int) {
	s := state.S
	if index < 0 || index >= len(s.CurrentDocs) {
		return
	}
	showDocument(s.CurrentDocs[index], s.DocsNS, s.DocsEditable)
}

// showDocument opens doc in the detail view and remembers where it came from.
func showDocument(doc bson.D, ns state.Namespace, editable bool) {
	s := state.S
	state.SetDetail(DocTitle(doc, ns, "Document"), mjson.Tokens(doc, true))
	s.ExpandedDoc = doc
	s.ExpandedNS = ns
	s.ExpandedEditable = editable
	s.DocTree.SetRoot(nil)
}

// CollapseDocument leaves the detail/tree view.
func CollapseDocument() {
	s := state.S
	EndDocSearch(true)
	back := s.DetailBackPage
	if back == "" {
		back = "list"
	}
	s.DocPages.SwitchToPage(back)
	s.App.SetFocus(state.ActiveDocPanel())
	state.RefreshPanelBorders()
}

// ToggleToTree switches from the JSON detail view to a collapsible tree.
func ToggleToTree() {
	s := state.S
	doc := s.ExpandedDoc
	if doc == nil {
		return
	}
	EndDocSearch(true)
	title := DocTitle(doc, s.ExpandedNS, "Document")
	root := tview.NewTreeNode(title).SetColor(tcell.ColorWhite)
	for _, e := range doc {
		BuildTreeNodes(root, e.Key, e.Value, true)
	}
	s.DocTree.SetRoot(root).SetCurrentNode(root)
	s.DocTree.SetTitle(title)
	s.DocPages.SwitchToPage("tree")
	s.App.SetFocus(s.DocTree)
	state.RefreshPanelBorders()
}

// ToggleToDetail switches from the tree view back to the JSON detail view.
func ToggleToDetail() {
	s := state.S
	if s.ExpandedDoc == nil {
		return
	}
	s.DocPages.SwitchToPage("detail")
	s.App.SetFocus(s.DocDetail)
	state.RefreshPanelBorders()
}

func BuildTreeNodes(parent *tview.TreeNode, key string, value interface{}, topLevel bool) {
	k := "[aqua]" + tview.Escape(key) + "[white]: "
	switch v := value.(type) {
	case bson.D:
		node := tview.NewTreeNode(k + tview.Escape(fmt.Sprintf("{%d keys}", len(v)))).SetColor(tcell.ColorWhite)
		node.SetExpanded(topLevel)
		for _, e := range v {
			BuildTreeNodes(node, e.Key, e.Value, false)
		}
		parent.AddChild(node)
	case bson.A:
		node := tview.NewTreeNode(k + tview.Escape(fmt.Sprintf("[%d items]", len(v)))).SetColor(tcell.ColorWhite)
		node.SetExpanded(topLevel)
		for i, item := range v {
			BuildTreeNodes(node, fmt.Sprintf("[%d]", i), item, false)
		}
		parent.AddChild(node)
	default:
		toks := mjson.Tokens(v, false)
		color := tcell.ColorYellow
		tag := "[yellow]"
		if len(toks) == 1 {
			switch toks[0].Kind {
			case mjson.String:
				color, tag = tcell.ColorGreen, "[green]"
			case mjson.Bool:
				color, tag = tcell.ColorFuchsia, "[fuchsia]"
			case mjson.Null:
				color, tag = tcell.ColorRed, "[red]"
			case mjson.Helper:
				color, tag = tcell.ColorOrange, "[orange]"
			}
		}
		node := tview.NewTreeNode(k + tag + tview.Escape(mjson.Join(toks))).SetColor(color)
		parent.AddChild(node)
	}
}
