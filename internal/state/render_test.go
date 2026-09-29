package state

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/mjson"
)

// drawn renders markup in a TextView on a simulation screen and returns the
// visible text with trailing spaces trimmed per line.
func drawn(t *testing.T, markup string, regions bool) string {
	t.Helper()
	scr := tcell.NewSimulationScreen("UTF-8")
	if err := scr.Init(); err != nil {
		t.Fatal(err)
	}
	defer scr.Fini()
	scr.SetSize(200, 40)
	tv := tview.NewTextView().SetDynamicColors(true).SetRegions(regions).SetWrap(false)
	tv.SetText(markup)
	tv.SetRect(0, 0, 200, 40)
	tv.Draw(scr)
	scr.Show()
	cells, w, h := scr.GetContents()
	var lines []string
	for y := 0; y < h; y++ {
		var b strings.Builder
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) > 0 {
				b.WriteRune(c.Runes[0])
			} else {
				b.WriteByte(' ')
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

var nastyDoc = bson.D{
	{Key: "label", Value: "[red]hi[-] [::b]bold [\"region\"] [yellow[]"},
	{Key: "tags", Value: bson.A{"x", "[y]", bson.A{}}},
	{Key: "[k]", Value: bson.D{{Key: "a", Value: int32(1)}}},
	{Key: "brackets", Value: "[[[]]] [] [-:-] [#ff0000]"},
}

func TestRenderTokensDrawsExactText(t *testing.T) {
	for _, pretty := range []bool{true, false} {
		toks := mjson.Tokens(nastyDoc, pretty)
		plain := mjson.Join(toks)
		for _, term := range []string{"", "y", "[", "hi[-]", "\"x\""} {
			markup, _ := RenderTokens(toks, term)
			if got := drawn(t, markup, true); got != plain {
				t.Errorf("pretty=%v term=%q\n got: %q\nwant: %q", pretty, term, got, plain)
			}
		}
	}
}

func TestRenderTokensMatchCount(t *testing.T) {
	toks := mjson.Tokens(bson.D{{Key: "Name", Value: "name NAME"}}, false)
	markup, n := RenderTokens(toks, "name")
	if n != 3 {
		t.Errorf("matches = %d, want 3", n)
	}
	if !strings.Contains(markup, `["m2"]`) {
		t.Errorf("expected region for third match: %s", markup)
	}
}

func TestListLabelEscapes(t *testing.T) {
	got := drawn(t, ListLabel(ListEntry{Name: "we[ir]d", Info: "3"}), false)
	if got != "we[ir]d (3)" {
		t.Errorf("got %q", got)
	}
}
