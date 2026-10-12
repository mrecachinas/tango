package chrome

import (
	"strings"

	"charm.land/bubbles/v2/spinner"
	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
)

// Hints are ordered highest priority first; the footer drops from the end.
type Hint struct {
	Key, Desc string
}

type FooterState struct {
	Mode             string
	Hints            []Hint
	Flash, FlashKind string
	Loading, Spinner string
	Override         string
}

// DefaultHints returns a fresh list; nil FooterState.Hints selects this default,
// while a non-nil empty list explicitly suppresses hints.
func DefaultHints() []Hint {
	return []Hint{
		{Key: "enter", Desc: "open"},
		{Key: "space", Desc: "select"},
		{Key: "/", Desc: "query"},
		{Key: ":", Desc: "cmd"},
		{Key: "?", Desc: "help"},
	}
}

// NewSpinner is ready for the root to embed, Tick and Update normally.
func NewSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(theme.Current().Muted))
}

// Footer returns one line of exactly width cells. A flash takes precedence
// over loading, both take precedence over hints, and Override replaces both.
func Footer(f FooterState, width int) string {
	if width <= 0 {
		return ""
	}
	t := theme.Current()
	mode := strings.ToUpper(strings.TrimSpace(plain(f.Mode)))
	if mode == "" {
		mode = "NORMAL"
	}
	pillStyle := t.ModePill
	switch mode {
	case "READ-ONLY":
		if !t.Mono {
			pillStyle = pillStyle.Background(t.P.Warn).Foreground(t.P.OnAcc)
		}
	case "LIVE":
		if !t.Mono {
			pillStyle = pillStyle.Background(t.P.Err).Foreground(t.P.OnAcc)
		}
	}
	pill := pillStyle.Render(mode)
	if f.Override != "" {
		return fitWidth(pill+" "+oneLine(f.Override), width)
	}
	if ansi.StringWidth(pill) >= width {
		return fitWidth(pill, width)
	}
	right := ""
	switch {
	case f.Flash != "":
		glyph := "✓ "
		if strings.EqualFold(f.FlashKind, Err) {
			glyph = "✗ "
		}
		kind := f.FlashKind
		if kind == "" {
			kind = Info
		}
		right = kindStyle(kind).Render(glyph + plain(f.Flash))
	case f.Loading != "":
		frame := plain(f.Spinner)
		if frame == "" {
			frame = spinner.MiniDot.Frames[0]
		}
		right = t.Muted.Render(frame + " " + plain(f.Loading))
	}
	right = ansi.Truncate(right, width-ansi.StringWidth(pill)-1, "…")
	hints := f.Hints
	if hints == nil {
		hints = DefaultHints()
	}
	left := pill
	for _, hint := range hints {
		part := t.Key.Render(plain(hint.Key)) + " " + t.KeyDesc.Render(plain(hint.Desc))
		sep := " "
		if left != pill {
			sep = t.Faint.Render(" · ")
		}
		candidate := left + sep + part
		need := ansi.StringWidth(candidate + right)
		if right != "" {
			need++
		}
		if need > width {
			break
		}
		left = candidate
	}
	return sides(left, right, width)
}
