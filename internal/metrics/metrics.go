package metrics

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/state"
)

const (
	SparkWidth      = 20
	gaugeWidth      = 15
	metricsInterval = 2 * time.Second
)

var sparkChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// CollectMetricsData performs all network I/O for metrics and returns the
// formatted text. Safe to call from any goroutine.
func CollectMetricsData() string {
	s := state.S
	ctx, cancel := context.WithTimeout(context.Background(), s.QueryTimeout/2)
	defer cancel()

	host := fmt.Sprintf("%s:%d", s.GlobalCfg.MongoURL, s.GlobalCfg.MongoPort)

	// Server build info
	var buildInfo bson.M
	version := "?"
	if err := s.Client.Database("admin").RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&buildInfo); err == nil {
		if v, ok := buildInfo["version"].(string); ok {
			version = v
		}
	}

	// Server status
	var ss bson.M
	uptime := "?"
	if err := s.Client.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&ss); err == nil {
		uptime = extractUptime(ss)
		collectMetricsSample(ss)
	}

	// Replica set status
	var replStatus bson.M
	if err := s.Client.Database("admin").RunCommand(ctx, bson.D{{Key: "replSetGetStatus", Value: 1}}).Decode(&replStatus); err == nil {
		s.IsReplicaSet = true
		if members, ok := replStatus["members"].(bson.A); ok {
			var primaryOptime, maxSecondaryOptime time.Time
			for _, m := range members {
				member, ok := m.(bson.M)
				if !ok {
					continue
				}
				stateVal, _ := state.ToFloat64(member["state"])
				if optime, ok := member["optimeDate"].(primitive.DateTime); ok {
					t := optime.Time()
					if int(stateVal) == 1 {
						primaryOptime = t
					} else if int(stateVal) == 2 {
						if t.After(maxSecondaryOptime) {
							maxSecondaryOptime = t
						}
					}
				}
			}
			if !primaryOptime.IsZero() && !maxSecondaryOptime.IsZero() {
				s.ReplLagMs = float64(primaryOptime.Sub(maxSecondaryOptime).Milliseconds())
				if s.ReplLagMs < 0 {
					s.ReplLagMs = 0
				}
			}
		}
	} else {
		s.IsReplicaSet = false
	}

	// Data counts (from cache, updated by browse operations)
	dbCount := s.CachedDBCount
	totalColls := s.CachedCollCount

	connCur, connAvail := extractConnections(ss)
	cachePercent := extractCachePercent(ss)

	// WT ticket usage
	ticketsPct := extractTicketsPct(ss)

	connIndicator := "[green]●[white]"
	if s.ReadOnly {
		connIndicator = "[yellow]◐[white] [gray](read-only)[white]"
	}

	line1 := fmt.Sprintf(
		" %s %s   [gray]MongoDB[white] v%s   [gray]uptime[white] %s   [aqua]%d[white] dbs · [aqua]%d[white] collections",
		connIndicator, host, version, uptime, dbCount, totalColls,
	)

	const cellWidth = 36 // fixed visible width per metric cell
	sep := "  "         // separator between cells

	line2 := " " +
		metricCell("ops/s", renderSparkline(s.OpsHistory.Data), fmt.Sprintf("%-8.0f", s.OpsHistory.Last()), cellWidth) + sep +
		metricCell("conns", renderSparkline(s.ConnHistory.Data), fmt.Sprintf("%-8s", fmt.Sprintf("%d/%d", connCur, connCur+connAvail)), cellWidth) + sep +
		metricCell("ping ", renderSparkline(s.PingHistory.Data), fmt.Sprintf("%-8s", fmt.Sprintf("%.1fms", s.PingHistory.Last())), cellWidth)

	line3 := " " +
		metricCell("mem  ", renderSparkline(s.MemHistory.Data), fmt.Sprintf("%-8s", fmt.Sprintf("%.0fMB", s.MemHistory.Last())), cellWidth) + sep +
		metricGaugeCell("cache", renderGauge(cachePercent), fmt.Sprintf("%-8s", fmt.Sprintf("%.0f%%", cachePercent)), cellWidth) + sep +
		metricCell("net  ", renderSparkline(s.NetInHistory.Data), fmt.Sprintf("%-8s", fmt.Sprintf("%.0fKB/s", s.NetInHistory.Last())), cellWidth)

	line4 := " " +
		metricCell("queue", renderSparkline(s.QueueHistory.Data), fmt.Sprintf("%-8.0f", s.QueueHistory.Last()), cellWidth) + sep +
		metricCell("curs ", renderSparkline(s.CursorsHistory.Data), fmt.Sprintf("%-8.0f", s.CursorsHistory.Last()), cellWidth) + sep +
		metricGaugeCell("tix  ", renderGauge(ticketsPct), fmt.Sprintf("%-8s", fmt.Sprintf("%.0f%%", ticketsPct)), cellWidth)

	if s.IsReplicaSet {
		lagColor := "[green]"
		if s.ReplLagMs > 1000 {
			lagColor = "[yellow]"
		}
		if s.ReplLagMs > 10000 {
			lagColor = "[red]"
		}
		line4 += fmt.Sprintf("   [gray]repl lag %s%.0fms[white]", lagColor, s.ReplLagMs)
	}

	return line1 + "\n" + line2 + "\n" + line3 + "\n" + line4
}

// RefreshMetrics collects metrics data and updates the UI. Must be called on
// the UI thread (or inside QueueUpdateDraw).
func RefreshMetrics() {
	state.S.MetricsBar.SetText(CollectMetricsData())
}

func collectMetricsSample(ss bson.M) {
	s := state.S

	// Measure ping latency with a short dedicated timeout
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
	pingStart := time.Now()
	var pingResult bson.M
	_ = s.Client.Database("admin").RunCommand(pingCtx, bson.D{{Key: "ping", Value: 1}}).Decode(&pingResult)
	pingMs := float64(time.Since(pingStart).Microseconds()) / 1000.0
	pingCancel()
	s.PingHistory.Push(pingMs)

	// ops/sec from opcounters delta
	if oc, ok := ss["opcounters"].(bson.M); ok {
		total := sumOpcounters(oc)
		if s.PrevOpsTotal > 0 {
			delta := float64(total-s.PrevOpsTotal) / metricsInterval.Seconds()
			if delta < 0 {
				delta = 0
			}
			s.OpsHistory.Push(delta)
		}
		s.PrevOpsTotal = total
	}

	// connections
	cur, _ := extractConnections(ss)
	s.ConnHistory.Push(float64(cur))

	// memory
	if mem, ok := ss["mem"].(bson.M); ok {
		if res, ok := state.ToFloat64(mem["resident"]); ok {
			s.MemHistory.Push(res)
		}
	}

	// queue depth
	if gl, ok := ss["globalLock"].(bson.M); ok {
		if cq, ok := gl["currentQueue"].(bson.M); ok {
			readers, _ := state.ToFloat64(cq["readers"])
			writers, _ := state.ToFloat64(cq["writers"])
			s.QueueHistory.Push(readers + writers)
		}
	}

	// network I/O rate (KB/s)
	if net, ok := ss["network"].(bson.M); ok {
		if bytesIn, ok := state.ToFloat64(net["bytesIn"]); ok {
			if s.PrevNetIn > 0 {
				rate := (bytesIn - float64(s.PrevNetIn)) / metricsInterval.Seconds() / 1024
				if rate < 0 {
					rate = 0
				}
				s.NetInHistory.Push(rate)
			}
			s.PrevNetIn = int64(bytesIn)
		}
	}

	// open cursors
	if m, ok := ss["metrics"].(bson.M); ok {
		if cur, ok := m["cursor"].(bson.M); ok {
			if open, ok := cur["open"].(bson.M); ok {
				if total, ok := state.ToFloat64(open["total"]); ok {
					s.CursorsHistory.Push(total)
				}
			}
		}
	}

	// page faults
	if ei, ok := ss["extra_info"].(bson.M); ok {
		if faults, ok := state.ToFloat64(ei["page_faults"]); ok {
			s.FaultsHistory.Push(faults)
		}
	}
}

func StartMetricsPoller() {
	s := state.S
	go func() {
		ticker := time.NewTicker(metricsInterval)
		defer ticker.Stop()
		for range ticker.C {
			text := CollectMetricsData()
			s.App.QueueUpdateDraw(func() {
				s.MetricsBar.SetText(text)
			})
		}
	}()
}
