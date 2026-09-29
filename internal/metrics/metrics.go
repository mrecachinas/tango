package metrics

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"tango/internal/state"
)

const (
	SparkWidth      = 20
	gaugeWidth      = 15
	DefaultInterval = 2 * time.Second
	replRecheck     = 30 * time.Second
)

var sparkChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

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
	ops, conn, mem, ping, queue, netIn, cursors *state.RollingBuffer
	prevOps, prevNetIn                          float64
	prevAt                                      time.Time
	havePrev                                    bool
	last                                        sample
}

func newModel() *model {
	nb := func() *state.RollingBuffer { return state.NewRollingBuffer(SparkWidth) }
	return &model{ops: nb(), conn: nb(), mem: nb(), ping: nb(), queue: nb(), netIn: nb(), cursors: nb()}
}

var m = newModel()

type poller struct {
	client   *mongo.Client
	timeout  time.Duration
	version  string
	replNext time.Time
}

func (p *poller) fetch() sample {
	smp := sample{at: time.Now()}
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
	m.last = smp
	if smp.err != nil {
		return
	}
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
	netIn, haveNet := networkBytesIn(ss)
	if m.havePrev && elapsed > 0 {
		if haveOps {
			m.ops.Push(rate(opsTotal, m.prevOps))
		}
		if haveNet {
			m.netIn.Push(rate(netIn, m.prevNetIn) / 1024)
		}
	}
	m.prevOps, m.prevNetIn, m.prevAt, m.havePrev = opsTotal, netIn, smp.at, true

	m.ping.Push(smp.pingMs)
	cur, _ := extractConnections(ss)
	m.conn.Push(float64(cur))
	if mem, ok := ss["mem"].(bson.M); ok {
		if res, ok := state.ToFloat64(mem["resident"]); ok {
			m.mem.Push(res)
		}
	}
	m.queue.Push(queueDepth(ss))
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

func (m *model) render() string {
	s := state.S
	smp := m.last
	var indicator string
	switch {
	case smp.err != nil:
		indicator = "[red]● disconnected[white] [gray](" + escape(truncate(smp.err.Error(), 80)) + ")[white]"
	case s.ReadOnly:
		indicator = "[yellow]◐[white] [gray](read-only)[white]"
	default:
		indicator = "[green]●[white]"
	}
	version := smp.version
	if version == "" {
		version = "?"
	}
	uptime := "?"
	if smp.ss != nil {
		uptime = extractUptime(smp.ss)
	}
	line1 := fmt.Sprintf(" %s %s   [gray]MongoDB[white] v%s   [gray]uptime[white] %s   [aqua]%d[white] dbs · [aqua]%d[white] collections",
		indicator, escape(s.HostLabel), escape(version), uptime, s.CachedDBCount, s.CachedCollCount)

	connCur, connAvail := extractConnections(smp.ss)
	cachePct := extractCachePercent(smp.ss)
	ticketsPct := extractTicketsPct(smp.ss)

	const cellWidth = 36
	sep := "  "
	line2 := " " +
		metricCell("ops/s", renderSparkline(m.ops.Data), fmt.Sprintf("%-8.0f", m.ops.Last()), cellWidth) + sep +
		metricCell("conns", renderSparkline(m.conn.Data), fmt.Sprintf("%-8s", fmt.Sprintf("%d/%d", connCur, connCur+connAvail)), cellWidth) + sep +
		metricCell("ping ", renderSparkline(m.ping.Data), fmt.Sprintf("%-8s", fmt.Sprintf("%.1fms", m.ping.Last())), cellWidth)
	line3 := " " +
		metricCell("mem  ", renderSparkline(m.mem.Data), fmt.Sprintf("%-8s", fmt.Sprintf("%.0fMB", m.mem.Last())), cellWidth) + sep +
		metricGaugeCell("cache", renderGauge(cachePct), fmt.Sprintf("%-8s", fmt.Sprintf("%.0f%%", cachePct)), cellWidth) + sep +
		metricCell("net  ", renderSparkline(m.netIn.Data), fmt.Sprintf("%-8s", fmt.Sprintf("%.0fKB/s", m.netIn.Last())), cellWidth)
	line4 := " " +
		metricCell("queue", renderSparkline(m.queue.Data), fmt.Sprintf("%-8.0f", m.queue.Last()), cellWidth) + sep +
		metricCell("curs ", renderSparkline(m.cursors.Data), fmt.Sprintf("%-8.0f", m.cursors.Last()), cellWidth) + sep +
		metricGaugeCell("tix  ", renderGauge(ticketsPct), fmt.Sprintf("%-8s", fmt.Sprintf("%.0f%%", ticketsPct)), cellWidth)

	if r := smp.repl; r != nil {
		lagColor := "[green]"
		if r.worstLagMs > 1000 {
			lagColor = "[yellow]"
		}
		if r.worstLagMs > 10000 {
			lagColor = "[red]"
		}
		healthColor := "[green]"
		if r.healthy < r.members {
			healthColor = "[red]"
		}
		line4 += fmt.Sprintf("   [gray]rs %s%d/%d[gray] healthy", healthColor, r.healthy, r.members)
		if r.worstMember != "" {
			line4 += fmt.Sprintf(" · max lag %s%.0fms[gray] (%s)[white]", lagColor, r.worstLagMs, escape(r.worstMember))
		}
	}
	return line1 + "\n" + line2 + "\n" + line3 + "\n" + line4
}

// Start polls the server every interval (immediately on start) and updates
// the metrics bar. Network I/O happens on the poller goroutine; history is
// updated and rendered on the UI goroutine.
func Start(interval time.Duration) {
	s := state.S
	if interval <= 0 {
		interval = DefaultInterval
	}
	p := &poller{client: s.Client, timeout: s.QueryTimeout}
	if p.timeout <= 0 || p.timeout > interval*5 {
		p.timeout = interval * 5
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			smp := p.fetch()
			s.App.QueueUpdateDraw(func() {
				m.apply(smp)
				s.MetricsBar.SetText(m.render())
			})
			<-ticker.C
		}
	}()
}
