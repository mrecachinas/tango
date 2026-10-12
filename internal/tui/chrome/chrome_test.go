package chrome

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tango/internal/metrics"
	"tango/internal/theme"
)

func snapshot() metrics.Snapshot {
	return metrics.Snapshot{
		HostLabel: "localhost:27017", Version: "7.0.31", Uptime: 23*time.Hour + 59*time.Minute,
		Connected: true, OpsPerSec: 2, ConnectionsCurrent: 3, ConnectionsAvailable: 100285,
		PingMS: .4, MemoryMB: 200, NetInKBPerSec: 2, NetOutKBPerSec: 4,
		Queue: 0, Cursors: 2, CachePercent: 60, TicketsPercent: 10,
		History: metrics.Histories{
			Ops:         []float64{0, 1, 2, 3, 4, 5, 6, 7},
			Connections: []float64{3, 3}, Ping: []float64{.4, .4}, Memory: []float64{200, 200},
			NetIn: []float64{0, 2}, Queue: []float64{0, 0}, Cursors: []float64{0, 2},
		},
	}
}

func assertSize(t *testing.T, s string, width, height int) {
	t.Helper()
	lines := strings.Split(s, "\n")
	if len(lines) != height {
		t.Fatalf("height = %d, want %d: %q", len(lines), height, ansi.Strip(s))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != width {
			t.Fatalf("line %d width = %d, want %d: %q", i, got, width, ansi.Strip(line))
		}
	}
}

func TestHeaderAndFooterExactSizes(t *testing.T) {
	snaps := []metrics.Snapshot{snapshot(), {}, {
		HostLabel: "🌱 数据库.example.com:27017", Version: "v[not markup]", Connected: true,
		ReadOnly: true, ReplicaSet: &metrics.ReplicaSetInfo{Healthy: 2, Members: 3},
	}, {
		HostLabel: "host\nwith\tcontrol\rcharacters", Err: errors.New("selection failed\n(error [x])"),
		ReadOnly: true,
	}}
	footers := []FooterState{
		{}, {Mode: "FILTER"}, {Mode: "QUERY", Loading: "loading…", Spinner: "⠙"},
		{Mode: "COMMAND", Flash: "Saved ✓", FlashKind: OK},
		{Mode: "SEARCH", Flash: "Failed\nretry\rquery", FlashKind: Err, Loading: "ignored"},
		{Mode: "CONFIRM", Override: theme.Current().Warn.Render("Delete 3 documents? y/N")},
		{Mode: "READ-ONLY", Hints: []Hint{}}, {Mode: "LIVE"},
		{Mode: "A very long mode\nname", Flash: strings.Repeat("数据库 ", 50)},
		{Hints: []Hint{{Key: "🔍", Desc: "搜索\n文档"}}},
	}
	for width := 20; width <= 240; width++ {
		for _, snap := range snaps {
			for _, expanded := range []bool{false, true} {
				assertSize(t, Header(snap, []Badge{{Text: "◉ LIVE", Kind: Err}}, width, expanded), width, HeaderHeight(expanded))
			}
		}
		for _, f := range footers {
			assertSize(t, Footer(f, width), width, 1)
		}
	}
}

func TestHeaderCompactContent(t *testing.T) {
	want := "● localhost:27017 · v7.0.31 · up 23h59m   ops 2/s ▁▂▃▄▅▆▇█  conns 3  ping 0.4ms  mem 200MB"
	got := strings.TrimRight(ansi.Strip(Header(snapshot(), nil, 160, false)), " ")
	if got != want {
		t.Fatalf("compact header:\n got %q\nwant %q", got, want)
	}
}

func TestHeaderDegradationOrder(t *testing.T) {
	full := strings.TrimRight(ansi.Strip(Header(snapshot(), nil, 240, false)), " ")
	metricsInOrder := []string{"mem ", "conns ", "ping ", "▁▂▃▄▅▆▇█", "ops 2/s", "up "}
	seenDropped := make([]bool, len(metricsInOrder))
	for width := ansi.StringWidth(full); width >= 20; width-- {
		got := ansi.Strip(Header(snapshot(), nil, width, false))
		for i, label := range metricsInOrder {
			present := strings.Contains(got, label)
			if !present {
				seenDropped[i] = true
				for j := 0; j < i; j++ {
					if strings.Contains(got, metricsInOrder[j]) {
						t.Fatalf("width %d: dropped %s before lower-priority %s: %q", width, label, metricsInOrder[j], got)
					}
				}
			} else if seenDropped[i] {
				t.Fatalf("width %d: %s returned after being dropped", width, label)
			}
		}
		if width >= ansi.StringWidth("● localhost:27017 · v7.0.31") && !strings.Contains(got, "localhost:27017 · v7.0.31") {
			t.Fatalf("width %d: identity truncated before optional metrics: %q", width, got)
		}
	}
	for i, dropped := range seenDropped {
		if !dropped {
			t.Errorf("%s was never dropped", metricsInOrder[i])
		}
	}
}

func TestHeaderBadgesRightAlignedAndDeduplicated(t *testing.T) {
	s := snapshot()
	s.ReadOnly = true
	s.ReplicaSet = &metrics.ReplicaSetInfo{Healthy: 3, Members: 3}
	badges := []Badge{{Text: "READ-ONLY", Kind: Warn}, {Text: "◉ LIVE", Kind: Err}}
	for _, width := range []int{60, 100, 160, 240} {
		got := ansi.Strip(Header(s, badges, width, false))
		if !strings.HasSuffix(got, "READ-ONLY  ◉ LIVE  rs 3/3") {
			t.Errorf("width %d: badges not right-aligned: %q", width, got)
		}
		if strings.Count(got, "READ-ONLY") != 1 {
			t.Errorf("width %d: duplicate badge: %q", width, got)
		}
	}
	narrow := ansi.Strip(Header(s, badges, 20, false))
	if !strings.HasSuffix(narrow, "READ-ONLY") {
		t.Errorf("safety badge lost at width 20: %q", narrow)
	}
	styled := Header(s, nil, 160, false)
	if !strings.Contains(styled, theme.Current().Warn.Render("READ-ONLY")) ||
		!strings.Contains(styled, theme.Current().OK.Render("rs 3/3")) {
		t.Error("badge semantic styles missing")
	}
	s.ReplicaSet.Healthy = 2
	if !strings.Contains(Header(s, nil, 160, false), theme.Current().Err.Render("rs 2/3")) {
		t.Error("unhealthy replica badge is not an error")
	}
}

func TestHeaderDisconnected(t *testing.T) {
	s := snapshot()
	s.Connected = false
	s.Err = errors.New("timeout [x]\nretry")
	got := Header(s, nil, 160, false)
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "● localhost:27017 · disconnected (timeout [x] retry)") ||
		strings.Contains(plain, "ops ") || !strings.Contains(got, theme.Current().Err.Render("●")) {
		t.Errorf("disconnected header = %q", plain)
	}
	narrow := ansi.Strip(Header(s, nil, 20, false))
	if !strings.Contains(narrow, "disconnected") {
		t.Errorf("disconnect status lost at width 20 = %q", narrow)
	}
}

func TestExpandedGrid(t *testing.T) {
	got := Header(snapshot(), nil, 120, true)
	lines := strings.Split(got, "\n")
	for row, labels := range [][]string{{"ops ", "conns ", "ping "}, {"mem ", "net ", "queue "}, {"cursors ", "cache ", "tickets "}} {
		for _, label := range labels {
			if !strings.Contains(ansi.Strip(lines[row+1]), label) {
				t.Errorf("row %d missing %q: %q", row+1, label, ansi.Strip(lines[row+1]))
			}
		}
	}
	if !strings.Contains(ansi.Strip(lines[2]), "↓2 KB/s ↑4 KB/s") {
		t.Error("network in/out not visible")
	}

	if !strings.Contains(lines[2], theme.Current().Faint.Render("▁▁▁▁▁▁▁▁")) {
		t.Error("flat zero history is not a faint baseline")
	}
	assertSize(t, got, 120, 4)
}

func TestNarrowHeaderPreservesHostAndVersion(t *testing.T) {
	s := snapshot()
	s.HostLabel = "127.0.0.1:27027"
	s.ReplicaSet = &metrics.ReplicaSetInfo{Healthy: 1, Members: 1}
	got := ansi.Strip(Header(s, nil, 40, false))
	if !strings.Contains(got, "127.0.0.1:27027 · v7.0.31") || strings.Contains(got, "ops ") {
		t.Fatalf("narrow identity: %q", got)
	}
}

func TestFooterHintsAndDropping(t *testing.T) {
	want := " NORMAL  enter open · space select · / query · : cmd · ? help"
	got := strings.TrimRight(ansi.Strip(Footer(FooterState{}, 100)), " ")
	if got != want {
		t.Fatalf("footer:\n got %q\nwant %q", got, want)
	}
	for width := ansi.StringWidth(want); width >= 20; width-- {
		got := ansi.Strip(Footer(FooterState{}, width))
		missing := false
		for _, hint := range DefaultHints() {
			present := strings.Contains(got, hint.Key+" "+hint.Desc)
			if present && missing {
				t.Errorf("width %d: low-priority hint retained after a missing hint: %q", width, got)
			}
			missing = missing || !present
		}
		if strings.HasSuffix(strings.TrimSpace(got), "·") {
			t.Errorf("width %d: dangling separator: %q", width, got)
		}
	}
	empty := ansi.Strip(Footer(FooterState{Hints: []Hint{}}, 80))
	if strings.Contains(empty, "enter") {
		t.Error("explicit empty hints did not suppress defaults")
	}
}

func TestFooterFlashLoadingAndOverride(t *testing.T) {
	f := FooterState{Loading: "loading…", Spinner: "⠙", Flash: "Saved", FlashKind: OK}
	got := Footer(f, 60)
	if !strings.HasSuffix(ansi.Strip(got), "✓ Saved") || strings.Contains(ansi.Strip(got), "loading") ||
		!strings.Contains(got, theme.Current().OK.Render("✓ Saved")) {
		t.Errorf("flash precedence or alignment = %q", ansi.Strip(got))
	}
	f.Flash, f.FlashKind = "Failed", Err
	if got := Footer(f, 60); !strings.Contains(got, theme.Current().Err.Render("✗ Failed")) {
		t.Error("error flash style/glyph missing")
	}
	for _, kind := range []string{Info, Warn} {
		f.FlashKind = kind
		if got := Footer(f, 60); !strings.Contains(got, kindStyle(kind).Render("✓ Failed")) {
			t.Errorf("%s flash style missing", kind)
		}
	}
	f.Flash = ""
	if got := ansi.Strip(Footer(f, 60)); !strings.HasSuffix(got, "⠙ loading…") {
		t.Errorf("loading alignment = %q", got)
	}
	f.Mode, f.Override = "CONFIRM", theme.Current().Warn.Render("Delete? y/N")
	got = Footer(f, 60)
	if !strings.HasPrefix(ansi.Strip(got), " CONFIRM  Delete? y/N") ||
		strings.Contains(ansi.Strip(got), "loading") || strings.Contains(ansi.Strip(got), "enter") ||
		!strings.Contains(got, f.Override) {
		t.Errorf("override = %q", ansi.Strip(got))
	}
}

func TestFooterStatusWinsOverHints(t *testing.T) {
	f := FooterState{Flash: "An important operation failed", FlashKind: Err}
	got := ansi.Strip(Footer(f, 40))
	if !strings.Contains(got, "✗ An important operation failed") || strings.Contains(got, "enter") {
		t.Errorf("flash should displace hints: %q", got)
	}
	f.Flash = strings.Repeat("long status ", 40)
	got = ansi.Strip(Footer(f, 20))
	if !strings.Contains(got, "✗") || !strings.HasSuffix(got, "…") || strings.Contains(got, "enter") {
		t.Errorf("long status must win and truncate: %q", got)
	}
}

func TestSpinner(t *testing.T) {
	s := NewSpinner()
	if len(s.Spinner.Frames) != 10 || s.Spinner.Frames[0] != "⠋" ||
		ansi.StringWidth(s.View()) != 1 || s.Spinner.FPS <= 0 {
		t.Errorf("spinner = %+v, view %q", s.Spinner, s.View())
	}
}

func TestMonoAndNoColor(t *testing.T) {
	old := theme.Current()
	t.Cleanup(func() { theme.Set(old) })
	t.Setenv("NO_COLOR", "1")
	theme.Set(theme.New(theme.Mono()))
	s := snapshot()
	s.ReadOnly = true
	s.ReplicaSet = &metrics.ReplicaSetInfo{Healthy: 2, Members: 3}
	for _, width := range []int{20, 40, 80, 120, 240} {
		values := []string{
			Header(s, []Badge{{Text: "◉ LIVE", Kind: Err}}, width, false),
			Header(s, nil, width, true),
			Footer(FooterState{Mode: "LIVE", Flash: "Problem", FlashKind: Err}, width),
			Footer(FooterState{Mode: "READ-ONLY", Loading: "loading"}, width),
		}
		for _, value := range values {
			if strings.Contains(value, "38;") || strings.Contains(value, "48;") {
				t.Errorf("mono emitted colour: %q", value)
			}
			height := 1
			if strings.Contains(value, "\n") {
				height = 4
			}
			assertSize(t, value, width, height)
		}
	}
}

func TestTinyWidths(t *testing.T) {
	for width := 0; width < 20; width++ {
		assertSize(t, Header(snapshot(), nil, width, false), width, 1)
		assertSize(t, Header(snapshot(), nil, width, true), width, 4)
		assertSize(t, Footer(FooterState{Flash: "Error", FlashKind: Err}, width), width, 1)
	}
}

func TestDuration(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{{0, "0m"}, {-time.Hour, "0m"}, {5 * time.Minute, "5m"}, {150 * time.Minute, "2h30m"},
		{77*time.Hour + 12*time.Minute, "3d5h12m"}} {
		t.Run(fmt.Sprint(tc.d), func(t *testing.T) {
			if got := duration(tc.d); got != tc.want {
				t.Errorf("duration = %q, want %q", got, tc.want)
			}
		})
	}
}
