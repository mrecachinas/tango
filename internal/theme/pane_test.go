package theme_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
	"tango/internal/theme/themetest"
)

func TestCursorLineKeepsSyntaxAndFillsResets(t *testing.T) {
	old := theme.Current()
	t.Cleanup(func() { theme.Set(old) })
	for _, palette := range []theme.Palette{theme.Dark(), theme.Light(), theme.Mono()} {
		theme.Set(theme.New(palette))
		th := theme.Current()
		text := th.JSONKey.Render("_id") + "  " + th.JSONHelper.Render("ObjectId(…)") +
			"  " + th.JSONNumber.Render("12") + th.JSONNull.Render("null") + " 界"
		for _, width := range []int{0, 1, 2, 8, 24, 50} {
			got := theme.CursorLine(text, width)
			themetest.Cursor(t, got, width)
			if ansi.Strip(got) != ansi.Strip(theme.FitWidth(text, width)) {
				t.Fatal("cursor styling changed content")
			}
		}
		if got := theme.CursorLine(text, 50); !strings.Contains(got, th.JSONHelper.Render("ObjectId(…)")[:strings.IndexByte(th.JSONHelper.Render("ObjectId(…)"), 'O')]) {
			t.Fatal("semantic helper foreground was lost")
		}
	}
}

func TestFitWidthWideTruncation(t *testing.T) {
	for _, text := range []string{"界界界", "👩‍💻👩‍💻", "é界", theme.Current().Muted.Render("界界")} {
		for width := range 10 {
			if got := ansi.StringWidth(theme.FitWidth(text, width)); got != width {
				t.Fatalf("width=%d want=%d", got, width)
			}
		}
	}
}
