package documents

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tango/internal/state"
)

func ShowStats() {
	s := state.S
	ctx, cancel := state.NewCtx()
	defer cancel()

	var buf strings.Builder

	// ── Active Operations ──
	buf.WriteString("[cyan]━━━ Active Operations ━━━[white]\n\n")
	var currentOp bson.M
	if err := s.Client.Database("admin").RunCommand(ctx, bson.D{{Key: "currentOp", Value: 1}}).Decode(&currentOp); err != nil {
		fmt.Fprintf(&buf, "  [red]Error: %s[white]\n  (may require admin privileges)\n", err)
	} else if inprog, ok := currentOp["inprog"].(primitive.A); ok {
		count := 0
		fmt.Fprintf(&buf, "  [gray]%-12s %-8s %-30s %-10s %s[white]\n", "OPID", "TYPE", "NAMESPACE", "SECS", "COMMAND")
		for _, raw := range inprog {
			op, ok := raw.(bson.M)
			if !ok {
				continue
			}
			opType, _ := op["op"].(string)
			if opType == "none" {
				continue
			}
			opid := fmt.Sprintf("%v", op["opid"])
			ns, _ := op["ns"].(string)
			var secsRunning string
			if sr, ok := state.ToFloat64(op["secs_running"]); ok {
				secsRunning = fmt.Sprintf("%.0f", sr)
			} else {
				secsRunning = "-"
			}
			cmdStr := ""
			if cmd, ok := op["command"].(bson.M); ok {
				b, _ := json.Marshal(cmd)
				cmdStr = string(b)
			}
			if len(cmdStr) > 60 {
				cmdStr = cmdStr[:57] + "..."
			}
			fmt.Fprintf(&buf, "  [yellow]%-12s[white] %-8s %-30s %-10s %s\n", opid, opType, ns, secsRunning, cmdStr)
			count++
		}
		if count == 0 {
			buf.WriteString("  [gray](no active operations)[white]\n")
		}
	} else {
		buf.WriteString("  [gray](no active operations)[white]\n")
	}

	// ── Slow Queries ──
	buf.WriteString("\n[cyan]━━━ Slow Queries (top 10) ━━━[white]\n\n")
	profileDB := s.CurrentDB
	if profileDB == "" {
		profileDB = "admin"
	}
	profileCursor, profileErr := s.Client.Database(profileDB).Collection("system.profile").Find(ctx, bson.M{},
		options.Find().SetSort(bson.D{{Key: "millis", Value: -1}}).SetLimit(10))
	if profileErr != nil {
		buf.WriteString("  [gray]Profiling not enabled or not accessible.[white]\n")
		buf.WriteString("  [gray]Enable with: db.setProfilingLevel(1)[white]\n")
	} else {
		defer func() { _ = profileCursor.Close(ctx) }()
		var profiles []bson.M
		if err := profileCursor.All(ctx, &profiles); err != nil || len(profiles) == 0 {
			buf.WriteString("  [gray]Profiling not enabled or no slow queries recorded.[white]\n")
			buf.WriteString("  [gray]Enable with: db.setProfilingLevel(1)[white]\n")
		} else {
			fmt.Fprintf(&buf, "  [gray]%-10s %-8s %-30s %s[white]\n", "MILLIS", "TYPE", "NAMESPACE", "COMMAND")
			for _, p := range profiles {
				millis := ""
				if ms, ok := state.ToFloat64(p["millis"]); ok {
					millis = fmt.Sprintf("%.0f", ms)
				}
				opType, _ := p["op"].(string)
				ns, _ := p["ns"].(string)
				cmdStr := ""
				if cmd, ok := p["command"].(bson.M); ok {
					b, _ := json.Marshal(cmd)
					cmdStr = string(b)
				} else if q, ok := p["query"].(bson.M); ok {
					b, _ := json.Marshal(q)
					cmdStr = string(b)
				}
				if len(cmdStr) > 80 {
					cmdStr = cmdStr[:77] + "..."
				}
				fmt.Fprintf(&buf, "  [yellow]%-10s[white] %-8s %-30s %s\n", millis, opType, ns, cmdStr)
			}
		}
	}

	// ── Top Collections ──
	buf.WriteString("\n[cyan]━━━ Top Collections ━━━[white]\n\n")
	var top bson.M
	if err := s.Client.Database("admin").RunCommand(ctx, bson.D{{Key: "top", Value: 1}}).Decode(&top); err != nil {
		fmt.Fprintf(&buf, "  [red]Error: %s[white]\n  (may require admin privileges)\n", err)
	} else if totals, ok := top["totals"].(bson.M); ok {
		type collStat struct {
			ns    string
			count int64
			time  int64
		}
		var stats []collStat
		for ns, val := range totals {
			if strings.Contains(ns, "system.") || strings.HasPrefix(ns, "local.") {
				continue
			}
			m, ok := val.(bson.M)
			if !ok {
				continue
			}
			totalM, ok := m["total"].(bson.M)
			if !ok {
				continue
			}
			var c, t int64
			if v, ok := state.ToFloat64(totalM["count"]); ok {
				c = int64(v)
			}
			if v, ok := state.ToFloat64(totalM["time"]); ok {
				t = int64(v)
			}
			if c > 0 {
				stats = append(stats, collStat{ns: ns, count: c, time: t})
			}
		}
		sort.Slice(stats, func(i, j int) bool {
			return stats[i].count > stats[j].count
		})
		if len(stats) > 10 {
			stats = stats[:10]
		}
		if len(stats) == 0 {
			buf.WriteString("  [gray](no collection activity)[white]\n")
		} else {
			fmt.Fprintf(&buf, "  [gray]%-40s %12s %14s[white]\n", "NAMESPACE", "TOTAL OPS", "TOTAL TIME µs")
			for _, stat := range stats {
				fmt.Fprintf(&buf, "  [yellow]%-40s[white] %12d %14d\n", stat.ns, stat.count, stat.time)
			}
		}
	} else {
		buf.WriteString("  [gray](no data)[white]\n")
	}

	s.StatsView.SetText(buf.String())
	s.StatsView.ScrollToBeginning()
	s.DocPages.SwitchToPage("stats")
	s.App.SetFocus(s.StatsView)
	state.RefreshPanelBorders()
}
