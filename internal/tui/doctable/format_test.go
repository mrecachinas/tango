package doctable

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/mjson"
	"tango/internal/theme"
)

func TestCellFormatting(t *testing.T) {
	id, err := primitive.ObjectIDFromHex("507f1f77bcf86cd799439011")
	if err != nil {
		t.Fatal(err)
	}
	decimal, err := primitive.ParseDecimal128("123.45")
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2025, 1, 23, 15, 4, 0, 0, time.UTC)
	tests := []struct {
		name     string
		value    any
		missing  bool
		midnight bool
		want     string
		kind     mjson.Kind
		numeric  bool
	}{
		{name: "id", value: id, want: "…9439011", kind: mjson.Helper},
		{name: "date", value: primitive.NewDateTimeFromTime(date), want: "2025-01-23 15:04", kind: mjson.Helper},
		{name: "UTC", value: date.In(time.FixedZone("local", -7*3600)), want: "2025-01-23 15:04", kind: mjson.Helper},
		{name: "date only", value: date, midnight: true, want: "2025-01-23", kind: mjson.Helper},
		{name: "text", value: `plain "text" \`, want: `plain "text" \`, kind: mjson.Text},
		{name: "controls", value: "a\nb\t\r\x1b\x7f\u0085", want: `a\nb\t\r\u001b\u007f\u0085`, kind: mjson.Text},
		{name: "Unicode", value: "日本 👩‍💻 é", want: "日本 👩‍💻 é", kind: mjson.Text},
		{name: "int", value: int32(-12), want: "-12", kind: mjson.Number, numeric: true},
		{name: "long", value: int64(9000000000), want: "9000000000", kind: mjson.Number, numeric: true},
		{name: "float", value: 12.5, want: "12.5", kind: mjson.Number, numeric: true},
		{name: "decimal", value: decimal, want: "123.45", kind: mjson.Number, numeric: true},
		{name: "bool", value: true, want: "true", kind: mjson.Bool},
		{name: "nil", want: "null", kind: mjson.Null},
		{name: "BSON null", value: primitive.Null{}, want: "null", kind: mjson.Null},
		{name: "missing", missing: true, want: "—"},
		{name: "binary", value: primitive.Binary{Subtype: 0, Data: []byte{1, 2}}, want: `BinData(0, "AQI=")`, kind: mjson.Helper},
		{name: "other", value: primitive.Timestamp{T: 12, I: 2}, want: "Timestamp(12, 2)", kind: mjson.Helper},
		{name: "document", value: numberedDoc(7), want: "{…7}", kind: mjson.Punct},
		{name: "empty document", value: bson.D{}, want: "{…0}", kind: mjson.Punct},
		{name: "array", value: bson.A{1, 2}, want: "[2]", kind: mjson.Punct},
		{name: "typed array", value: []string{"a", "b"}, want: "[2]", kind: mjson.Punct},
		{name: "empty array", value: bson.A{}, want: "[0]", kind: mjson.Punct},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := formatCell(tt.value, !tt.missing, tt.midnight)
			if c.text != tt.want || c.numeric != tt.numeric || c.missing != tt.missing || (!tt.missing && c.kind != tt.kind) {
				t.Fatalf("cell = %+v, want text %q kind %v numeric %v missing %v", c, tt.want, tt.kind, tt.numeric, tt.missing)
			}
		})
	}
}

func TestColumnDatePrecision(t *testing.T) {
	midnight := time.Date(2025, 1, 23, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		last any
		want string
	}{
		{name: "all midnight", last: primitive.NewDateTimeFromTime(midnight), want: "2025-01-23"},
		{name: "null ignored", last: nil, want: "2025-01-23"},
		{name: "any time", last: midnight.Add(time.Minute), want: "2025-01-23 00:00"},
		{name: "subsecond", last: midnight.Add(time.Millisecond), want: "2025-01-23 00:00"},
		{name: "mixed type", last: "not a date", want: "2025-01-23 00:00"},
		{name: "local midnight not UTC", last: time.Date(2025, 1, 23, 0, 0, 0, 0, time.FixedZone("local", 3600)), want: "2025-01-23 00:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.SetDocs([]bson.D{
				{{Key: "ts", Value: midnight}},
				{},
				{{Key: "ts", Value: tt.last}},
			})
			if got := m.cells[0][0].text; got != tt.want {
				t.Fatalf("date = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCellAlignmentAndTruncation(t *testing.T) {
	th := theme.Current()
	if got := ansi.Strip(renderCell(formatCell(int64(123), true, false), 6, false, th)); got != "   123" {
		t.Fatalf("number = %q", got)
	}
	if got := ansi.Strip(renderCell(formatCell("abcdef", true, false), 4, false, th)); got != "abc…" {
		t.Fatalf("text = %q", got)
	}
	if got := ansi.Strip(renderCell(formatCell("日日本", true, false), 4, false, th)); ansi.StringWidth(got) != 4 || !strings.Contains(got, "…") {
		t.Fatalf("wide truncated text = %q (%d cells)", got, ansi.StringWidth(got))
	}
	if got := ansi.Strip(renderCell(formatCell(nil, false, false), 4, false, th)); got != "—   " {
		t.Fatalf("missing = %q", got)
	}
}

func TestCellSemanticStylesAndCursorBackground(t *testing.T) {
	th := theme.Current()
	for _, test := range []struct {
		value any
		style mjson.Kind
	}{
		{int32(12), mjson.Number},
		{true, mjson.Bool},
		{nil, mjson.Null},
		{primitive.NewObjectID(), mjson.Helper},
		{"text", mjson.Text},
	} {
		c := formatCell(test.value, true, false)
		for _, cursor := range []bool{false, true} {
			expected := tokenStyle(th, test.style)
			if cursor {
				expected = expected.Inherit(th.CursorRow)
			}
			width := ansi.StringWidth(c.text)
			if got, want := renderCell(c, width, cursor, th), expected.Render(c.text); got != want {
				t.Fatalf("value %v cursor %v: got %q, want %q", test.value, cursor, got, want)
			}
		}
	}
	c := formatCell(nil, false, false)
	if got, want := renderCell(c, 1, true, th), th.Faint.Inherit(th.CursorRow).Render("—"); got != want {
		t.Fatalf("missing style: got %q, want %q", got, want)
	}
}
