package chrome

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"

	"tango/internal/theme"
)

var sparkChars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline scales the newest width values to eight block heights. Missing
// history and flat series are faint baselines, not apparent activity.
func Sparkline(data []float64, width int) string {
	if width <= 0 {
		return ""
	}
	t := theme.Current()
	if len(data) > width {
		data = data[len(data)-width:]
	}
	baseline := strings.Repeat(string(sparkChars[0]), width)
	if len(data) == 0 {
		return t.Faint.Render(baseline)
	}
	finite := func(v float64) float64 {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0
		}
		return v
	}
	lo, hi := finite(data[0]), finite(data[0])
	for _, v := range data {
		v = finite(v)
		lo, hi = min(lo, v), max(hi, v)
	}
	if lo == hi {
		return t.Faint.Render(baseline)
	}
	var out strings.Builder
	for _, v := range data {
		fraction := (finite(v) - lo) / (hi - lo)
		if math.IsInf(hi-lo, 0) {
			fraction = (finite(v)/2 - lo/2) / (hi/2 - lo/2)
		}
		idx := min(len(sparkChars)-1, max(0, int(fraction*float64(len(sparkChars)-1))))
		out.WriteRune(sparkChars[idx])
	}
	if len(data) == width {
		return t.Accent.Render(out.String())
	}
	return t.Faint.Render(strings.Repeat(string(sparkChars[0]), width-len(data))) + t.Accent.Render(out.String())
}

func gaugeStyle(percent float64) lipgloss.Style {
	t := theme.Current()
	switch {
	case percent < 70:
		return t.OK
	case percent < 90:
		return t.Warn
	default:
		return t.Err
	}
}

// Gauge clamps percentages to 0…100; colour signals utilisation thresholds.
func Gauge(percent float64, width int) string {
	if width <= 0 {
		return ""
	}
	if math.IsNaN(percent) {
		percent = 0
	}
	percent = min(100, max(0, percent))
	filled := int(percent / 100 * float64(width))
	if filled == 0 {
		return theme.Current().Faint.Render(strings.Repeat("░", width))
	}
	if filled == width {
		return gaugeStyle(percent).Render(strings.Repeat("█", width))
	}
	return gaugeStyle(percent).Render(strings.Repeat("█", filled)) +
		theme.Current().Faint.Render(strings.Repeat("░", width-filled))
}
