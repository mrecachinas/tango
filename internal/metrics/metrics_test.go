package metrics

import (
	"errors"
	"math"
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

func TestSnapshotDisconnected(t *testing.T) {
	old := state.S
	t.Cleanup(func() { state.S = old })
	state.S = &state.AppState{HostLabel: "h:1"}
	mm := newModel()
	err := errors.New("server selection timeout [x]")
	mm.apply(sample{at: time.Now(), err: err})
	out := mm.snapshot()
	if out.HostLabel != "h:1" || out.Connected || out.Err != err {
		t.Errorf("snapshot = %+v", out)
	}
}

func TestSnapshotDataAndOwnership(t *testing.T) {
	old := state.S
	t.Cleanup(func() { state.S = old })
	state.S = &state.AppState{HostLabel: "mongo:27017", ReadOnly: true, CachedDBCount: 2, CachedCollCount: 5}
	mm := newModel()
	t0 := time.Now()
	status := func(ops, in, out int64) bson.M {
		ss := ss82()
		ss["uptime"] = float64(86340)
		ss["opcounters"] = bson.M{"query": ops}
		ss["network"] = bson.M{"bytesIn": in, "bytesOut": out}
		ss["mem"] = bson.M{"resident": int32(200)}
		ss["connections"] = bson.M{"current": int32(3), "available": int32(97)}
		ss["wiredTiger"] = bson.M{"cache": bson.M{
			"bytes currently in the cache": int64(80), "maximum bytes configured": int64(100),
		}}
		ss["metrics"] = bson.M{"cursor": bson.M{"open": bson.M{"total": int64(6)}}}
		return ss
	}
	rs := &replInfo{name: "rs0", members: 3, healthy: 2, worstMember: "secondary", worstLagMs: 1200}
	mm.apply(sample{at: t0, version: "7.0.31", ss: status(100, 1024, 2048), pingMs: .4, repl: rs})
	mm.apply(sample{at: t0.Add(2 * time.Second), version: "7.0.31", ss: status(104, 5120, 10240), pingMs: .6, repl: rs})
	out := mm.snapshot()
	if out.HostLabel != "mongo:27017" || !out.ReadOnly || !out.Connected || out.Err != nil ||
		out.Version != "7.0.31" || out.DBCount != 2 || out.CollectionCount != 5 || out.Uptime != 86340*time.Second {
		t.Errorf("metadata = %+v", out)
	}
	if out.OpsPerSec != 2 || out.NetInKBPerSec != 2 || out.NetOutKBPerSec != 4 ||
		out.PingMS != .6 || out.MemoryMB != 200 || out.ConnectionsCurrent != 3 || out.ConnectionsAvailable != 97 ||
		out.Queue != 4 || out.Cursors != 6 || out.CachePercent != 80 || out.TicketsPercent != 25 {
		t.Errorf("values = %+v", out)
	}
	if out.ReplicaSet.Name != "rs0" || out.ReplicaSet.Healthy != 2 || out.ReplicaSet.WorstLagMS != 1200 {
		t.Errorf("replica = %+v", out.ReplicaSet)
	}
	histories := [][]float64{
		out.History.Ops, out.History.Connections, out.History.Ping, out.History.Memory,
		out.History.NetIn, out.History.NetOut, out.History.Queue, out.History.Cursors,
		out.History.Cache, out.History.Tickets,
	}
	for i, data := range histories {
		if len(data) == 0 {
			t.Fatalf("history %d is empty", i)
		}
		data[0] = -12345
	}
	out.ReplicaSet.Name = "changed"
	next := mm.snapshot()
	if next.History.Ops[0] != 2 || next.History.Connections[0] != 3 || next.History.Ping[0] != .4 ||
		next.History.Memory[0] != 200 || next.History.NetIn[0] != 2 || next.History.NetOut[0] != 4 ||
		next.History.Queue[0] != 4 || next.History.Cursors[0] != 6 ||
		next.History.Cache[0] != 80 || next.History.Tickets[0] != 25 || next.ReplicaSet.Name != "rs0" {
		t.Error("snapshot aliases mutable history or replica metadata")
	}
	mm.apply(sample{at: t0.Add(3 * time.Second), err: errors.New("down")})
	failed := mm.snapshot()
	if failed.Connected || failed.Err == nil || failed.MemoryMB != 200 || failed.Version != "7.0.31" ||
		failed.CachePercent != 80 || failed.ConnectionsAvailable != 97 {
		t.Errorf("failed poll lost last good sample = %+v", failed)
	}
}

func TestHistoryBoundsAndCounterReset(t *testing.T) {
	mm := newModel()
	t0 := time.Now()
	for i := 0; i < SparkWidth+10; i++ {
		mm.apply(sample{at: t0.Add(time.Duration(i) * time.Second), ss: bson.M{
			"opcounters": bson.M{"query": int64(i * 10)},
			"network":    bson.M{"bytesIn": int64(i * 1024), "bytesOut": int64(i * 2048)},
		}})
	}
	for _, data := range [][]float64{mm.ops.Data, mm.conn.Data, mm.netIn.Data, mm.netOut.Data, mm.ping.Data} {
		if len(data) != SparkWidth {
			t.Errorf("history length = %d, want %d", len(data), SparkWidth)
		}
	}
	mm.apply(sample{at: t0.Add(31 * time.Second), ss: bson.M{
		"opcounters": bson.M{"query": int64(0)},
		"network":    bson.M{"bytesIn": int64(0), "bytesOut": int64(0)},
	}})
	if mm.ops.Last() != 0 || mm.netIn.Last() != 0 || mm.netOut.Last() != 0 {
		t.Error("counter reset produced negative rates")
	}
}

func TestMissingCountersDoNotCreateSpikes(t *testing.T) {
	mm := newModel()
	t0 := time.Now()
	mm.apply(sample{at: t0, ss: bson.M{}})
	mm.apply(sample{at: t0.Add(time.Second), ss: bson.M{
		"opcounters": bson.M{"query": int64(9000)},
		"network":    bson.M{"bytesIn": int64(90000), "bytesOut": int64(90000)},
	}})
	if len(mm.ops.Data) != 0 || len(mm.netIn.Data) != 0 || len(mm.netOut.Data) != 0 {
		t.Error("missing baselines created rates")
	}
	mm.apply(sample{at: t0.Add(2 * time.Second), ss: bson.M{
		"opcounters": bson.M{"query": int64(9005)},
		"network":    bson.M{"bytesIn": int64(91024), "bytesOut": int64(92048)},
	}})
	if mm.ops.Last() != 5 || mm.netIn.Last() != 1 || mm.netOut.Last() != 2 {
		t.Error("subsequent valid baselines did not produce rates")
	}
}

func TestDeliverOnlyAppliesOnPostedClosure(t *testing.T) {
	old := state.S
	t.Cleanup(func() { state.S = old })
	state.S = &state.AppState{HostLabel: "before"}
	mm := newModel()
	posted := make(chan func(), 1)
	done := make(chan struct{})
	var received Snapshot
	called := false
	go func() {
		mm.deliver(sample{at: time.Now(), ss: bson.M{"uptime": int64(5)}},
			func(f func()) { posted <- f },
			func(s Snapshot) { received = s; called = true })
		close(done)
	}()
	closure := <-posted
	<-done
	if called || mm.last.ss != nil {
		t.Fatal("sample or sink ran on poller goroutine")
	}
	state.S.HostLabel = "on-ui"
	closure()
	if !called || received.HostLabel != "on-ui" || received.Uptime != 5*time.Second || !received.Connected {
		t.Errorf("UI handoff = %+v, called = %v", received, called)
	}
}

func TestFetchWithoutClient(t *testing.T) {
	p := &poller{timeout: time.Second, version: "7.0.31"}
	smp := p.fetch()
	if smp.err == nil || smp.version != "7.0.31" || smp.at.IsZero() {
		t.Errorf("nil client sample = %+v", smp)
	}
}
