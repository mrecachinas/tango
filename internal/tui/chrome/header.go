package chrome

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tango/internal/metrics"
	"tango/internal/theme"
)

const miniSparkWidth = 8

// HeaderHeight is independent of terminal width, so toggling the metric grid
// never surprises the layout. The root owns the M binding.
func HeaderHeight(expanded bool) int {
	if expanded {
		return 4
	}
	return 1
}

// Header returns exactly HeaderHeight(expanded) lines of width cells.
// READ-ONLY and replica-health badges are inferred from the snapshot; badges
// adds root-owned state such as LIVE. Identical badge text is not repeated.
func Header(snap metrics.Snapshot, badges []Badge, width int, expanded bool) string {
	line := compactHeader(snap, headerBadges(snap, badges), width, expanded)
	if !expanded {
		return line
	}
	return line + "\n" + expandedHeader(snap, width)
}

func headerBadges(snap metrics.Snapshot, badges []Badge) []Badge {
	var out []Badge
	if snap.ReadOnly {
		out = append(out, Badge{Text: "READ-ONLY", Kind: Warn})
	}
	out = append(out, badges...)
	if rs := snap.ReplicaSet; rs != nil && rs.Members > 0 {
		kind := OK
		if !snap.Connected || snap.Err != nil || rs.Healthy < rs.Members {
			kind = Err
		}
		out = append(out, Badge{Text: fmt.Sprintf("rs %d/%d", rs.Healthy, rs.Members), Kind: kind})
	}
	seen := make(map[string]bool)
	unique := out[:0]
	for _, badge := range out {
		badge.Text = strings.TrimSpace(plain(badge.Text))
		if badge.Text != "" && !seen[badge.Text] {
			seen[badge.Text] = true
			unique = append(unique, badge)
		}
	}
	return unique
}

func compactHeader(snap metrics.Snapshot, badges []Badge, width int, expanded bool) string {
	if width <= 0 {
		return ""
	}
	t := theme.Current()
	var badgeParts []string
	// Keep a readable connection identity even when safety badges are present.
	for _, badge := range badges {
		part := kindStyle(badge.Kind).Render(badge.Text)
		candidate := strings.Join(append(badgeParts, part), "  ")
		if ansi.StringWidth(candidate)+2+min(8, width) > width {
			break
		}
		badgeParts = append(badgeParts, part)
	}
	right := strings.Join(badgeParts, "  ")
	room := width
	if right != "" {
		room -= ansi.StringWidth(right) + 2
	}
	host := strings.TrimSpace(plain(snap.HostLabel))
	if host == "" {
		host = "connecting…"
	}
	connected := snap.Connected && snap.Err == nil
	dot := t.OK.Render("●")
	if !connected {
		dot = t.Err.Render("●")
		reason := "connecting"
		if snap.Err != nil {
			reason = plain(snap.Err.Error())
		}
		status := t.Err.Render("disconnected") + " " + t.Muted.Render("("+reason+")")
		// Keep the error status before its lower-priority host and reason when
		// the terminal is too narrow to display all three.
		if ansi.StringWidth(dot+" "+host+" · "+status) > room {
			left := dot + " " + status
			return sides(ansi.Truncate(left, room, "…"), right, width)
		}
		left := dot + " " + t.Text.Render(host) + t.Faint.Render(" · ") + status
		return sides(left, right, width)
	}
	identity := dot + " " + t.Text.Render(host)
	version := plain(snap.Version)
	if version == "" {
		version = "?"
	}
	identity += t.Faint.Render(" · ") + t.Muted.Render("v") + t.Text.Render(version)
	uptime := t.Faint.Render(" · ") + t.Muted.Render("up ") + t.Text.Render(duration(snap.Uptime))
	ops := t.Muted.Render("ops ") + t.Text.Render(fmt.Sprintf("%.0f/s", snap.OpsPerSec))
	spark := " " + Sparkline(snap.History.Ops, miniSparkWidth)
	conns := "  " + t.Muted.Render("conns ") + t.Text.Render(fmt.Sprintf("%d", snap.ConnectionsCurrent))
	ping := "  " + t.Muted.Render("ping ") + t.Text.Render(fmt.Sprintf("%.1fms", snap.PingMS))
	mem := "  " + t.Muted.Render("mem ") + t.Text.Render(fmt.Sprintf("%.0fMB", snap.MemoryMB))
	build := func(drop int) string {
		left := identity
		if drop < 6 {
			left += uptime
		}
		if !expanded && drop < 5 {
			left += "   " + ops
			if drop < 4 {
				left += spark
			}
			if drop < 2 {
				left += conns
			}
			if drop < 3 {
				left += ping
			}
			if drop < 1 {
				left += mem
			}
		}
		return left
	}
	for drop := 0; drop <= 6; drop++ {
		left := build(drop)
		if ansi.StringWidth(left) <= room {
			return sides(left, right, width)
		}
	}
	return sides(ansi.Truncate(identity, room, "…"), right, width)
}

func duration(d time.Duration) string {
	d = max(0, d)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd%dh%dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

type metric struct {
	label, value string
	history      []float64
	gauge        bool
	percent      float64
}

func expandedHeader(snap metrics.Snapshot, width int) string {
	h := snap.History
	cells := []metric{
		{label: "ops", value: fmt.Sprintf("%.0f/s", snap.OpsPerSec), history: h.Ops},
		{label: "conns", value: fmt.Sprintf("%d · %s avail", snap.ConnectionsCurrent, compactCount(snap.ConnectionsAvailable)), history: h.Connections},
		{label: "ping", value: fmt.Sprintf("%.1fms", snap.PingMS), history: h.Ping},
		{label: "mem", value: fmt.Sprintf("%.0fMB", snap.MemoryMB), history: h.Memory},
		{label: "net", value: fmt.Sprintf("↓%.0f KB/s ↑%.0f KB/s", snap.NetInKBPerSec, snap.NetOutKBPerSec), history: h.NetIn},
		{label: "queue", value: fmt.Sprintf("%.0f", snap.Queue), history: h.Queue},
		{label: "cursors", value: fmt.Sprintf("%.0f", snap.Cursors), history: h.Cursors},
		{label: "cache", value: fmt.Sprintf("%.0f%%", snap.CachePercent), gauge: true, percent: snap.CachePercent},
		{label: "tickets", value: fmt.Sprintf("%.0f%%", snap.TicketsPercent), gauge: true, percent: snap.TicketsPercent},
	}
	// Three columns is the useful maximum for nine metrics in three rows.
	cols := min(3, max(1, (width+2)/38))
	cellWidth := max(0, (width-(cols-1)*2)/cols)
	valueWidths := make([]int, cols)
	for col := range cols {
		for row := range 3 {
			valueWidths[col] = max(valueWidths[col], ansi.StringWidth(cells[row*cols+col].value))
		}
	}
	rows := make([]string, 3)
	for row := range rows {
		var parts []string
		for col := 0; col < cols; col++ {
			parts = append(parts, metricCell(cells[row*cols+col], cellWidth, valueWidths[col]))
		}
		rows[row] = fitWidth(strings.Join(parts, "  "), width)
	}
	return strings.Join(rows, "\n")
}

func compactCount(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprint(n)
}

func metricCell(m metric, width, valueWidth int) string {
	t := theme.Current()
	label := t.Muted.Render(fitWidth(m.label, 7))
	value := t.Text.Render(m.value)
	if m.gauge {
		value = gaugeStyle(m.percent).Render(m.value)
	}
	valueWidth = min(valueWidth, max(0, width-8))
	value = fitWidth(value, valueWidth)
	vizWidth := min(miniSparkWidth, max(0, width-8-valueWidth-1))
	if vizWidth == 0 {
		return fitWidth(label+" "+value, width)
	}
	viz := Sparkline(m.history, vizWidth)
	if m.gauge {
		viz = Gauge(m.percent, vizWidth)
	}
	return fitWidth(label+" "+value+" "+viz, width)
}
