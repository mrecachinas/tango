package doctable

import (
	"reflect"
	"sort"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const maxFlattenKeys = 6

type page struct {
	columns []string
	rows    []map[string]any
}

// derivePage expands only small top-level documents. Shape changes across rows
// can produce both a parent column and dotted columns; neither loses its value.
func derivePage(docs []bson.D) page {
	p := page{rows: make([]map[string]any, len(docs))}
	seen := make(map[string]bool)
	for i, doc := range docs {
		row := make(map[string]any, len(doc))
		add := func(key string, value any) {
			if _, exists := row[key]; !exists {
				row[key] = value
			}
			if !seen[key] {
				seen[key] = true
				p.columns = append(p.columns, key)
			}
		}
		for _, e := range doc {
			sub, ok := asDocument(e.Value)
			if e.Key != "_id" && ok && len(sub) > 0 && len(sub) <= maxFlattenKeys {
				for _, child := range sub {
					add(e.Key+"."+child.Key, child.Value)
				}
			} else {
				add(e.Key, e.Value)
			}
		}
		p.rows[i] = row
	}
	for i, name := range p.columns {
		if name == "_id" {
			copy(p.columns[1:i+1], p.columns[:i])
			p.columns[0] = "_id"
			break
		}
	}
	return p
}

func asDocument(value any) (bson.D, bool) {
	switch v := value.(type) {
	case bson.D:
		return v, true
	case bson.M:
		return mapDocument(v), true
	case map[string]any:
		return mapDocument(v), true
	case bson.Raw:
		var d bson.D
		if bson.Unmarshal(v, &d) == nil {
			return d, true
		}
	}
	return nil, false
}

func mapDocument(m map[string]any) bson.D {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	d := make(bson.D, 0, len(keys))
	for _, key := range keys {
		d = append(d, bson.E{Key: key, Value: m[key]})
	}
	return d
}

func arrayLength(value any) (int, bool) {
	// Binary is a scalar, even when supplied as an ordinary byte slice.
	switch value.(type) {
	case []byte, primitive.ObjectID, bson.Raw:
		return 0, false
	}
	v := reflect.ValueOf(value)
	if v.IsValid() && (v.Kind() == reflect.Array || v.Kind() == reflect.Slice) {
		return v.Len(), true
	}
	return 0, false
}
