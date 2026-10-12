package documents

import (
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

func TestStatsSectionsKeepTypedLayoutAndCounts(t *testing.T) {
	oldState, oldTop, oldAt, oldProfile := state.S, prevTop, prevTopAt, profileSection
	t.Cleanup(func() {
		state.S, prevTop, prevTopAt, profileSection = oldState, oldTop, oldAt, oldProfile
	})
	state.S = &state.AppState{RefreshInterval: 2 * time.Second}
	prevTop, prevTopAt = nil, time.Time{}
	long := strings.Repeat("command ", 30)
	renderStats(statsData{
		at: time.Date(2026, 10, 11, 12, 34, 56, 0, time.UTC),
		ops: bson.A{
			bson.M{"opid": 12, "op": "command", "secs_running": 2, "command": bson.D{{Key: "find", Value: long}}},
			bson.M{"opid": 34, "op": "query", "secs_running": 20},
			bson.M{"opid": 56, "op": "none"},
		},
		top: bson.M{
			"app.events":     bson.M{"total": bson.M{"count": 40, "time": 2000}},
			"local.internal": bson.M{"total": bson.M{"count": 1, "time": 10}},
		},
		profileFetched: true,
		profiles:       []bson.M{{"millis": 250, "op": "query", "ns": "app.events"}},
		profileDBs:     1,
	})
	s := state.S
	if len(s.StatsSections) != 3 || !strings.Contains(s.StatsUpdated, "Updated 12:34:56 · auto-refresh every 2s") {
		t.Fatalf("stats metadata: %q %+v", s.StatsUpdated, s.StatsSections)
	}
	ops, top, profiles := s.StatsSections[0], s.StatsSections[1], s.StatsSections[2]
	if ops.Note != "(2)" || ops.Rows[0][0] != "34" || ops.Rows[0][3] != "20" || !ops.Columns[3].Right {
		t.Fatalf("operations ordering or numeric column: %+v", ops)
	}
	if !strings.Contains(ops.Rows[1][5], long) {
		t.Fatal("command was truncated before viewport layout")
	}
	if len(top.Rows) != 1 || top.Rows[0][0] != "app.events" || top.Rows[0][3] != "40" {
		t.Fatalf("top rows: %+v", top)
	}
	if len(profiles.Rows) != 1 || profiles.Rows[0][0] != "250" || !profiles.Columns[0].Right {
		t.Fatalf("slow queries: %+v", profiles)
	}
	if !strings.Contains(s.StatsText, "Active Operations") {
		t.Fatal("plain stats summary lost its compatible section label")
	}
	renderStats(statsData{at: time.Now()})
	if len(s.StatsSections[2].Rows) != 1 {
		t.Fatal("automatic refresh discarded profiler results")
	}
}
