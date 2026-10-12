package metrics

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"tango/internal/state"
)

const (
	SparkWidth      = 20
	DefaultInterval = 2 * time.Second
	replRecheck     = 30 * time.Second
)

// Snapshot is a plain, immutable copy of the latest sample and rolling history.
// The sink receives it on the UI goroutine; it contains no terminal markup.
type Snapshot struct {
	HostLabel, Version                       string
	Uptime                                   time.Duration
	Connected, ReadOnly                      bool
	Err                                      error
	DBCount, CollectionCount                 int
	OpsPerSec                                float64
	ConnectionsCurrent, ConnectionsAvailable int
	PingMS, MemoryMB                         float64
	NetInKBPerSec, NetOutKBPerSec            float64
	Queue, Cursors                           float64
	CachePercent, TicketsPercent             float64
	ReplicaSet                               *ReplicaSetInfo
	History                                  Histories
}

type ReplicaSetInfo struct {
	Name             string
	Members, Healthy int
	WorstLagMS       float64
	WorstMember      string
}

// Histories are ordered oldest to newest, with at most SparkWidth samples.
type Histories struct {
	Ops, Connections, Ping, Memory []float64
	NetIn, NetOut, Queue, Cursors  []float64
	Cache, Tickets                 []float64
}

// serverStatus sections tango doesn't use; excluding them keeps each poll small.
var excludedSections = []string{
	"asserts", "batchedDeletes", "catalogStats", "changeStreamPreImages", "collectionCatalog",
	"defaultRWConcern", "electionMetrics", "extra_info", "featureCompatibilityVersion", "fle",
	"flowControl", "ftdcCollectionMetrics", "health", "incrementalRollout", "indexBuilds",
	"indexBulkBuilder", "indexStats", "intentRegistry", "internalTransactions", "locks",
	"logicalSessionRecordCache", "mirroredReads", "opLatencies", "opWorkingTime", "opcountersRepl",
	"oplogTruncation", "profiler", "queryAnalyzers", "querySettings", "readConcernCounters",
	"readPreferenceCounters", "recoveryOplogApplier", "repl", "scramCache", "security",
	"shardedIndexConsistency", "shardingStatistics", "spillWiredTiger", "storageEngine",
	"tcmalloc", "trafficRecording", "transactions", "twoPhaseCommitCoordinator",
}

// replInfo summarises replSetGetStatus.
type replInfo struct {
	name        string
	worstLagMs  float64
	worstMember string
	members     int
	healthy     int
}

// sample is one poll's raw data; produced off the UI goroutine.
type sample struct {
	at      time.Time
	err     error
	version string
	ss      bson.M
	pingMs  float64
	repl    *replInfo
}

// model holds metric history; only touched on the UI goroutine.
type model struct {
	ops, conn, mem, ping, queue, netIn, netOut, cursors, cache, tickets *state.RollingBuffer
	prevOps, prevNetIn, prevNetOut                                      float64
	prevAt                                                              time.Time
	havePrev, haveOps, haveNetIn, haveNetOut                            bool
	last                                                                sample
}

func newModel() *model {
	nb := func() *state.RollingBuffer { return state.NewRollingBuffer(SparkWidth) }
	return &model{
		ops: nb(), conn: nb(), mem: nb(), ping: nb(), queue: nb(),
		netIn: nb(), netOut: nb(), cursors: nb(), cache: nb(), tickets: nb(),
	}
}

type poller struct {
	client   *mongo.Client
	timeout  time.Duration
	version  string
	replNext time.Time
}

func (p *poller) fetch() sample {
	smp := sample{at: time.Now(), version: p.version}
	if p.client == nil {
		smp.err = errors.New("no MongoDB client")
		return smp
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	admin := p.client.Database("admin")

	pingStart := time.Now()
	if err := admin.RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Err(); err != nil {
		smp.err = err
		return smp
	}
	smp.pingMs = float64(time.Since(pingStart).Microseconds()) / 1000.0

	if p.version == "" {
		var bi bson.M
		if err := admin.RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&bi); err == nil {
			p.version, _ = bi["version"].(string)
		}
	}
	smp.version = p.version

	cmd := bson.D{{Key: "serverStatus", Value: 1}}
	for _, sec := range excludedSections {
		cmd = append(cmd, bson.E{Key: sec, Value: 0})
	}
	if err := admin.RunCommand(ctx, cmd).Decode(&smp.ss); err != nil {
		smp.err = err
		return smp
	}

	if !smp.at.Before(p.replNext) {
		var rs bson.M
		if err := admin.RunCommand(ctx, bson.D{{Key: "replSetGetStatus", Value: 1}}).Decode(&rs); err == nil {
			smp.repl = summarizeRepl(rs)
		} else {
			// Standalone, mongos or not permitted: don't ask every tick.
			p.replNext = smp.at.Add(replRecheck)
		}
	}
	return smp
}

// summarizeRepl finds the most-lagged secondary relative to the primary.
func summarizeRepl(rs bson.M) *replInfo {
	members, _ := rs["members"].(bson.A)
	info := &replInfo{members: len(members)}
	info.name, _ = rs["set"].(string)
	var primary time.Time
	type sec struct {
		name string
		t    time.Time
	}
	var secs []sec
	for _, raw := range members {
		mem, ok := raw.(bson.M)
		if !ok {
			continue
		}
		if h, _ := state.ToFloat64(mem["health"]); h == 1 {
			info.healthy++
		}
		st, _ := state.ToFloat64(mem["state"])
		name, _ := mem["name"].(string)
		opt, ok := mem["optimeDate"].(primitive.DateTime)
		if !ok {
			continue
		}
		switch int(st) {
		case 1:
			primary = opt.Time()
		case 2:
			secs = append(secs, sec{name, opt.Time()})
		}
	}
	if primary.IsZero() {
		return info
	}
	for _, s := range secs {
		lag := float64(primary.Sub(s.t).Milliseconds())
		if lag < 0 {
			lag = 0
		}
		if info.worstMember == "" || lag > info.worstLagMs {
			info.worstLagMs, info.worstMember = lag, s.name
		}
	}
	return info
}

// apply folds a sample into the metric history using the real elapsed time.
func (m *model) apply(smp sample) {
	if smp.err != nil {
		// Preserve the last good values while reporting the connection error.
		smp.ss, smp.repl = m.last.ss, m.last.repl
		if smp.version == "" {
			smp.version = m.last.version
		}
		m.last = smp
		return
	}
	m.last = smp
	ss := smp.ss
	elapsed := smp.at.Sub(m.prevAt).Seconds()
	rate := func(cur, prev float64) float64 {
		r := (cur - prev) / elapsed
		if r < 0 {
			return 0
		}
		return r
	}

	opsTotal, haveOps := sumOpcounters(ss)
	netIn, haveNetIn := networkBytesIn(ss)
	netOut, haveNetOut := networkBytesOut(ss)
	if m.havePrev && elapsed > 0 {
		if haveOps && m.haveOps {
			m.ops.Push(rate(opsTotal, m.prevOps))
		}
		if haveNetIn && m.haveNetIn {
			m.netIn.Push(rate(netIn, m.prevNetIn) / 1024)
		}
		if haveNetOut && m.haveNetOut {
			m.netOut.Push(rate(netOut, m.prevNetOut) / 1024)
		}
	}
	m.prevOps, m.prevNetIn, m.prevNetOut, m.prevAt = opsTotal, netIn, netOut, smp.at
	m.havePrev, m.haveOps, m.haveNetIn, m.haveNetOut = true, haveOps, haveNetIn, haveNetOut

	m.ping.Push(smp.pingMs)
	cur, _ := extractConnections(ss)
	m.conn.Push(float64(cur))
	if mem, ok := ss["mem"].(bson.M); ok {
		if res, ok := state.ToFloat64(mem["resident"]); ok {
			m.mem.Push(res)
		}
	}
	m.queue.Push(queueDepth(ss))
	m.cache.Push(extractCachePercent(ss))
	m.tickets.Push(extractTicketsPct(ss))
	if mt, ok := ss["metrics"].(bson.M); ok {
		if c, ok := mt["cursor"].(bson.M); ok {
			if open, ok := c["open"].(bson.M); ok {
				if total, ok := state.ToFloat64(open["total"]); ok {
					m.cursors.Push(total)
				}
			}
		}
	}
}

func (m *model) snapshot() Snapshot {
	s := state.S
	smp := m.last
	cur, avail := extractConnections(smp.ss)
	uptime, _ := state.ToFloat64(smp.ss["uptime"])
	out := Snapshot{
		HostLabel: s.HostLabel, Version: smp.version, Uptime: time.Duration(uptime * float64(time.Second)),
		Connected: smp.err == nil && smp.ss != nil, Err: smp.err, ReadOnly: s.ReadOnly,
		DBCount: s.CachedDBCount, CollectionCount: s.CachedCollCount,
		OpsPerSec: m.ops.Last(), ConnectionsCurrent: cur, ConnectionsAvailable: avail,
		PingMS: m.ping.Last(), MemoryMB: m.mem.Last(),
		NetInKBPerSec: m.netIn.Last(), NetOutKBPerSec: m.netOut.Last(),
		Queue: m.queue.Last(), Cursors: m.cursors.Last(),
		CachePercent: m.cache.Last(), TicketsPercent: m.tickets.Last(),
	}
	if r := smp.repl; r != nil {
		out.ReplicaSet = &ReplicaSetInfo{
			Name: r.name, Members: r.members, Healthy: r.healthy,
			WorstLagMS: r.worstLagMs, WorstMember: r.worstMember,
		}
	}
	copyData := func(b *state.RollingBuffer) []float64 { return append([]float64(nil), b.Data...) }
	out.History = Histories{
		Ops: copyData(m.ops), Connections: copyData(m.conn), Ping: copyData(m.ping), Memory: copyData(m.mem),
		NetIn: copyData(m.netIn), NetOut: copyData(m.netOut), Queue: copyData(m.queue), Cursors: copyData(m.cursors),
		Cache: copyData(m.cache), Tickets: copyData(m.tickets),
	}
	return out
}

func (m *model) deliver(smp sample, post func(func()), sink func(Snapshot)) {
	post(func() {
		m.apply(smp)
		sink(m.snapshot())
	})
}

// Start polls immediately and then every interval, returning a stop function.
// post must enqueue its
// closure on the UI goroutine; that closure applies history and calls sink.
// Each Start owns independent history. Client and timeout are captured here,
// while host label, read-only state and counts are read inside the posted closure.
func Start(interval time.Duration, post func(func()), sink func(Snapshot)) func() {
	s := state.S
	if interval <= 0 {
		interval = DefaultInterval
	}
	p := &poller{client: s.Client, timeout: s.QueryTimeout}
	if p.timeout <= 0 || p.timeout > interval*5 {
		p.timeout = interval * 5
	}
	m := newModel()
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			smp := p.fetch()
			m.deliver(smp, func(fn func()) {
				post(func() {
					if state.S != s {
						return
					}
					select {
					case <-stop:
						return
					default:
						fn()
					}
				})
			}, sink)
			select {
			case <-ticker.C:
			case <-stop:
				return
			}
		}
	}()
	return func() { once.Do(func() { close(stop) }) }
}
