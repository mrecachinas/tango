package doctable

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/mjson"
	"tango/internal/theme"
)

type cell struct {
	text    string
	kind    mjson.Kind
	numeric bool
	missing bool
}

func dateValue(value any) (time.Time, bool) {
	switch v := value.(type) {
	case primitive.DateTime:
		return v.Time().UTC(), true
	case time.Time:
		return v.UTC(), true
	default:
		return time.Time{}, false
	}
}

func dateOnly(rows []map[string]any, column string) bool {
	found := false
	for _, row := range rows {
		value, present := row[column]
		if !present || value == nil {
			continue
		}
		if _, null := value.(primitive.Null); null {
			continue
		}
		d, ok := dateValue(value)
		if !ok || d.Hour() != 0 || d.Minute() != 0 || d.Second() != 0 || d.Nanosecond() != 0 {
			return false
		}
		found = true
	}
	return found
}

func formatCell(value any, present, midnight bool) cell {
	if !present {
		return cell{text: "—", missing: true}
	}
	if d, ok := dateValue(value); ok {
		layout := "2006-01-02 15:04"
		if midnight {
			layout = "2006-01-02"
		}
		return cell{text: d.Format(layout), kind: mjson.Helper}
	}
	if d, ok := asDocument(value); ok {
		return cell{text: fmt.Sprintf("{…%d}", len(d)), kind: mjson.Punct}
	}
	if n, ok := arrayLength(value); ok {
		return cell{text: fmt.Sprintf("[%d]", n), kind: mjson.Punct}
	}
	c := cell{kind: mjson.Text}
	switch v := value.(type) {
	case primitive.ObjectID:
		c.text, c.kind = "…"+v.Hex()[17:], mjson.Helper
	case string:
		c.text = escapeControls(v)
	case bool:
		c.text, c.kind = strconv.FormatBool(v), mjson.Bool
	case nil, primitive.Null:
		c.text, c.kind = "null", mjson.Null
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		c.text, c.kind, c.numeric = fmt.Sprint(v), mjson.Number, true
	case float32, float64:
		c.text, c.kind, c.numeric = mjson.Compact(v), mjson.Number, true
	case primitive.Decimal128:
		c.text, c.kind, c.numeric = v.String(), mjson.Number, true
	default:
		tokens := mjson.Tokens(value, false)
		c.text = escapeControls(mjson.Join(tokens))
		if len(tokens) > 0 {
			c.kind = tokens[0].Kind
		}
	}
	return c
}

func escapeControls(s string) string {
	if !strings.ContainsFunc(s, unicode.IsControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if unicode.IsControl(r) {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func tokenStyle(t *theme.Theme, kind mjson.Kind) lipgloss.Style {
	switch kind {
	case mjson.Key:
		return t.JSONKey
	case mjson.String:
		return t.JSONString
	case mjson.Number:
		return t.JSONNumber
	case mjson.Bool:
		return t.JSONBool
	case mjson.Null:
		return t.JSONNull
	case mjson.Helper:
		return t.JSONHelper
	case mjson.Punct:
		return t.JSONPunct
	case mjson.Error:
		return t.JSONError
	default:
		return t.Text
	}
}

func rowStyle(style lipgloss.Style, cursor bool, t *theme.Theme) lipgloss.Style {
	if cursor {
		style = style.Inherit(t.CursorRow)
	}
	return style
}

func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	// A wide grapheme can leave one unused cell after truncation.
	return theme.FitWidth(ansi.Truncate(s, width, "…"), width)
}

func renderCell(c cell, width int, cursor bool, t *theme.Theme) string {
	s := fit(c.text, width)
	if c.numeric {
		text := strings.TrimRight(s, " ")
		s = strings.Repeat(" ", max(0, width-ansi.StringWidth(text))) + text
	}
	style := tokenStyle(t, c.kind)
	if c.missing {
		style = t.Faint
	}
	return rowStyle(style, cursor, t).Render(s)
}
