package documents

import (
	"strconv"
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

func pageFindJSON(t *testing.T, st *state.AppState) string {
	t.Helper()
	old := state.S
	t.Cleanup(func() { state.S = old })
	state.S = st
	spec, ok := CurrentPageFind()
	if !ok {
		t.Fatal("no find for the current page")
	}
	got, err := bson.MarshalExtJSON(spec.Command("c"), false, false)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestCurrentPageFind_Keyset(t *testing.T) {
	got := pageFindJSON(t, &state.AppState{
		CurrentDB: "d", CurrentColl: "c", CurrentPage: 2, KeysetMode: true,
		PageStarts: []state.PageStart{{}, {ID: int32(50), Set: true}, {ID: int32(100), Set: true}},
	})
	want := `{"find":"c","filter":{"_id":{"$ne":100}},"hint":{"_id":1},"min":{"_id":100},"limit":` + strconv.Itoa(state.PageSize) + `}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestCurrentPageFind_SnapshotSkip(t *testing.T) {
	got := pageFindJSON(t, &state.AppState{
		CurrentDB: "d", CurrentColl: "c", CurrentPage: 1, SnapshotID: int32(9),
		CurrentFilter: bson.D{{Key: "i", Value: int32(7)}}, CurrentSort: bson.D{{Key: "i", Value: int32(1)}},
	})
	want := `{"find":"c","filter":{"$and":[{"i":7},{"_id":{"$not":{"$gt":9}}}]},"sort":{"i":1},"skip":` +
		strconv.Itoa(state.PageSize) + `,"limit":` + strconv.Itoa(state.PageSize) + `}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestCurrentPageFind_LiveSort(t *testing.T) {
	got := pageFindJSON(t, &state.AppState{
		CurrentDB: "d", CurrentColl: "c", LiveMode: true, SnapshotID: int32(9),
	})
	want := `{"find":"c","filter":{},"sort":{"_id":-1},"limit":` + strconv.Itoa(state.PageSize) + `}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
