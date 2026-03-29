package documents

import (
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/state"
)

// ---------- colorizeValue ----------

func TestColorizeValue_String(t *testing.T) {
	got := colorizeValue(`"hello"`)
	if !strings.Contains(got, "[green]") {
		t.Errorf("expected green for string, got %q", got)
	}
	if !strings.Contains(got, `"hello"`) {
		t.Errorf("expected value preserved, got %q", got)
	}
}

func TestColorizeValue_Number(t *testing.T) {
	for _, n := range []string{"42", "-3", "3.14"} {
		got := colorizeValue(n)
		if !strings.Contains(got, "[yellow]") {
			t.Errorf("expected yellow for number %q, got %q", n, got)
		}
	}
}

func TestColorizeValue_Boolean(t *testing.T) {
	for _, b := range []string{"true", "false"} {
		got := colorizeValue(b)
		if !strings.Contains(got, "[magenta]") {
			t.Errorf("expected magenta for %q, got %q", b, got)
		}
	}
}

func TestColorizeValue_Null(t *testing.T) {
	got := colorizeValue("null")
	if !strings.Contains(got, "[red]") {
		t.Errorf("expected red for null, got %q", got)
	}
}

func TestColorizeValue_Structural(t *testing.T) {
	for _, ch := range []string{"{", "}", "[", "]", "[]"} {
		got := colorizeValue(ch)
		if !strings.Contains(got, "[white]") {
			t.Errorf("expected white for structural %q, got %q", ch, got)
		}
		if !strings.Contains(got, ch) {
			t.Errorf("structural char %q missing from output %q", ch, got)
		}
	}
}

func TestColorizeValue_TrailingComma(t *testing.T) {
	got := colorizeValue("42,")
	if !strings.HasSuffix(got, ",") {
		t.Errorf("expected trailing comma preserved, got %q", got)
	}
	if !strings.Contains(got, "[yellow]") {
		t.Errorf("expected yellow for number with comma, got %q", got)
	}
}

// ---------- HighlightJSON ----------

func TestHighlightJSON_SimpleObject(t *testing.T) {
	input := `{
  "name": "Alice",
  "age": 30,
  "active": true,
  "deleted": null
}`
	got := HighlightJSON(input)

	// Keys cyan
	if !strings.Contains(got, `[cyan]"name"`) {
		t.Error("expected cyan key for 'name'")
	}
	// String values green
	if !strings.Contains(got, `[green]"Alice"`) {
		t.Error("expected green string value")
	}
	// Numbers yellow
	if !strings.Contains(got, `[yellow]30`) {
		t.Error("expected yellow number")
	}
	// Booleans magenta
	if !strings.Contains(got, `[magenta]true`) {
		t.Error("expected magenta boolean")
	}
	// Null red
	if !strings.Contains(got, `[red]null`) {
		t.Error("expected red null")
	}
}

func TestHighlightJSON_NestedObject(t *testing.T) {
	input := `{
  "outer": {
    "inner": 1
  }
}`
	got := HighlightJSON(input)

	if !strings.Contains(got, `[cyan]"outer"`) {
		t.Error("expected cyan key for 'outer'")
	}
	if !strings.Contains(got, `[cyan]"inner"`) {
		t.Error("expected cyan key for 'inner'")
	}
	if !strings.Contains(got, `[yellow]1`) {
		t.Error("expected yellow number for inner value")
	}
}

func TestHighlightJSON_Array(t *testing.T) {
	input := `{
  "items": [
    1,
    2
  ]
}`
	got := HighlightJSON(input)

	// Array brackets should be present and not mangled into [[]
	if !strings.Contains(got, "[") {
		t.Error("expected [ bracket in output")
	}
	if strings.Contains(got, "[[]") {
		t.Error("array bracket should not be escaped as [[]")
	}
	if !strings.Contains(got, `[yellow]1`) {
		t.Error("expected yellow for array element 1")
	}
}

func TestHighlightJSON_EmptyObject(t *testing.T) {
	got := HighlightJSON("{}")
	if !strings.Contains(got, "{") || !strings.Contains(got, "}") {
		t.Errorf("expected braces preserved, got %q", got)
	}
}

func TestHighlightJSON_StructuralChars(t *testing.T) {
	input := `{
  "a": 1
}`
	got := HighlightJSON(input)
	// Opening and closing braces on their own lines
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], "{") {
		t.Errorf("expected opening brace on first line, got %q", lines[0])
	}
	if !strings.Contains(lines[len(lines)-1], "}") {
		t.Errorf("expected closing brace on last line, got %q", lines[len(lines)-1])
	}
}

func TestHighlightJSON_BooleanFalse(t *testing.T) {
	input := `{
  "ok": false
}`
	got := HighlightJSON(input)
	if !strings.Contains(got, "[magenta]false") {
		t.Errorf("expected magenta false, got %q", got)
	}
}

// ---------- DocTitle ----------

func initState(coll string) {
	state.S = &state.AppState{CurrentColl: coll}
}

func TestDocTitle_StringID(t *testing.T) {
	initState("users")
	doc := bson.M{"_id": "abc123"}
	got := DocTitle(doc, "")
	if !strings.Contains(got, "abc123") {
		t.Errorf("expected string _id in title, got %q", got)
	}
	if !strings.Contains(got, "users") {
		t.Errorf("expected collection name in title, got %q", got)
	}
}

func TestDocTitle_ObjectID(t *testing.T) {
	initState("orders")
	oid := primitive.NewObjectID()
	doc := bson.M{"_id": oid}
	got := DocTitle(doc, "")
	if !strings.Contains(got, oid.Hex()) {
		t.Errorf("expected ObjectID hex in title, got %q", got)
	}
}

func TestDocTitle_NoID(t *testing.T) {
	initState("logs")
	doc := bson.M{"foo": "bar"}
	got := DocTitle(doc, "View")
	if !strings.Contains(got, "View") {
		t.Errorf("expected viewLabel in title, got %q", got)
	}
	if !strings.Contains(got, "logs") {
		t.Errorf("expected collection in fallback title, got %q", got)
	}
}

func TestDocTitle_NoID_NoColl(t *testing.T) {
	initState("")
	doc := bson.M{"foo": "bar"}
	got := DocTitle(doc, "View")
	if !strings.Contains(got, "View") {
		t.Errorf("expected viewLabel in title, got %q", got)
	}
}

func TestDocTitle_WithViewLabel(t *testing.T) {
	initState("")
	doc := bson.M{"_id": "xyz"}
	got := DocTitle(doc, "Detail")
	// When there is an _id but no collection, viewLabel is not used
	if !strings.Contains(got, "xyz") {
		t.Errorf("expected _id in title, got %q", got)
	}
}
