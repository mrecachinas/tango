package state

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/rivo/tview"

	"tango/internal/mjson"
)

var kindColors = map[mjson.Kind]string{
	mjson.Punct:  "white",
	mjson.Key:    "aqua",
	mjson.String: "green",
	mjson.Number: "yellow",
	mjson.Bool:   "fuchsia",
	mjson.Null:   "red",
	mjson.Helper: "orange",
	mjson.Text:   "white",
	mjson.Error:  "red",
}

// RenderTokens converts tokens to tview markup. All token text is escaped,
// so document content can never be interpreted as color or region tags.
// Case-insensitive matches of term are highlighted and wrapped in regions
// "m0", "m1", …; the number of matches is returned.
func RenderTokens(toks []mjson.Token, term string) (string, int) {
	ids, n := matchIDs(toks, term)
	var b strings.Builder
	pos, cur := 0, -1
	for _, t := range toks {
		rs := []rune(t.Text)
		color := kindColors[t.Kind]
		for start := 0; start < len(rs); {
			id, end := -1, len(rs)
			if ids != nil {
				id = ids[pos+start]
				end = start + 1
				for end < len(rs) && ids[pos+end] == id {
					end++
				}
			}
			if id != cur {
				if cur >= 0 {
					b.WriteString(`[""]`)
				}
				if id >= 0 {
					b.WriteString(`["m` + strconv.Itoa(id) + `"]`)
				}
				cur = id
			}
			if id >= 0 {
				b.WriteString("[black:yellow]")
			} else {
				b.WriteString("[" + color + ":-]")
			}
			b.WriteString(tview.Escape(string(rs[start:end])))
			start = end
		}
		pos += len(rs)
	}
	if cur >= 0 {
		b.WriteString(`[""]`)
	}
	b.WriteString("[-:-]")
	return b.String(), n
}

// matchIDs returns, per rune of the joined token text, the index of the
// match covering it (or -1), and the number of matches.
func matchIDs(toks []mjson.Token, term string) ([]int, int) {
	if term == "" {
		return nil, 0
	}
	hay := []rune(mjson.Join(toks))
	needle := []rune(term)
	for i, r := range hay {
		hay[i] = unicode.ToLower(r)
	}
	for i, r := range needle {
		needle[i] = unicode.ToLower(r)
	}
	ids := make([]int, len(hay))
	for i := range ids {
		ids[i] = -1
	}
	n := 0
	for i := 0; i+len(needle) <= len(hay); {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if !match {
			i++
			continue
		}
		for j := range needle {
			ids[i+j] = n
		}
		n++
		i += len(needle)
	}
	return ids, n
}
