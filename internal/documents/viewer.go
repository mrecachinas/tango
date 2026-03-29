package documents

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/state"
)

// DocTitle returns a title string using the document's _id value.
func DocTitle(doc bson.M, viewLabel string) string {
	s := state.S
	idVal, ok := doc["_id"]
	if !ok {
		if s.CurrentColl != "" {
			return fmt.Sprintf(" %s [yellow](%s)[white] ", viewLabel, s.CurrentColl)
		}
		return fmt.Sprintf(" %s ", viewLabel)
	}
	var idStr string
	switch v := idVal.(type) {
	case primitive.ObjectID:
		idStr = v.Hex()
	default:
		idStr = fmt.Sprintf("%v", v)
	}
	if s.CurrentColl != "" {
		return fmt.Sprintf(" %s • %s ", s.CurrentColl, idStr)
	}
	return fmt.Sprintf(" %s ", idStr)
}

func ExpandDocument(index int) {
	s := state.S
	if index < 0 || index >= len(s.CurrentDocs) {
		return
	}

	doc := s.CurrentDocs[index]
	title := DocTitle(doc, "")

	// Prepare tree view in background
	rootLabel := title
	root := tview.NewTreeNode(rootLabel).SetColor(tcell.ColorWhite)

	// Sort top-level keys for consistent display
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		BuildTreeNodes(root, k, doc[k], true)
	}

	s.DocTree.SetRoot(root).SetCurrentNode(root)
	s.DocTree.SetTitle(title)

	// Default to JSON detail view
	prettyBytes, _ := json.MarshalIndent(doc, "", "  ")
	s.DocDetail.SetText(HighlightJSON(string(prettyBytes)))
	s.DocDetail.SetTitle(title)
	s.DocDetail.ScrollToBeginning()
	s.DocPages.SwitchToPage("detail")
	s.App.SetFocus(s.DocDetail)
	state.RefreshPanelBorders()
}

func CollapseDocument() {
	s := state.S
	EndDocSearch(true)
	s.DocPages.SwitchToPage("list")
	s.App.SetFocus(s.DocList)
	state.RefreshPanelBorders()
}

// ToggleToTree switches from JSON detail view to tree view.
func ToggleToTree() {
	s := state.S
	idx := s.DocList.GetCurrentItem()
	if idx < 0 || idx >= len(s.CurrentDocs) {
		return
	}
	doc := s.CurrentDocs[idx]
	title := DocTitle(doc, "")
	root := tview.NewTreeNode(title).SetColor(tcell.ColorWhite)
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		BuildTreeNodes(root, k, doc[k], true)
	}
	s.DocTree.SetRoot(root).SetCurrentNode(root)
	s.DocTree.SetTitle(title)
	s.DocPages.SwitchToPage("tree")
	s.App.SetFocus(s.DocTree)
	state.RefreshPanelBorders()
}

// ToggleToDetail switches from tree view to JSON detail view.
func ToggleToDetail() {
	s := state.S
	idx := s.DocList.GetCurrentItem()
	if idx < 0 || idx >= len(s.CurrentDocs) {
		return
	}
	doc := s.CurrentDocs[idx]
	title := DocTitle(doc, "")
	prettyBytes, _ := json.MarshalIndent(doc, "", "  ")
	s.DocDetail.SetText(HighlightJSON(string(prettyBytes)))
	s.DocDetail.SetTitle(title)
	s.DocDetail.ScrollToBeginning()
	s.DocPages.SwitchToPage("detail")
	s.App.SetFocus(s.DocDetail)
	state.RefreshPanelBorders()
}

func BuildTreeNodes(parent *tview.TreeNode, key string, value interface{}, topLevel bool) {
	switch v := value.(type) {
	case bson.M:
		label := fmt.Sprintf("[cyan]%s[white]: {%d keys}", key, len(v))
		node := tview.NewTreeNode(label).SetColor(tcell.ColorWhite)
		node.SetExpanded(topLevel)
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			BuildTreeNodes(node, k, v[k], false)
		}
		parent.AddChild(node)
	case bson.A:
		label := fmt.Sprintf("[cyan]%s[white]: [%d items]", key, len(v))
		node := tview.NewTreeNode(label).SetColor(tcell.ColorWhite)
		node.SetExpanded(topLevel)
		for i, item := range v {
			BuildTreeNodes(node, fmt.Sprintf("[%d]", i), item, false)
		}
		parent.AddChild(node)
	case []interface{}:
		label := fmt.Sprintf("[cyan]%s[white]: [%d items]", key, len(v))
		node := tview.NewTreeNode(label).SetColor(tcell.ColorWhite)
		node.SetExpanded(topLevel)
		for i, item := range v {
			BuildTreeNodes(node, fmt.Sprintf("[[]%d]", i), item, false)
		}
		parent.AddChild(node)
	case string:
		node := tview.NewTreeNode(fmt.Sprintf("[cyan]%s[white]: [green]\"%s\"", key, tview.Escape(v))).
			SetColor(tcell.ColorGreen)
		parent.AddChild(node)
	case bool:
		node := tview.NewTreeNode(fmt.Sprintf("[cyan]%s[white]: [magenta]%v", key, v)).
			SetColor(tcell.ColorFuchsia)
		parent.AddChild(node)
	case nil:
		node := tview.NewTreeNode(fmt.Sprintf("[cyan]%s[white]: [red]null", key)).
			SetColor(tcell.ColorRed)
		parent.AddChild(node)
	case int, int32, int64, float32, float64:
		node := tview.NewTreeNode(fmt.Sprintf("[cyan]%s[white]: [yellow]%v", key, v)).
			SetColor(tcell.ColorYellow)
		parent.AddChild(node)
	default:
		// Fallback for other BSON types (ObjectID, DateTime, etc.)
		node := tview.NewTreeNode(fmt.Sprintf("[cyan]%s[white]: [yellow]%v", key, v)).
			SetColor(tcell.ColorYellow)
		parent.AddChild(node)
	}
}

func HighlightJSON(jsonStr string) string {
	lines := strings.Split(jsonStr, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := line[:len(line)-len(trimmed)]

		highlighted := colorizeJSONLine(trimmed)
		lines[i] = indent + highlighted
	}
	return strings.Join(lines, "\n")
}

func colorizeJSONLine(trimmed string) string {
	// Pure structural lines
	if trimmed == "{" || trimmed == "}," || trimmed == "}" ||
		trimmed == "[" || trimmed == "[," ||
		trimmed == "[]" || trimmed == "[]," ||
		trimmed == "]" || trimmed == "]," {
		return "[white]" + trimmed + "[white]"
	}

	// Lines starting with a key: "key": value
	if len(trimmed) > 0 && trimmed[0] == '"' {
		colonIdx := findColon(trimmed)
		if colonIdx > 0 {
			key := trimmed[:colonIdx]
			rest := trimmed[colonIdx:]
			// rest starts with ": "
			if len(rest) >= 2 {
				value := strings.TrimSpace(rest[1:])
				return "[cyan]" + key + "[white]:" + " " + colorizeValue(value)
			}
			return "[cyan]" + key + "[white]" + rest
		}
		// Bare string in array
		return colorizeValue(trimmed)
	}

	// Bare values in arrays (numbers, booleans, null)
	return colorizeValue(trimmed)
}

func findColon(s string) int {
	// Find the colon that separates key from value, skipping the key string
	inString := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' && (i == 0 || s[i-1] != '\\') {
			inString = !inString
		} else if ch == ':' && !inString {
			return i
		}
	}
	return -1
}

func colorizeValue(v string) string {
	trailing := ""
	core := v
	if strings.HasSuffix(core, ",") {
		trailing = ","
		core = core[:len(core)-1]
	}

	switch {
	case core == "null":
		return "[red]null[white]" + trailing
	case core == "true" || core == "false":
		return "[magenta]" + core + "[white]" + trailing
	case len(core) > 0 && core[0] == '"':
		return "[green]" + core + "[white]" + trailing
	case core == "{" || core == "}" || core == "[" || core == "[]" || core == "]":
		return "[white]" + core + "[white]" + trailing
	case len(core) > 0 && (core[0] >= '0' && core[0] <= '9' || core[0] == '-'):
		return "[yellow]" + core + "[white]" + trailing
	default:
		return v
	}
}
