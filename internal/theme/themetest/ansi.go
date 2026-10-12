// Package themetest checks terminal-cell styling in component tests.
package themetest

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
)

var sgr = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

// Cursor verifies that every printable cell, including spaces, has the
// theme's cursor background (or reverse video in monochrome).
func Cursor(t *testing.T, row string, width int) {
	t.Helper()
	if ansi.StringWidth(row) != width {
		t.Fatalf("cursor width %d, want %d: %q", ansi.StringWidth(row), width, row)
	}
	fill := theme.Current().CursorRow.Render(" ")
	wantBG, wantReverse := "", false
	apply := func(seq string, bg *string, reverse *bool) {
		codes := strings.Split(seq, ";")
		for i := 0; i < len(codes); i++ {
			switch codes[i] {
			case "", "0":
				*bg, *reverse = "", false
			case "7":
				*reverse = true
			case "27":
				*reverse = false
			case "49":
				*bg = ""
			case "38", "48":
				n := 0
				if i+1 < len(codes) {
					switch codes[i+1] {
					case "2":
						n = 4
					case "5":
						n = 2
					}
				}
				if n > 0 && i+n < len(codes) {
					if codes[i] == "48" {
						*bg = strings.Join(codes[i:i+n+1], ";")
					}
					i += n
				}
			default:
				code, _ := strconv.Atoi(codes[i])
				if code >= 40 && code <= 47 {
					*bg = codes[i]
				}
			}
		}
	}
	for _, seq := range sgr.FindAllStringSubmatch(fill, -1) {
		apply(seq[1], &wantBG, &wantReverse)
		if wantBG != "" || wantReverse {
			break
		}
	}
	bg, reverse, pos := "", false, 0
	check := func(text string) {
		if ansi.StringWidth(text) > 0 && (bg != wantBG || reverse != wantReverse) {
			t.Fatalf("cursor has unfilled cells at %q: bg=%q reverse=%v, want bg=%q reverse=%v; row=%q",
				text, bg, reverse, wantBG, wantReverse, row)
		}
	}
	for _, loc := range sgr.FindAllStringSubmatchIndex(row, -1) {
		check(row[pos:loc[0]])
		apply(row[loc[2]:loc[3]], &bg, &reverse)
		pos = loc[1]
	}
	check(row[pos:])
}
