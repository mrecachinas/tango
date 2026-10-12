package documents

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
)

func TestIndexRowsKeepStableColumnsAndOptions(t *testing.T) {
	specs := []bson.D{
		{{Key: "v", Value: 2}, {Key: "name", Value: "_id_"}, {Key: "key", Value: bson.D{{Key: "_id", Value: 1}}}},
		{
			{Key: "name", Value: "expires"}, {Key: "key", Value: bson.D{{Key: "ts", Value: -1}, {Key: "type", Value: 1}}},
			{Key: "unique", Value: true}, {Key: "sparse", Value: true}, {Key: "expireAfterSeconds", Value: 60},
			{Key: "partialFilterExpression", Value: bson.D{{Key: "active", Value: true}}},
			{Key: "size", Value: 4096}, {Key: "hidden", Value: true},
			{Key: "collation", Value: bson.D{{Key: "locale", Value: "en"}}},
		},
	}
	rows := IndexRows(specs)
	want := []string{"name", "keys", "unique", "sparse", "ttl", "partial", "size"}
	var columns []string
	for _, e := range rows[0][:7] {
		columns = append(columns, e.Key)
	}
	if !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns=%v want=%v", columns, want)
	}
	for _, tc := range []struct {
		row   int
		key   string
		value any
	}{
		{0, "unique", true}, {0, "sparse", false}, {0, "ttl", ""}, {0, "v", nil},
		{1, "keys", `{"ts": -1, "type": 1}`}, {1, "ttl", 60},
		{1, "partial", `{"active": true}`}, {1, "size", 4096}, {1, "hidden", true},
		{1, "collation", `{"locale": "en"}`},
	} {
		if got, _ := mjson.Get(rows[tc.row], tc.key); !reflect.DeepEqual(got, tc.value) {
			t.Errorf("row %d %s=%v want=%v", tc.row, tc.key, got, tc.value)
		}
	}
	if _, present := mjson.Get(specs[0], "unique"); present {
		t.Fatal("table conversion mutated source specification")
	}
}
