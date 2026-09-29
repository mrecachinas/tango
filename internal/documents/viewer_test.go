package documents

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/state"
)

var users = state.Namespace{DB: "app", Coll: "users"}

func TestDocTitle_StringID(t *testing.T) {
	got := DocTitle(bson.D{{Key: "_id", Value: "abc123"}}, users, "")
	if !strings.Contains(got, "abc123") || !strings.Contains(got, "users") {
		t.Errorf("got %q", got)
	}
}

func TestDocTitle_ObjectID(t *testing.T) {
	oid := primitive.NewObjectID()
	if got := DocTitle(bson.D{{Key: "_id", Value: oid}}, users, ""); !strings.Contains(got, oid.Hex()) {
		t.Errorf("got %q", got)
	}
}

func TestDocTitle_CompoundIDIsEscaped(t *testing.T) {
	got := DocTitle(bson.D{{Key: "_id", Value: bson.D{{Key: "a", Value: "[red]"}}}}, users, "")
	if strings.Contains(got, `"[red]"`) {
		t.Errorf("markup in _id must be escaped: %q", got)
	}
}

func TestDocTitle_NoID(t *testing.T) {
	got := DocTitle(bson.D{{Key: "foo", Value: "bar"}}, users, "View")
	if !strings.Contains(got, "View") || !strings.Contains(got, "users") {
		t.Errorf("got %q", got)
	}
	if got := DocTitle(bson.D{}, state.Namespace{}, "View"); !strings.Contains(got, "View") {
		t.Errorf("got %q", got)
	}
}

func TestBuildTreeNodesEscapes(t *testing.T) {
	root := tview.NewTreeNode("root")
	BuildTreeNodes(root, "[k]", bson.A{"[red]x"}, true)
	node := root.GetChildren()[0]
	if !strings.Contains(node.GetText(), "[k[]") || !strings.Contains(node.GetText(), "[1 items[]") {
		t.Errorf("array node text not escaped: %q", node.GetText())
	}
	leaf := node.GetChildren()[0].GetText()
	if !strings.Contains(leaf, "[0[]") || !strings.Contains(leaf, `"[red[]x"`) {
		t.Errorf("leaf not escaped: %q", leaf)
	}
}

func TestDocListItemTruncatesAndEscapes(t *testing.T) {
	long := strings.Repeat("x", 5000)
	item := docListItem(bson.D{{Key: "s", Value: long}, {Key: "t", Value: "[red]"}})
	if len([]rune(item)) > maxListItemLen+10 {
		t.Errorf("item not truncated: %d runes", len([]rune(item)))
	}
	item = docListItem(bson.D{{Key: "tags", Value: bson.A{"x"}}})
	if !strings.Contains(item, `["x"[]`) {
		t.Errorf("array brackets must be escaped: %q", item)
	}
}
