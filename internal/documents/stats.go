package documents

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tango/internal/mjson"
	"tango/internal/state"
)

type topSample struct {
	count  int64
	micros int64
}

// Stats view state; only touched on the UI goroutine.
var (
	prevTop        map[string]topSample
	prevTopAt      time.Time
	profileSection string
	statsFetching  bool
)

type statsData struct {
	at             time.Time
	ops            bson.A
	opsErr         error
	top            bson.M
	topErr         error
	profileFetched bool
	profiles       []bson.M
	profileDBs     int
}

func refreshInterval() time.Duration {
	if d := state.S.RefreshInterval; d > 0 {
		return d
	}
	return 2 * time.Second
}

// ShowStats opens the server stats view, which auto-refreshes while visible.
func ShowStats() {
	s := state.S
	s.StatsEpoch++
	epoch := s.StatsEpoch
	if s.StatsText == "" {
		s.StatsText = "loading…"
		s.StatsRevision++
	}
	s.View, s.Focus = "stats", "content"
	if s.StatsStopCh != nil {
		close(s.StatsStopCh)
	}
	stop := make(chan struct{})
	s.StatsStopCh = stop
	RefreshStats(true)

	interval := refreshInterval()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				state.Post(func() {
					if s.StatsEpoch == epoch && s.View == "stats" {
						RefreshStats(false)
					}
				})
			case <-stop:
				return
			}
		}
	}()
}

// RefreshStats re-fetches stats; withProfile also re-reads the profiler
// collections of every database (skipped on automatic refreshes).
func RefreshStats(withProfile bool) {
	s := state.S
	if statsFetching {
		return
	}
	statsFetching = true
	var dbs []string
	if withProfile {
		for _, e := range s.AllDBItems {
			dbs = append(dbs, e.Name)
		}
	}
	client := s.Client
	msg := ""
	if withProfile {
		msg = "Loading server stats"
	}
	state.Async(msg, func(ctx context.Context) func() {
		data := fetchStats(ctx, client, withProfile, dbs)
		return func() {
			statsFetching = false
			renderStats(data)
		}
	})
}

func fetchStats(ctx context.Context, client *mongo.Client, withProfile bool, dbs []string) statsData {
	d := statsData{at: time.Now()}
	admin := client.Database("admin")

	var cur bson.M
	d.opsErr = admin.RunCommand(ctx, bson.D{{Key: "currentOp", Value: 1}, {Key: "active", Value: true}}).Decode(&cur)
	d.ops, _ = cur["inprog"].(bson.A)

	var top bson.M
	if d.topErr = admin.RunCommand(ctx, bson.D{{Key: "top", Value: 1}}).Decode(&top); d.topErr == nil {
		d.top, _ = top["totals"].(bson.M)
	}

	if withProfile {
		d.profileFetched = true
		results := make([][]bson.M, len(dbs))
		forEachLimit(len(dbs), listConcurrency, func(i int) {
			c, cancel := context.WithTimeout(ctx, perItemTimeout)
			defer cancel()
			cursor, err := client.Database(dbs[i]).Collection("system.profile").Find(c, bson.D{},
				options.Find().SetSort(bson.D{{Key: "millis", Value: -1}}).SetLimit(10))
			if err != nil {
				return
			}
			_ = cursor.All(c, &results[i])
		})
		for _, r := range results {
			if len(r) > 0 {
				d.profileDBs++
			}
			d.profiles = append(d.profiles, r...)
		}
		sort.SliceStable(d.profiles, func(i, j int) bool {
			a, _ := state.ToFloat64(d.profiles[i]["millis"])
			b, _ := state.ToFloat64(d.profiles[j]["millis"])
			return a > b
		})
		if len(d.profiles) > 10 {
			d.profiles = d.profiles[:10]
		}
	}
	return d
}

func compactCmd(v interface{}, n int) string {
	if v == nil {
		return ""
	}
	return truncate(strings.ReplaceAll(mjson.Compact(v), "\n", " "), n)
}

func renderStats(d statsData) {
	s := state.S
	var buf strings.Builder
	fmt.Fprintf(&buf, "Updated %s · auto-refresh every %s · r refresh incl. profiler · :killop <opid> to kill\n\n",
		d.at.Format("15:04:05"), refreshInterval())

	// Active operations
	s.StatsOps = map[string]interface{}{}
	type opRow struct {
		opid, typ, ns, client, cmd string
		secs                       float64
	}
	var rows []opRow
	for _, raw := range d.ops {
		op, ok := raw.(bson.M)
		if !ok {
			continue
		}
		typ, _ := op["op"].(string)
		if typ == "none" {
			continue
		}
		opid := fmt.Sprintf("%v", op["opid"])
		s.StatsOps[opid] = op["opid"]
		ns, _ := op["ns"].(string)
		cl, _ := op["client"].(string)
		secs, _ := state.ToFloat64(op["secs_running"])
		rows = append(rows, opRow{opid: opid, typ: typ, ns: ns, client: cl, cmd: compactCmd(op["command"], 80), secs: secs})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].secs > rows[j].secs })
	fmt.Fprintf(&buf, "Active Operations (%d)\n\n", len(rows))
	switch {
	case d.opsErr != nil:
		fmt.Fprintf(&buf, "  Error: %s\n  (may require the inprog privilege)\n", d.opsErr)
	case len(rows) == 0:
		buf.WriteString("  (no active operations)\n")
	default:
		fmt.Fprintf(&buf, "  %-14s %-9s %-32s %8s  %-21s %s\n", "OPID", "TYPE", "NAMESPACE", "SECS", "CLIENT", "COMMAND")
		for i, r := range rows {
			if i == 50 {
				fmt.Fprintf(&buf, "  … %d more\n", len(rows)-50)
				break
			}
			fmt.Fprintf(&buf, "  %-14s %-9s %-32s %8.0f  %-21s %s\n",
				r.opid, r.typ, truncate(r.ns, 32), r.secs, r.client, r.cmd)
		}
	}

	// Top collections, as rates over the last refresh interval.
	buf.WriteString("\n")
	switch {
	case d.topErr != nil:
		buf.WriteString("Top Collections\n\n")
		fmt.Fprintf(&buf, "  Error: %s\n  (top is only available on mongod and needs the top privilege)\n", d.topErr)
	default:
		renderTop(&buf, d)
	}

	// Slow queries
	if d.profileFetched {
		var pb strings.Builder
		fmt.Fprintf(&pb, "Slow Queries (profiler, top 10 across %d database(s))\n\n", d.profileDBs)
		if len(d.profiles) == 0 {
			pb.WriteString("  No profiler data. Enable with db.setProfilingLevel(1) in the databases you care about.\n")
		} else {
			fmt.Fprintf(&pb, "  %-8s %-9s %-40s %-20s %s\n", "MILLIS", "TYPE", "NAMESPACE", "WHEN", "COMMAND")
			for _, p := range d.profiles {
				ms, _ := state.ToFloat64(p["millis"])
				typ, _ := p["op"].(string)
				ns, _ := p["ns"].(string)
				when := ""
				if ts, ok := p["ts"].(interface{ Time() time.Time }); ok {
					when = ts.Time().Local().Format("2006-01-02 15:04:05")
				}
				cmd := p["command"]
				if cmd == nil {
					cmd = p["query"]
				}
				fmt.Fprintf(&pb, "  %-8.0f %-9s %-40s %-20s %s\n",
					ms, typ, truncate(ns, 40), when, compactCmd(cmd, 80))
			}
		}
		profileSection = pb.String()
	}
	buf.WriteString("\n")
	buf.WriteString(profileSection)

	s.StatsText = buf.String()
	s.StatsRevision++
}

func renderTop(buf *strings.Builder, d statsData) {
	cur := map[string]topSample{}
	for ns, val := range d.top {
		if strings.HasPrefix(ns, "local.") || strings.Contains(ns, ".system.") || ns == "note" {
			continue
		}
		m, ok := val.(bson.M)
		if !ok {
			continue
		}
		total, ok := m["total"].(bson.M)
		if !ok {
			continue
		}
		c, _ := state.ToFloat64(total["count"])
		t, _ := state.ToFloat64(total["time"])
		cur[ns] = topSample{count: int64(c), micros: int64(t)}
	}

	type row struct {
		ns          string
		rate, avgMs float64
		total       int64
	}
	var rows []row
	elapsed := d.at.Sub(prevTopAt).Seconds()
	haveRates := prevTop != nil && elapsed > 0
	for ns, c := range cur {
		r := row{ns: ns, total: c.count}
		if haveRates {
			p := prevTop[ns]
			dc, dt := c.count-p.count, c.micros-p.micros
			if dc > 0 {
				r.rate = float64(dc) / elapsed
				r.avgMs = float64(dt) / float64(dc) / 1000
			}
		} else if c.count > 0 {
			r.avgMs = float64(c.micros) / float64(c.count) / 1000
		}
		rows = append(rows, r)
	}
	prevTop, prevTopAt = cur, d.at

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].rate != rows[j].rate {
			return rows[i].rate > rows[j].rate
		}
		return rows[i].total > rows[j].total
	})
	if len(rows) > 10 {
		rows = rows[:10]
	}
	if haveRates {
		fmt.Fprintf(buf, "Top Collections (last %.0fs)\n\n", elapsed)
	} else {
		buf.WriteString("Top Collections (cumulative since startup; rates from next refresh)\n\n")
	}
	if len(rows) == 0 {
		buf.WriteString("  (no collection activity)\n")
		return
	}
	fmt.Fprintf(buf, "  %-44s %10s %10s %14s\n", "NAMESPACE", "OPS/S", "AVG MS", "TOTAL OPS")
	for _, r := range rows {
		fmt.Fprintf(buf, "  %-44s %10.1f %10.2f %14d\n", truncate(r.ns, 44), r.rate, r.avgMs, r.total)
	}
}

// KillOpConfirm asks to kill the operation with the given opid.
func KillOpConfirm(arg string) {
	s := state.S
	raw, ok := s.StatsOps[arg]
	if !ok {
		n, err := strconv.ParseInt(arg, 10, 64)
		switch {
		case err == nil && n >= -1<<31 && n < 1<<31:
			raw = int32(n)
		case err == nil:
			raw = n
		case strings.Contains(arg, ":"):
			raw = arg // mongos: "shard:opid"
		default:
			state.ShowError(fmt.Sprintf("Unknown opid %q", arg))
			return
		}
	}
	if s.ReadOnly {
		state.ShowError("Read-only mode: killOp is disabled")
		return
	}
	confirm("killop", fmt.Sprintf("Kill operation %s?", arg), "", func() {
		admin := s.Client.Database("admin")
		state.Async("Killing operation", func(ctx context.Context) func() {
			err := admin.RunCommand(ctx, bson.D{{Key: "killOp", Value: 1}, {Key: "op", Value: raw}}).Err()
			return func() {
				if err != nil {
					state.FlashError("killOp failed: " + err.Error())
					return
				}
				state.FlashStatus("Kill requested for op " + arg)
				if s.View == "stats" {
					RefreshStats(false)
				}
			}
		})
	})
}
