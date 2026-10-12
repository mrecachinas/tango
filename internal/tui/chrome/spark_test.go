package chrome

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"tango/internal/theme"
)

func TestSparklineEmptySingleFlatAndZero(t *testing.T) {
	for _, data := range [][]float64{nil, {42}, {10, 10, 10}, {0, 0, 0}} {
		got := Sparkline(data, 8)
		want := theme.Current().Faint.Render("▁▁▁▁▁▁▁▁")
		if got != want {
			t.Errorf("Sparkline(%v) = %q, want faint baseline %q", data, got, want)
		}
	}
}

func TestSparklineScaling(t *testing.T) {
	for _, tc := range []struct {
		data  []float64
		width int
		want  string
	}{
		{[]float64{0, 1, 2, 3, 4, 5, 6, 7}, 8, "▁▂▃▄▅▆▇█"},
		{[]float64{10000, 0, 1, 2, 3, 4, 5, 6, 7}, 8, "▁▂▃▄▅▆▇█"},
		{[]float64{-10, -5, 0, 5, 10}, 8, "▁▁▁▁▂▄▆█"},
		{[]float64{0, 7}, 8, "▁▁▁▁▁▁▁█"},
		{[]float64{math.NaN(), math.Inf(1), math.Inf(-1), 1}, 4, "▁▁▁█"},
		{[]float64{-math.MaxFloat64, 0, math.MaxFloat64}, 3, "▁▄█"},
	} {
		got := ansi.Strip(Sparkline(tc.data, tc.width))
		if got != tc.want {
			t.Errorf("Sparkline(%v, %d) = %q, want %q", tc.data, tc.width, got, tc.want)
		}
	}
}

func TestSparklineWidthAndNoMutation(t *testing.T) {
	for _, width := range []int{0, 1, 8, 20, 40} {
		for _, n := range []int{0, 1, 5, 20, 25, 100} {
			data := make([]float64, n)
			for i := range data {
				data[i] = float64(i)
			}
			got := Sparkline(data, width)
			if ansi.StringWidth(got) != width {
				t.Errorf("width %d data len %d: got %q", width, n, got)
			}
			for i := range data {
				if data[i] != float64(i) {
					t.Fatal("Sparkline mutated history")
				}
			}
		}
	}
	if Sparkline(nil, -1) != "" {
		t.Error("negative width should be empty")
	}
}

func TestGaugeClampingAndFill(t *testing.T) {
	for _, tc := range []struct {
		pct    float64
		filled int
	}{
		{0, 0}, {50, 7}, {100, 15}, {150, 15}, {-20, 0}, {math.NaN(), 0}, {math.Inf(1), 15},
	} {
		got := ansi.Strip(Gauge(tc.pct, 15))
		if strings.Count(got, "█") != tc.filled || strings.Count(got, "░") != 15-tc.filled {
			t.Errorf("Gauge(%v) = %q", tc.pct, got)
		}
	}
	for _, width := range []int{-1, 0, 1, 8, 20, 40} {
		got := Gauge(42, width)
		if ansi.StringWidth(got) != max(0, width) {
			t.Errorf("Gauge width %d = %q", width, got)
		}
	}
}

func TestGaugeThresholdStyles(t *testing.T) {
	th := theme.Current()
	for _, tc := range []struct {
		pct  float64
		want string
	}{
		{69, th.OK.Render(strings.Repeat("█", 69)) + th.Faint.Render(strings.Repeat("░", 31))},
		{70, th.Warn.Render(strings.Repeat("█", 70)) + th.Faint.Render(strings.Repeat("░", 30))},
		{89, th.Warn.Render(strings.Repeat("█", 89)) + th.Faint.Render(strings.Repeat("░", 11))},
		{90, th.Err.Render(strings.Repeat("█", 90)) + th.Faint.Render(strings.Repeat("░", 10))},
		{100, th.Err.Render(strings.Repeat("█", 100))},
	} {
		if got := Gauge(tc.pct, 100); got != tc.want {
			t.Errorf("gauge at %v%% uses incorrect threshold style", tc.pct)
		}
	}
}
