package documents

import (
	"fmt"
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestCapCollectionsPrefersProfiled(t *testing.T) {
	var names []string
	for i := 0; i < 5; i++ {
		names = append(names, fmt.Sprintf("c%d", i))
	}
	profile := []bson.D{{{Key: "ns", Value: "d.c4"}}, {{Key: "ns", Value: "other.c3"}}}
	if got := capCollections(append([]string(nil), names...), "d", profile, 3); !reflect.DeepEqual(got, []string{"c0", "c1", "c4"}) {
		t.Errorf("capped = %v", got)
	}
	if got := capCollections([]string{"b", "a"}, "d", profile, 3); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("uncapped = %v", got)
	}
}

func TestSumIndexSizesAcrossShards(t *testing.T) {
	doc := func(sizes bson.D) bson.D {
		return bson.D{{Key: "shard", Value: "s"}, {Key: "storageStats", Value: bson.D{{Key: "indexSizes", Value: sizes}}}}
	}
	got := sumIndexSizes([]bson.D{
		doc(bson.D{{Key: "_id_", Value: int32(100)}, {Key: "a_1", Value: int64(50)}}),
		doc(bson.D{{Key: "a_1", Value: 25.0}, {Key: "_id_", Value: int32(10)}}),
	})
	want := bson.D{{Key: "_id_", Value: 110.0}, {Key: "a_1", Value: 75.0}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sizes = %v, want %v", got, want)
	}
	if got := sumIndexSizes(nil); got != nil {
		t.Errorf("empty = %v", got)
	}
}
