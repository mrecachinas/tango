// Package theme holds tango's design tokens: one accent, a neutral grey
// ramp, semantic state colours, and JSON syntax colours. Every UI component
// styles itself from Current() so skins and NO_COLOR apply everywhere.
//
// Design rules:
//   - Never paint a background behind content; the terminal's own background
//     shows through. The only filled areas are the cursor row, the mode pill,
//     and search matches.
//   - Colour carries meaning (focus, state, data type), never decoration.
//   - Focus is shown by the accent border and title; everything else is grey.
package theme

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// Palette is the full set of colour tokens. Nil means "terminal default".
type Palette struct {
	Text    color.Color // primary text; nil = terminal foreground
	Muted   color.Color // secondary text: labels, counts, hints
	Faint   color.Color // tertiary text: separators, disabled, placeholders
	Border  color.Color // unfocused pane borders
	Accent  color.Color // focus, active title, cursor marker, key hints
	OnAcc   color.Color // text drawn on an Accent fill (mode pill)
	Cursor  color.Color // cursor-row background
	Select  color.Color // multi-select marker / selected-row tint
	Match   color.Color // search-match background
	OnMatch color.Color // text on Match

	OK   color.Color
	Warn color.Color
	Err  color.Color
	Info color.Color

	// JSON syntax.
	Key    color.Color
	String color.Color
	Number color.Color
	Bool   color.Color
	Null   color.Color
	Helper color.Color // ObjectId(…), ISODate(…), etc.
	Punct  color.Color
}

func hex(s string) color.Color { return lipgloss.Color(s) }

// Dark is the default palette, tuned for dark terminal backgrounds.
func Dark() Palette {
	return Palette{
		Muted:   hex("#8A8F98"),
		Faint:   hex("#565B63"),
		Border:  hex("#3A3F47"),
		Accent:  hex("#FFA94D"), // mango
		OnAcc:   hex("#1A1B1E"),
		Cursor:  hex("#2B3038"),
		Select:  hex("#7AA2F7"),
		Match:   hex("#FFD166"),
		OnMatch: hex("#1A1B1E"),
		OK:      hex("#7BD88F"),
		Warn:    hex("#F5C451"),
		Err:     hex("#FF6B6B"),
		Info:    hex("#6CB6FF"),
		Key:     hex("#9CB8D9"),
		String:  hex("#A6D99B"),
		Number:  hex("#F2B880"),
		Bool:    hex("#D3A6F0"),
		Null:    hex("#8A8F98"),
		Helper:  hex("#7FD1C7"),
		Punct:   hex("#6B7079"),
	}
}

// Light is tuned for light terminal backgrounds.
func Light() Palette {
	return Palette{
		Muted:   hex("#6A6F78"),
		Faint:   hex("#A3A8B0"),
		Border:  hex("#D0D4DA"),
		Accent:  hex("#D9480F"),
		OnAcc:   hex("#FFFFFF"),
		Cursor:  hex("#E8ECF2"),
		Select:  hex("#3B5BDB"),
		Match:   hex("#FFE066"),
		OnMatch: hex("#1A1B1E"),
		OK:      hex("#2B8A3E"),
		Warn:    hex("#B07D00"),
		Err:     hex("#C92A2A"),
		Info:    hex("#1864AB"),
		Key:     hex("#364FC7"),
		String:  hex("#2B8A3E"),
		Number:  hex("#C2410C"),
		Bool:    hex("#862E9C"),
		Null:    hex("#6A6F78"),
		Helper:  hex("#0B7285"),
		Punct:   hex("#868E96"),
	}
}

// Mono uses only terminal defaults plus bold/faint/reverse; used for
// NO_COLOR and the "mono" skin.
func Mono() Palette { return Palette{} }

// Theme is the palette plus derived lipgloss styles.
type Theme struct {
	P    Palette
	Mono bool

	Text, Muted, Faint       lipgloss.Style
	Accent, AccentBold       lipgloss.Style
	OK, Warn, Err, Info      lipgloss.Style
	Title, TitleFocused      lipgloss.Style // pane titles
	Pane, PaneFocused        lipgloss.Style // rounded pane borders (no padding)
	CursorRow                lipgloss.Style // full-width cursor-row fill
	SelectMark               lipgloss.Style // "●" marker for multi-selected rows
	MatchHL                  lipgloss.Style // search match
	Key, KeyDesc             lipgloss.Style // footer/help key hints: "enter" "open"
	ModePill                 lipgloss.Style // footer mode label, e.g. " NORMAL "
	Header                   lipgloss.Style // column headers in tables
	JSONKey, JSONString      lipgloss.Style
	JSONNumber, JSONBool     lipgloss.Style
	JSONNull, JSONHelper     lipgloss.Style
	JSONPunct, JSONError     lipgloss.Style
	Overlay                  lipgloss.Style // help/palette/confirm boxes
	PromptPrefix, Suggestion lipgloss.Style
}

func fg(c color.Color) lipgloss.Style {
	s := lipgloss.NewStyle()
	if c != nil {
		s = s.Foreground(c)
	}
	return s
}

// New derives styles from a palette.
func New(p Palette) *Theme {
	mono := p.Accent == nil
	t := &Theme{P: p, Mono: mono}
	t.Text = fg(p.Text)
	t.Muted = fg(p.Muted)
	t.Faint = fg(p.Faint)
	t.Accent = fg(p.Accent)
	t.AccentBold = fg(p.Accent).Bold(true)
	t.OK, t.Warn, t.Err, t.Info = fg(p.OK), fg(p.Warn), fg(p.Err), fg(p.Info)
	t.Title = fg(p.Muted)
	t.TitleFocused = fg(p.Accent).Bold(true)
	t.Pane = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	t.PaneFocused = t.Pane
	if p.Border != nil {
		t.Pane = t.Pane.BorderForeground(p.Border)
	}
	if p.Accent != nil {
		t.PaneFocused = t.PaneFocused.BorderForeground(p.Accent)
	}
	t.CursorRow = lipgloss.NewStyle()
	if p.Cursor != nil {
		t.CursorRow = t.CursorRow.Background(p.Cursor)
	} else {
		t.CursorRow = t.CursorRow.Reverse(true)
	}
	t.SelectMark = fg(p.Select).Bold(true)
	t.MatchHL = lipgloss.NewStyle().Reverse(true)
	if p.Match != nil {
		t.MatchHL = lipgloss.NewStyle().Background(p.Match).Foreground(p.OnMatch)
	}
	t.Key = fg(p.Accent).Bold(true)
	t.KeyDesc = fg(p.Muted)
	t.ModePill = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	if p.Accent != nil {
		t.ModePill = t.ModePill.Background(p.Accent).Foreground(p.OnAcc)
	} else {
		t.ModePill = t.ModePill.Reverse(true)
	}
	t.Header = fg(p.Muted).Bold(true)
	t.JSONKey = fg(p.Key)
	t.JSONString = fg(p.String)
	t.JSONNumber = fg(p.Number)
	t.JSONBool = fg(p.Bool)
	t.JSONNull = fg(p.Null).Italic(true)
	t.JSONHelper = fg(p.Helper)
	t.JSONPunct = fg(p.Punct)
	t.JSONError = fg(p.Err)
	t.Overlay = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if p.Accent != nil {
		t.Overlay = t.Overlay.BorderForeground(p.Accent)
	}
	t.PromptPrefix = fg(p.Accent).Bold(true)
	t.Suggestion = fg(p.Faint)
	if mono {
		t.Muted = t.Muted.Faint(true)
		t.Faint = t.Faint.Faint(true)
		t.Title = t.Title.Faint(true)
		t.TitleFocused = lipgloss.NewStyle().Bold(true)
		t.Key = lipgloss.NewStyle().Bold(true)
		t.KeyDesc = t.KeyDesc.Faint(true)
		t.Header = lipgloss.NewStyle().Bold(true).Underline(true)
		t.SelectMark = lipgloss.NewStyle().Bold(true)
		t.Suggestion = t.Suggestion.Faint(true)
	}
	return t
}

var current = New(Dark())

// Current returns the active theme. Set it once at startup, before the UI runs.
func Current() *Theme { return current }

// Set replaces the active theme.
func Set(t *Theme) { current = t }

// Skin is the user-facing theme config (the `theme:` block of ~/.tango.yaml).
type Skin struct {
	// Name is "dark" (default), "light", "mono", or "auto" (pick dark/light
	// from the detected terminal background).
	Name string `yaml:"name"`
	// Colors overrides individual palette tokens by lowercase name, e.g.
	// {accent: "#89b4fa", border: "238"}. Values are hex or ANSI numbers.
	Colors map[string]string `yaml:"colors"`
}

// Load builds a theme from a skin. darkBG is the detected terminal
// background (used when Name is "auto"). NO_COLOR forces mono.
func Load(sk Skin, darkBG bool) (*Theme, error) {
	if os.Getenv("NO_COLOR") != "" {
		return New(Mono()), nil
	}
	var p Palette
	switch strings.ToLower(sk.Name) {
	case "auto":
		if darkBG {
			p = Dark()
		} else {
			p = Light()
		}
	case "", "dark":
		p = Dark()
	case "light":
		p = Light()
	case "mono":
		p = Mono()
	default:
		return nil, fmt.Errorf("unknown theme %q (want dark, light, mono, or auto)", sk.Name)
	}
	for k, v := range sk.Colors {
		dst := p.token(strings.ToLower(k))
		if dst == nil {
			return nil, fmt.Errorf("unknown theme color %q", k)
		}
		*dst = lipgloss.Color(v)
	}
	return New(p), nil
}

func (p *Palette) token(name string) *color.Color {
	m := map[string]*color.Color{
		"text": &p.Text, "muted": &p.Muted, "faint": &p.Faint, "border": &p.Border,
		"accent": &p.Accent, "onaccent": &p.OnAcc, "cursor": &p.Cursor, "select": &p.Select,
		"match": &p.Match, "onmatch": &p.OnMatch, "ok": &p.OK, "warn": &p.Warn,
		"err": &p.Err, "info": &p.Info, "key": &p.Key, "string": &p.String,
		"number": &p.Number, "bool": &p.Bool, "null": &p.Null, "helper": &p.Helper,
		"punct": &p.Punct,
	}
	return m[name]
}
