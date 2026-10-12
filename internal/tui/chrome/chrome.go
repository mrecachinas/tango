// Package chrome renders tango's borderless header and context-sensitive footer.
package chrome

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
)

const (
	OK   = "ok"
	Info = "info"
	Warn = "warn"
	Err  = "err"
)

type Badge struct {
	Text string
	Kind string
}

func kindStyle(kind string) lipgloss.Style {
	t := theme.Current()
	switch strings.ToLower(kind) {
	case OK:
		return t.OK
	case Info:
		return t.Info
	case Warn:
		return t.Warn
	case Err:
		return t.Err
	default:
		return t.Text
	}
}

func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\x1b' {
			return ' '
		}
		return r
	}, s)
}

func plain(s string) string { return oneLine(ansi.Strip(s)) }

func fitWidth(s string, width int) string {
	s = theme.FitWidth(s, width)
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

// sides keeps the right-hand status anchored to the final cell. Its callers
// reserve space first, so truncation never lets hints overwrite status.
func sides(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	if right == "" {
		return fitWidth(left, width)
	}
	right = ansi.Truncate(right, width, "…")
	room := width - ansi.StringWidth(right)
	if left == "" {
		return strings.Repeat(" ", room) + right
	}
	return fitWidth(left, max(0, room-1)) + strings.Repeat(" ", min(1, room)) + right
}
