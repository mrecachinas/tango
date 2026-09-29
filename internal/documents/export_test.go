package documents

import (
	"bytes"
	"encoding/csv"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestFlattenDoc_SimpleFlat(t *testing.T) {
	keys, got := flattenDoc("", bson.D{{Key: "name", Value: "Alice"}, {Key: "age", Value: 30}})
	if got["name"] != "Alice" || got["age"] != "30" || len(got) != 2 {
		t.Errorf("got %v", got)
	}
	if !reflect.DeepEqual(keys, []string{"name", "age"}) {
		t.Errorf("keys should keep document order, got %v", keys)
	}
}

func TestFlattenDoc_Nested(t *testing.T) {
	_, got := flattenDoc("", bson.D{{Key: "a", Value: bson.D{{Key: "b", Value: bson.D{{Key: "c", Value: int32(1)}}}}}})
	if got["a.b.c"] != "1" || len(got) != 1 {
		t.Errorf("got %v", got)
	}
	_, got = flattenDoc("", bson.D{{Key: "addr", Value: bson.M{"city": "NYC"}}})
	if got["addr.city"] != "NYC" {
		t.Errorf("bson.M nested: got %v", got)
	}
}

func TestFlattenDoc_Arrays(t *testing.T) {
	_, got := flattenDoc("", bson.D{{Key: "tags", Value: bson.A{"a", "b"}}, {Key: "s", Value: []string{"x"}}})
	if got["tags"] != `["a","b"]` {
		t.Errorf("tags = %q", got["tags"])
	}
	if got["s"] != `["x"]` {
		t.Errorf("s = %q", got["s"])
	}
}

func TestFlattenDoc_Types(t *testing.T) {
	oid, _ := primitive.ObjectIDFromHex("65f000000000000000000000")
	_, got := flattenDoc("", bson.D{
		{Key: "_id", Value: oid},
		{Key: "active", Value: true},
		{Key: "score", Value: 3.14},
		{Key: "n", Value: int64(9007199254740993)},
		{Key: "when", Value: primitive.NewDateTimeFromTime(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC))},
		{Key: "x", Value: nil},
	})
	want := map[string]string{
		"_id": "65f000000000000000000000", "active": "true", "score": "3.14",
		"n": "9007199254740993", "when": "2024-01-02T03:04:05.000Z", "x": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func docsIter(docs []bson.D) func() (bson.D, bool, error) {
	i := 0
	return func() (bson.D, bool, error) {
		if i >= len(docs) {
			return nil, false, nil
		}
		i++
		return docs[i-1], true, nil
	}
}

func TestWriteCSV_ColumnsIDFirstThenFirstAppearance(t *testing.T) {
	var buf bytes.Buffer
	n, err := writeCSV(&buf, docsIter([]bson.D{
		{{Key: "b", Value: int32(1)}, {Key: "_id", Value: int32(1)}},
		{{Key: "_id", Value: int32(2)}, {Key: "a", Value: "x,y"}},
	}))
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"_id", "b", "a"}, {"1", "1", ""}, {"2", "", "x,y"}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("got %v want %v", rows, want)
	}
}

func TestWriteJSON_CanonicalExtJSON(t *testing.T) {
	var buf bytes.Buffer
	_, err := writeJSON(&buf, docsIter([]bson.D{{{Key: "n", Value: int64(5)}, {Key: "i", Value: int32(1)}}}))
	if err != nil {
		t.Fatal(err)
	}
	var arr bson.A
	if err := bson.UnmarshalExtJSON(buf.Bytes(), true, &arr); err != nil {
		t.Fatalf("output not valid Extended JSON: %v\n%s", err, buf.String())
	}
	d := arr[0].(bson.D)
	if _, ok := d[0].Value.(int64); !ok || !strings.Contains(buf.String(), "$numberLong") {
		t.Errorf("int64 not preserved: %s", buf.String())
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{"users": "users", "a/b": "a_b", "a\\b": "a_b", "a..b": "a_b", "../../etc": "____etc", "": ""}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
