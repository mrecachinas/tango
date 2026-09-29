package metrics

import (
	"fmt"
	"strings"
	"time"

	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

func extractUptime(ss bson.M) string {
	if u, ok := state.ToFloat64(ss["uptime"]); ok {
		return formatDuration(time.Duration(u) * time.Second)
	}
	return "?"
}

func extractConnections(ss bson.M) (current, available int) {
	if ss == nil {
		return 0, 0
	}
	if conns, ok := ss["connections"].(bson.M); ok {
		if c, ok := state.ToFloat64(conns["current"]); ok {
			current = int(c)
		}
		if a, ok := state.ToFloat64(conns["available"]); ok {
			available = int(a)
		}
	}
	return
}

func extractCachePercent(ss bson.M) float64 {
	if ss == nil {
		return 0
	}
	wt, ok := ss["wiredTiger"].(bson.M)
	if !ok {
		return 0
	}
	cache, ok := wt["cache"].(bson.M)
	if !ok {
		return 0
	}
	inCache, ok1 := state.ToFloat64(cache["bytes currently in the cache"])
	maxCache, ok2 := state.ToFloat64(cache["maximum bytes configured"])
	if ok1 && ok2 && maxCache > 0 {
		return inCache / maxCache * 100
	}
	return 0
}

// ticketSections returns the read/write execution ticket stats:
// queues.execution on MongoDB 7.0+/8.x, wiredTiger.concurrentTransactions before.
func ticketSections(ss bson.M) []bson.M {
	var ct bson.M
	if q, ok := ss["queues"].(bson.M); ok {
		ct, _ = q["execution"].(bson.M)
	}
	if ct == nil {
		if wt, ok := ss["wiredTiger"].(bson.M); ok {
			ct, _ = wt["concurrentTransactions"].(bson.M)
		}
	}
	var out []bson.M
	for _, k := range []string{"read", "write"} {
		if sec, ok := ct[k].(bson.M); ok {
			out = append(out, sec)
		}
	}
	return out
}

// extractTicketsPct returns the percentage of execution tickets in use.
func extractTicketsPct(ss bson.M) float64 {
	var avail, total float64
	for _, sec := range ticketSections(ss) {
		a, _ := state.ToFloat64(sec["available"])
		t, _ := state.ToFloat64(sec["totalTickets"])
		avail += a
		total += t
	}
	if total > 0 {
		return (1 - avail/total) * 100
	}
	return 0
}

// queueDepth returns the number of operations waiting for an execution
// ticket, falling back to globalLock.currentQueue on old servers.
func queueDepth(ss bson.M) float64 {
	var n float64
	found := false
	for _, sec := range ticketSections(ss) {
		if v, ok := state.ToFloat64(sec["queueLength"]); ok {
			n += v
			found = true
			continue
		}
		// 8.x reports queue lengths per priority class.
		for _, sub := range sec {
			if m, ok := sub.(bson.M); ok {
				if v, ok := state.ToFloat64(m["queueLength"]); ok {
					n += v
					found = true
				}
			}
		}
	}
	if found {
		return n
	}
	if gl, ok := ss["globalLock"].(bson.M); ok {
		if cq, ok := gl["currentQueue"].(bson.M); ok {
			r, _ := state.ToFloat64(cq["readers"])
			w, _ := state.ToFloat64(cq["writers"])
			return r + w
		}
	}
	return 0
}

func sumOpcounters(ss bson.M) (float64, bool) {
	oc, ok := ss["opcounters"].(bson.M)
	if !ok {
		return 0, false
	}
	var total float64
	for _, key := range []string{"insert", "query", "update", "delete", "getmore", "command"} {
		if v, ok := state.ToFloat64(oc[key]); ok {
			total += v
		}
	}
	return total, true
}

func networkBytesIn(ss bson.M) (float64, bool) {
	if net, ok := ss["network"].(bson.M); ok {
		return state.ToFloat64(net["bytesIn"])
	}
	return 0, false
}

func escape(s string) string { return tview.Escape(s) }

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd%dh%dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func renderSparkline(data []float64) string {
	if len(data) == 0 {
		return strings.Repeat(string(sparkChars[0]), SparkWidth)
	}
	start := 0
	if len(data) > SparkWidth {
		start = len(data) - SparkWidth
	}
	visible := data[start:]

	min, max := visible[0], visible[0]
	for _, v := range visible {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}

	var buf strings.Builder
	for i := 0; i < SparkWidth-len(visible); i++ {
		buf.WriteRune(sparkChars[0])
	}
	rng := max - min
	for _, v := range visible {
		idx := 0
		if rng > 0 {
			idx = int((v - min) / rng * float64(len(sparkChars)-1))
		}
		if idx >= len(sparkChars) {
			idx = len(sparkChars) - 1
		}
		buf.WriteRune(sparkChars[idx])
	}
	return buf.String()
}

func renderGauge(percent float64) string {
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}
	filled := int(percent / 100 * float64(gaugeWidth))
	return "[green]" + strings.Repeat("█", filled) + "[gray]" + strings.Repeat("░", gaugeWidth-filled) + "[white]"
}

// visibleLen counts the number of visible runes in a string,
// ignoring tview color tags like [white], [yellow], etc.
func visibleLen(s string) int {
	n := 0
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if runes[i] == '[' {
			// Look for closing ]
			j := i + 1
			for j < len(runes) && runes[j] != ']' {
				j++
			}
			if j < len(runes) {
				// Skip the entire tag (color tags are zero-width)
				i = j + 1
				continue
			}
		}
		n++
		i++
	}
	return n
}

// padCell pads a string with spaces to a fixed visible width.
// Visible width ignores tview color tags.
func padCell(s string, width int) string {
	vl := visibleLen(s)
	if vl >= width {
		return s
	}
	return s + strings.Repeat(" ", width-vl)
}

// metricCell formats a single metric as: "label viz value" padded to cellWidth.
func metricCell(label, viz, value string, cellWidth int) string {
	content := fmt.Sprintf("[gray]%s [yellow]%s[white] %s", label, viz, value)
	return padCell(content, cellWidth)
}

// metricGaugeCell formats a gauge metric as: "label gauge value" padded to cellWidth.
func metricGaugeCell(label, gauge, value string, cellWidth int) string {
	content := fmt.Sprintf("[gray]%s %s %s", label, gauge, value)
	return padCell(content, cellWidth)
}
