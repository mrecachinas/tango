package documents

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestFlattenDoc_SimpleFlat(t *testing.T) {
	doc := bson.M{"name": "Alice", "age": 30}
	got := flattenDoc("", doc)

	if got["name"] != "Alice" {
		t.Errorf("name = %q, want %q", got["name"], "Alice")
	}
	if got["age"] != "30" {
		t.Errorf("age = %q, want %q", got["age"], "30")
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestFlattenDoc_Nested(t *testing.T) {
	doc := bson.M{"addr": bson.M{"city": "NYC"}}
	got := flattenDoc("", doc)

	if got["addr.city"] != "NYC" {
		t.Errorf("addr.city = %q, want %q", got["addr.city"], "NYC")
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestFlattenDoc_DeepNesting(t *testing.T) {
	doc := bson.M{"a": bson.M{"b": bson.M{"c": 1}}}
	got := flattenDoc("", doc)

	if got["a.b.c"] != "1" {
		t.Errorf("a.b.c = %q, want %q", got["a.b.c"], "1")
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

func TestFlattenDoc_ArraySlice(t *testing.T) {
	// []string is detected by the "[]" prefix check → JSON marshaled
	doc := bson.M{"tags": []string{"a", "b"}}
	got := flattenDoc("", doc)

	want := `["a","b"]`
	if got["tags"] != want {
		t.Errorf("tags = %q, want %q", got["tags"], want)
	}
}

func TestFlattenDoc_BsonA(t *testing.T) {
	// bson.A (primitive.A) uses fmt.Sprintf since %T is "primitive.A"
	doc := bson.M{"tags": bson.A{"a", "b"}}
	got := flattenDoc("", doc)

	if got["tags"] == "" {
		t.Error("tags should not be empty")
	}
}

func TestFlattenDoc_MixedTypes(t *testing.T) {
	doc := bson.M{
		"name":   "Bob",
		"age":    25,
		"active": true,
		"score":  3.14,
		"addr":   bson.M{"zip": "10001"},
		"tags":   []string{"x"},
	}
	got := flattenDoc("", doc)

	cases := map[string]string{
		"name":     "Bob",
		"age":      "25",
		"active":   "true",
		"score":    "3.14",
		"addr.zip": "10001",
		"tags":     `["x"]`,
	}
	for k, want := range cases {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	if len(got) != len(cases) {
		t.Errorf("len = %d, want %d", len(got), len(cases))
	}
}

func TestFlattenDoc_Empty(t *testing.T) {
	got := flattenDoc("", bson.M{})
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestFlattenDoc_NilValue(t *testing.T) {
	doc := bson.M{"x": nil}
	got := flattenDoc("", doc)

	if got["x"] != "" {
		t.Errorf("x = %q, want empty string", got["x"])
	}
}

func TestSanitizeFilename_Normal(t *testing.T) {
	if got := sanitizeFilename("users"); got != "users" {
		t.Errorf("got %q, want %q", got, "users")
	}
}

func TestSanitizeFilename_ForwardSlash(t *testing.T) {
	if got := sanitizeFilename("a/b"); got != "a_b" {
		t.Errorf("got %q, want %q", got, "a_b")
	}
}

func TestSanitizeFilename_Backslash(t *testing.T) {
	if got := sanitizeFilename("a\\b"); got != "a_b" {
		t.Errorf("got %q, want %q", got, "a_b")
	}
}

func TestSanitizeFilename_DoubleDots(t *testing.T) {
	if got := sanitizeFilename("a..b"); got != "a_b" {
		t.Errorf("got %q, want %q", got, "a_b")
	}
}

func TestSanitizeFilename_PathTraversal(t *testing.T) {
	got := sanitizeFilename("../../etc")
	// ".." → "_", then "/" → "_", so "../../etc" → "__/_/etc" → "____etc"
	if got != "____etc" {
		t.Errorf("got %q, want %q", got, "____etc")
	}
}

func TestSanitizeFilename_Empty(t *testing.T) {
	if got := sanitizeFilename(""); got != "" {
		t.Errorf("got %q, want %q", got, "")
	}
}
