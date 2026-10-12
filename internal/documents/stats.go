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
	profileSection state.StatsSection
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
	s.StatsUpdated = fmt.Sprintf("Updated %s · auto-refresh every %s · r refresh incl. profiler · :killop <opid> to kill",
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
		rows = append(rows, opRow{opid: opid, typ: typ, ns: ns, client: cl, cmd: compactCmd(op["command"], 2000), secs: secs})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].secs > rows[j].secs })
	operations := state.StatsSection{
		Title: "Active Operations", Note: fmt.Sprintf("(%d)", len(rows)),
		Columns: []state.StatsColumn{
			{Name: "OPID", Width: 8, Right: true}, {Name: "TYPE", Width: 9},
			{Name: "NAMESPACE", Width: 24}, {Name: "SECS", Width: 6, Right: true},
			{Name: "CLIENT", Width: 21}, {Name: "COMMAND"},
		},
	}
	switch {
	case d.opsErr != nil:
		operations.Empty = fmt.Sprintf("Error: %s (may require the inprog privilege)", d.opsErr)
	case len(rows) == 0:
		operations.Empty = "No active operations"
	default:
		for i, r := range rows {
			if i == 50 {
				operations.Note += fmt.Sprintf(" · showing first 50 of %d", len(rows))
				break
			}
			operations.Rows = append(operations.Rows, []string{r.opid, r.typ, r.ns, fmt.Sprintf("%.0f", r.secs), r.client, r.cmd})
		}
	}

	// Top collections, as rates over the last refresh interval.
	var collections state.StatsSection
	switch {
	case d.topErr != nil:
		collections = state.StatsSection{
			Title: "Top Collections",
			Empty: fmt.Sprintf("Error: %s (top is only available on mongod and needs the top privilege)", d.topErr),
		}
	default:
		collections = renderTop(d)
	}

	// Slow queries
	if d.profileFetched {
		profileSection = state.StatsSection{
			Title: "Slow Queries", Note: fmt.Sprintf("(%d) · profiler across %d databases", len(d.profiles), d.profileDBs),
			Columns: []state.StatsColumn{
				{Name: "MILLIS", Width: 8, Right: true}, {Name: "TYPE", Width: 9},
				{Name: "NAMESPACE", Width: 32}, {Name: "WHEN", Width: 19}, {Name: "COMMAND"},
			},
		}
		if len(d.profiles) == 0 {
			profileSection.Empty = "No profiler data. Enable with db.setProfilingLevel(1) in the databases you care about."
		} else {
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
				profileSection.Rows = append(profileSection.Rows, []string{fmt.Sprintf("%.0f", ms), typ, ns, when, compactCmd(cmd, 2000)})
			}
		}
	}
	s.StatsSections = []state.StatsSection{operations, collections}
	if profileSection.Title != "" {
		s.StatsSections = append(s.StatsSections, profileSection)
	}
	var buf strings.Builder
	buf.WriteString(s.StatsUpdated + "\n")
	for _, section := range s.StatsSections {
		fmt.Fprintf(&buf, "\n%s %s\n", section.Title, section.Note)
		if section.Empty != "" {
			buf.WriteString(section.Empty + "\n")
			continue
		}
		for _, row := range section.Rows {
			buf.WriteString(strings.Join(row, "  ") + "\n")
		}
	}
	s.StatsText = buf.String()
	s.StatsRevision++
}

func renderTop(d statsData) state.StatsSection {
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
	section := state.StatsSection{
		Title: "Top Collections", Note: fmt.Sprintf("(%d) · cumulative since startup; rates from next refresh", len(rows)),
		Columns: []state.StatsColumn{
			{Name: "NAMESPACE", Width: 44}, {Name: "OPS/S", Width: 10, Right: true},
			{Name: "AVG MS", Width: 10, Right: true}, {Name: "TOTAL OPS", Width: 14, Right: true},
		},
	}
	if haveRates {
		section.Note = fmt.Sprintf("(%d) · last %.0fs", len(rows), elapsed)
	}
	if len(rows) == 0 {
		section.Empty = "No collection activity"
		return section
	}
	for _, r := range rows {
		section.Rows = append(section.Rows, []string{r.ns, fmt.Sprintf("%.1f", r.rate), fmt.Sprintf("%.2f", r.avgMs), fmt.Sprint(r.total)})
	}
	return section
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
