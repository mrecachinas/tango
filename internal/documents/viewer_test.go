package documents

import (
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/charmbracelet/x/ansi"
	"tango/internal/state"
	"tango/internal/tui/jsontree"
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

func TestDocTitle_CompoundIDIsLiteral(t *testing.T) {
	got := DocTitle(bson.D{{Key: "_id", Value: bson.D{{Key: "a", Value: "[red]"}}}}, users, "")
	if !strings.Contains(got, `"[red]"`) {
		t.Errorf("brackets in _id must be preserved: %q", got)
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

func TestTreePreservesBrackets(t *testing.T) {
	tree := jsontree.New()
	tree.SetDoc(bson.D{{Key: "[k]", Value: bson.A{"[red]x"}}})
	tree.SetSize(100, 10)
	text := ansi.Strip(tree.View())
	if !strings.Contains(text, "[k]") || !strings.Contains(text, "[red]x") {
		t.Errorf("brackets were interpreted: %q", text)
	}
}

func TestDocListItemTruncatesAndPreservesBrackets(t *testing.T) {
	long := strings.Repeat("x", 5000)
	item := docListItem(bson.D{{Key: "s", Value: long}, {Key: "t", Value: "[red]"}})
	if len([]rune(item)) > maxListItemLen+10 {
		t.Errorf("item not truncated: %d runes", len([]rune(item)))
	}
	item = docListItem(bson.D{{Key: "tags", Value: bson.A{"x"}}})
	if !strings.Contains(item, `["x"]`) {
		t.Errorf("array brackets must be preserved: %q", item)
	}
}
