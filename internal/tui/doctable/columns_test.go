package doctable

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestDeriveColumns(t *testing.T) {
	tests := []struct {
		name string
		docs []bson.D
		want []string
	}{
		{
			name: "first appearance with id moved first",
			docs: []bson.D{
				{{Key: "b", Value: 1}, {Key: "a", Value: 2}},
				{{Key: "c", Value: 3}, {Key: "_id", Value: primitive.NewObjectID()}, {Key: "b", Value: 4}},
			},
			want: []string{"_id", "b", "a", "c"},
		},
		{
			name: "small documents flatten one level",
			docs: []bson.D{{
				{Key: "metadata", Value: bson.D{
					{Key: "browser", Value: "Chrome"},
					{Key: "os", Value: "Android"},
					{Key: "deep", Value: bson.D{{Key: "inner", Value: 1}}},
				}},
				{Key: "page", Value: "/"},
			}},
			want: []string{"metadata.browser", "metadata.os", "metadata.deep", "page"},
		},
		{
			name: "six keys flatten",
			docs: []bson.D{{{Key: "obj", Value: numberedDoc(6)}}},
			want: []string{"obj.a", "obj.b", "obj.c", "obj.d", "obj.e", "obj.f"},
		},
		{
			name: "large document empty document and arrays are columns",
			docs: []bson.D{{
				{Key: "large", Value: numberedDoc(7)},
				{Key: "empty", Value: bson.D{}},
				{Key: "array", Value: bson.A{bson.D{{Key: "inside", Value: true}}, 2}},
			}},
			want: []string{"large", "empty", "array"},
		},
		{
			name: "mixed shapes preserve parent and children",
			docs: []bson.D{
				{{Key: "obj", Value: bson.D{{Key: "child", Value: 1}}}},
				{{Key: "obj", Value: numberedDoc(7)}},
				{{Key: "obj", Value: 2}, {Key: "later", Value: true}},
			},
			want: []string{"obj.child", "obj", "later"},
		},
		{
			name: "map subdocuments sorted deterministically",
			docs: []bson.D{{{Key: "obj", Value: bson.M{"z": 1, "a": 2}}}},
			want: []string{"obj.a", "obj.z"},
		},
		{
			name: "id remains a parent even for document values",
			docs: []bson.D{{{Key: "other", Value: 1}, {Key: "_id", Value: bson.D{{Key: "a", Value: 2}}}}},
			want: []string{"_id", "other"},
		},
		{name: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := derivePage(tt.docs)
			if !reflect.DeepEqual(p.columns, tt.want) {
				t.Fatalf("columns = %v, want %v", p.columns, tt.want)
			}
			if len(p.rows) != len(tt.docs) {
				t.Fatalf("rows = %d, want %d", len(p.rows), len(tt.docs))
			}
		})
	}
}

func numberedDoc(n int) bson.D {
	d := make(bson.D, n)
	for i := range d {
		d[i] = bson.E{Key: string(rune('a' + i)), Value: i}
	}
	return d
}

func TestDerivedMissingAndSummaryValues(t *testing.T) {
	m := New()
	m.SetDocs([]bson.D{
		{{Key: "obj", Value: bson.D{{Key: "deep", Value: numberedDoc(2)}}}, {Key: "array", Value: bson.A{1, 2, 3}}},
		{{Key: "obj", Value: numberedDoc(7)}, {Key: "null", Value: nil}},
	})
	if m.cells[0][0].text != "{…2}" || m.cells[0][1].text != "[3]" {
		t.Fatalf("unexpected summary cells: %+v", m.cells[0])
	}
	if m.cells[0][2].text != "—" || !m.cells[0][2].missing {
		t.Fatalf("absent parent should be missing: %+v", m.cells[0][2])
	}
	if m.cells[1][0].text != "—" || m.cells[1][2].text != "{…7}" || m.cells[1][3].text != "null" {
		t.Fatalf("unexpected second row: %+v", m.cells[1])
	}
}

func TestRawDocumentFlattening(t *testing.T) {
	raw, err := bson.Marshal(bson.D{{Key: "browser", Value: "Chrome"}})
	if err != nil {
		t.Fatal(err)
	}
	p := derivePage([]bson.D{{{Key: "metadata", Value: bson.Raw(raw)}}})
	if !reflect.DeepEqual(p.columns, []string{"metadata.browser"}) {
		t.Fatalf("columns = %v", p.columns)
	}
}
