package metrics

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"tango/internal/state"
)

// --- renderSparkline ---

func TestRenderSparkline_Empty(t *testing.T) {
	result := renderSparkline(nil)
	if utf8.RuneCountInString(result) != SparkWidth {
		t.Fatalf("expected width %d, got %d", SparkWidth, utf8.RuneCountInString(result))
	}
	expected := strings.Repeat(string(sparkChars[0]), SparkWidth)
	if result != expected {
		t.Fatalf("expected all lowest blocks, got %q", result)
	}
}

func TestRenderSparkline_SingleValue(t *testing.T) {
	result := renderSparkline([]float64{42})
	runes := []rune(result)
	if len(runes) != SparkWidth {
		t.Fatalf("expected width %d, got %d", SparkWidth, len(runes))
	}
	// Single value has no range, so all visible chars should be sparkChars[0].
	for _, r := range runes {
		if r != sparkChars[0] {
			t.Fatalf("expected all same block char %c, got %c", sparkChars[0], r)
		}
	}
}

func TestRenderSparkline_Ascending(t *testing.T) {
	data := make([]float64, SparkWidth)
	for i := range data {
		data[i] = float64(i)
	}
	result := renderSparkline(data)
	runes := []rune(result)
	if len(runes) != SparkWidth {
		t.Fatalf("expected width %d, got %d", SparkWidth, len(runes))
	}
	// First rune should be lowest, last should be highest.
	if runes[0] != sparkChars[0] {
		t.Errorf("first rune should be lowest block %c, got %c", sparkChars[0], runes[0])
	}
	if runes[len(runes)-1] != sparkChars[len(sparkChars)-1] {
		t.Errorf("last rune should be highest block %c, got %c", sparkChars[len(sparkChars)-1], runes[len(runes)-1])
	}
	// Should be non-decreasing.
	for i := 1; i < len(runes); i++ {
		if runes[i] < runes[i-1] {
			t.Errorf("runes should be non-decreasing at index %d", i)
		}
	}
}

func TestRenderSparkline_AllSame(t *testing.T) {
	data := make([]float64, 5)
	for i := range data {
		data[i] = 10
	}
	result := renderSparkline(data)
	// All visible values identical → range is 0 → all sparkChars[0].
	for _, r := range result {
		if r != sparkChars[0] {
			t.Fatalf("expected all same block char, got %c", r)
		}
	}
}

func TestRenderSparkline_MoreDataThanWidth(t *testing.T) {
	data := make([]float64, SparkWidth+10)
	for i := range data {
		data[i] = float64(i)
	}
	result := renderSparkline(data)
	runes := []rune(result)
	if len(runes) != SparkWidth {
		t.Fatalf("expected width %d, got %d", SparkWidth, len(runes))
	}
	// Last rune should still be highest block (from the tail of data).
	if runes[len(runes)-1] != sparkChars[len(sparkChars)-1] {
		t.Errorf("last rune should be highest block")
	}
}

func TestRenderSparkline_NegativeValues(t *testing.T) {
	data := []float64{-10, -5, 0, 5, 10}
	result := renderSparkline(data)
	runes := []rune(result)
	if len(runes) != SparkWidth {
		t.Fatalf("expected width %d, got %d", SparkWidth, len(runes))
	}
}

func TestRenderSparkline_LengthAlwaysSparkWidth(t *testing.T) {
	for _, n := range []int{0, 1, 5, SparkWidth, SparkWidth + 5, 100} {
		data := make([]float64, n)
		for i := range data {
			data[i] = float64(i)
		}
		result := renderSparkline(data)
		runes := []rune(result)
		if len(runes) != SparkWidth {
			t.Errorf("data len %d: expected rune width %d, got %d", n, SparkWidth, len(runes))
		}
	}
}

// --- renderGauge ---

func TestRenderGauge_Zero(t *testing.T) {
	result := renderGauge(0)
	if strings.Contains(result, "█") {
		t.Error("0% gauge should have no filled blocks")
	}
	if !strings.Contains(result, "[green]") {
		t.Error("should contain tview [green] tag")
	}
	if !strings.Contains(result, "[gray]") {
		t.Error("should contain tview [gray] tag")
	}
	if !strings.Contains(result, "[white]") {
		t.Error("should contain tview [white] tag")
	}
}

func TestRenderGauge_Full(t *testing.T) {
	result := renderGauge(100)
	if strings.Contains(result, "░") {
		t.Error("100% gauge should have no empty blocks")
	}
	filled := strings.Count(result, "█")
	if filled != gaugeWidth {
		t.Errorf("expected %d filled blocks, got %d", gaugeWidth, filled)
	}
}

func TestRenderGauge_Half(t *testing.T) {
	result := renderGauge(50)
	filled := strings.Count(result, "█")
	empty := strings.Count(result, "░")
	if filled+empty != gaugeWidth {
		t.Errorf("total blocks should be %d, got %d", gaugeWidth, filled+empty)
	}
	pct := 50.0
	expectedFilled := int(pct / 100 * float64(gaugeWidth))
	if filled != expectedFilled {
		t.Errorf("expected %d filled blocks, got %d", expectedFilled, filled)
	}
}

func TestRenderGauge_Over100(t *testing.T) {
	result := renderGauge(150)
	filled := strings.Count(result, "█")
	if filled != gaugeWidth {
		t.Errorf("over 100%% should cap at %d filled, got %d", gaugeWidth, filled)
	}
}

func TestRenderGauge_Negative(t *testing.T) {
	result := renderGauge(-20)
	if strings.Contains(result, "█") {
		t.Error("negative percent should be treated as 0%%")
	}
}

func TestRenderGauge_ContainsTviewTags(t *testing.T) {
	result := renderGauge(42)
	for _, tag := range []string{"[green]", "[gray]", "[white]"} {
		if !strings.Contains(result, tag) {
			t.Errorf("expected tview tag %s in gauge output", tag)
		}
	}
}

// --- formatDuration ---

func TestFormatDuration_UnderAnHour(t *testing.T) {
	d := 5 * time.Minute
	if got := formatDuration(d); got != "5m" {
		t.Errorf("expected 5m, got %s", got)
	}
}

func TestFormatDuration_Hours(t *testing.T) {
	d := 2*time.Hour + 30*time.Minute
	if got := formatDuration(d); got != "2h30m" {
		t.Errorf("expected 2h30m, got %s", got)
	}
}

func TestFormatDuration_Days(t *testing.T) {
	d := 3*24*time.Hour + 5*time.Hour + 12*time.Minute
	if got := formatDuration(d); got != "3d5h12m" {
		t.Errorf("expected 3d5h12m, got %s", got)
	}
}

func TestFormatDuration_Zero(t *testing.T) {
	if got := formatDuration(0); got != "0m" {
		t.Errorf("expected 0m, got %s", got)
	}
}

func TestFormatDuration_Exported(t *testing.T) {
	// Ensure the exported wrapper matches the unexported one.
	d := 1*time.Hour + 15*time.Minute
	if FormatDuration(d) != formatDuration(d) {
		t.Error("FormatDuration should delegate to formatDuration")
	}
}

// --- ToFloat64 (from state package) ---

func TestToFloat64_Float64(t *testing.T) {
	v, ok := state.ToFloat64(float64(3.14))
	if !ok || v != 3.14 {
		t.Errorf("expected (3.14, true), got (%v, %v)", v, ok)
	}
}

func TestToFloat64_Int64(t *testing.T) {
	v, ok := state.ToFloat64(int64(42))
	if !ok || v != 42 {
		t.Errorf("expected (42, true), got (%v, %v)", v, ok)
	}
}

func TestToFloat64_Int32(t *testing.T) {
	v, ok := state.ToFloat64(int32(7))
	if !ok || v != 7 {
		t.Errorf("expected (7, true), got (%v, %v)", v, ok)
	}
}

func TestToFloat64_Int(t *testing.T) {
	v, ok := state.ToFloat64(int(99))
	if !ok || v != 99 {
		t.Errorf("expected (99, true), got (%v, %v)", v, ok)
	}
}

func TestToFloat64_String(t *testing.T) {
	v, ok := state.ToFloat64("hello")
	if ok || v != 0 {
		t.Errorf("expected (0, false), got (%v, %v)", v, ok)
	}
}

func TestToFloat64_Nil(t *testing.T) {
	v, ok := state.ToFloat64(nil)
	if ok || v != 0 {
		t.Errorf("expected (0, false), got (%v, %v)", v, ok)
	}
}
