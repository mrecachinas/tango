package metrics

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/state"
)

func ss70() bson.M {
	return bson.M{"wiredTiger": bson.M{"concurrentTransactions": bson.M{
		"read":  bson.M{"out": int32(4), "available": int32(12), "totalTickets": int32(16), "queueLength": int64(2)},
		"write": bson.M{"out": int32(0), "available": int32(16), "totalTickets": int32(16), "queueLength": int64(1)},
	}}}
}

func ss82() bson.M {
	return bson.M{"queues": bson.M{"execution": bson.M{
		"read": bson.M{"out": int32(8), "available": int32(8), "totalTickets": int32(16),
			"normalPriority": bson.M{"queueLength": int64(3)}, "exempt": bson.M{"queueLength": int64(1)}},
		"write": bson.M{"out": int32(0), "available": int32(16), "totalTickets": int32(16),
			"normalPriority": bson.M{"queueLength": int64(0)}},
	}}, "globalLock": bson.M{"currentQueue": bson.M{"readers": int32(99), "writers": int32(99)}}}
}

func TestTicketsAndQueue(t *testing.T) {
	if got := extractTicketsPct(ss70()); got != 12.5 {
		t.Errorf("7.0 tickets = %v, want 12.5", got)
	}
	if got := queueDepth(ss70()); got != 3 {
		t.Errorf("7.0 queue = %v, want 3", got)
	}
	if got := extractTicketsPct(ss82()); got != 25 {
		t.Errorf("8.x tickets = %v, want 25", got)
	}
	if got := queueDepth(ss82()); got != 4 {
		t.Errorf("8.x queue = %v, want 4 (must not fall back to globalLock)", got)
	}
	old := bson.M{"globalLock": bson.M{"currentQueue": bson.M{"readers": int32(1), "writers": int32(2)}}}
	if got := queueDepth(old); got != 3 {
		t.Errorf("legacy queue = %v, want 3", got)
	}
}

func member(name string, st int32, health float64, t time.Time) bson.M {
	return bson.M{"name": name, "state": st, "health": health, "optimeDate": primitive.NewDateTimeFromTime(t)}
}

func TestSummarizeReplWorstLag(t *testing.T) {
	now := time.Now()
	info := summarizeRepl(bson.M{"members": bson.A{
		member("p:1", 1, 1, now),
		member("s:1", 2, 1, now.Add(-500*time.Millisecond)),
		member("s:2", 2, 1, now.Add(-30*time.Second)),
		member("a:1", 7, 0, time.Time{}),
	}})
	if info.worstMember != "s:2" || math.Abs(info.worstLagMs-30000) > 1 {
		t.Errorf("worst = %s %.0fms, want s:2 30000ms", info.worstMember, info.worstLagMs)
	}
	if info.members != 4 || info.healthy != 3 {
		t.Errorf("health = %d/%d", info.healthy, info.members)
	}
}

func TestApplyUsesElapsedTime(t *testing.T) {
	mm := newModel()
	t0 := time.Now()
	mk := func(at time.Time, ops, bytesIn int64) sample {
		return sample{at: at, ss: bson.M{
			"opcounters": bson.M{"query": ops},
			"network":    bson.M{"bytesIn": bytesIn},
		}}
	}
	mm.apply(mk(t0, 1000, 0))
	mm.apply(mk(t0.Add(5*time.Second), 1500, 10*1024*5))
	if got := mm.ops.Last(); got != 100 {
		t.Errorf("ops/s = %v, want 100 (500 ops over 5s)", got)
	}
	if got := mm.netIn.Last(); got != 10 {
		t.Errorf("net KB/s = %v, want 10", got)
	}
	// A failed poll must not produce a rate or reset the baseline.
	mm.apply(sample{at: t0.Add(6 * time.Second), err: errors.New("down")})
	mm.apply(mk(t0.Add(10*time.Second), 2000, 10*1024*5))
	if got := mm.ops.Last(); got != 100 {
		t.Errorf("ops/s after gap = %v, want 100", got)
	}
}

func TestRenderDisconnected(t *testing.T) {
	state.S = &state.AppState{HostLabel: "h:1"}
	mm := newModel()
	mm.apply(sample{at: time.Now(), err: errors.New("server selection timeout [x]")})
	out := mm.render()
	if !strings.Contains(out, "disconnected") || strings.Contains(out, "[x]") && !strings.Contains(out, "[x[]") {
		t.Errorf("render = %q", out)
	}
}
