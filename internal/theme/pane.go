package theme

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Pane frames body in a rounded border of exactly width×height cells with
// title (and optional right-aligned info) embedded in the top edge:
//
//	╭─ Documents ───────── 1–50 of 500 ─╮
//
// title and info may already contain ANSI styling; when they don't, the
// focused/unfocused title style is applied. body lines are truncated or
// padded to fit; extra lines are dropped.
func Pane(title, info, body string, width, height int, focused bool) string {
	t := Current()
	if width < 4 || height < 2 {
		return ""
	}
	bs, ts := t.Pane, t.Title
	if focused {
		bs, ts = t.PaneFocused, t.TitleFocused
	}
	edge := lipgloss.NewStyle().Foreground(bs.GetBorderTopForeground())
	b := lipgloss.RoundedBorder()
	inner := width - 2

	styled := func(s string) string {
		if s == "" || strings.Contains(s, "\x1b[") {
			return s
		}
		return ts.Render(s)
	}
	// Top edge: ╭─ title ─────── info ─╮
	var top strings.Builder
	top.WriteString(edge.Render(b.TopLeft + b.Top))
	used := 1
	if title != "" {
		seg := " " + styled(title) + " "
		seg = ansi.Truncate(seg, inner-2, "…")
		top.WriteString(seg)
		used += ansi.StringWidth(seg)
	}
	infoSeg := ""
	if info != "" {
		infoSeg = " " + styled(info) + " "
		if used+ansi.StringWidth(infoSeg)+1 > inner {
			infoSeg = ""
		}
	}
	fill := inner - used - ansi.StringWidth(infoSeg) - 1
	if fill < 0 {
		fill = 0
	}
	top.WriteString(edge.Render(strings.Repeat(b.Top, fill)))
	if infoSeg != "" {
		top.WriteString(infoSeg + edge.Render(b.Top))
	} else if used+fill < inner {
		top.WriteString(edge.Render(strings.Repeat(b.Top, inner-used-fill)))
	}
	top.WriteString(edge.Render(b.TopRight))

	lines := strings.Split(body, "\n")
	rows := height - 2
	out := make([]string, 0, height)
	out = append(out, top.String())
	l, r := edge.Render(b.Left), edge.Render(b.Right)
	for i := 0; i < rows; i++ {
		var line string
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, l+FitWidth(line, inner)+r)
	}
	out = append(out, edge.Render(b.BottomLeft+strings.Repeat(b.Bottom, inner)+b.BottomRight))
	return strings.Join(out, "\n")
}

// FitWidth truncates (with "…") or right-pads s to exactly w cells,
// preserving ANSI styling.
func FitWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := ansi.StringWidth(s)
	if sw > w {
		s = ansi.Truncate(s, w, "…")
		sw = ansi.StringWidth(s)
	}
	return s + strings.Repeat(" ", w-sw)
}

var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// CursorLine fills every cell, including gaps and padding, without replacing
// semantic foregrounds. Reapply the fill after inner ANSI resets so they
// cannot punch holes in the row. Mono themes use reverse instead.
func CursorLine(s string, width int) string {
	s = FitWidth(s, width)
	if width <= 0 {
		return s
	}
	fill := Current().CursorRow.Render(" ")
	prefix := fill[:strings.IndexByte(fill, ' ')]
	if prefix == "" {
		return s
	}
	return prefix + sgr.ReplaceAllStringFunc(s, func(seq string) string {
		return seq + prefix
	}) + "\x1b[m"
}
