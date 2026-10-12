package jsontree

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/mjson"
	"tango/internal/theme"
)

func press(s string) tea.KeyPressMsg {
	codes := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "space": tea.KeySpace,
	}
	if code, ok := codes[s]; ok {
		if s == "space" {
			return tea.KeyPressMsg{Code: code, Text: " "}
		}
		return tea.KeyPressMsg{Code: code}
	}
	if strings.HasPrefix(s, "ctrl+") {
		return tea.KeyPressMsg{Code: []rune(s)[5], Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func fixture() *Model {
	m := New()
	m.SetSize(40, 10)
	m.SetDoc(bson.D{
		{Key: "metadata", Value: bson.D{
			{Key: "browser", Value: "Chrome"},
			{Key: "nested", Value: bson.D{{Key: "enabled", Value: true}}},
		}},
		{Key: "events", Value: bson.A{bson.D{{Key: "page", Value: "/"}}, 42}},
		{Key: "null", Value: nil},
	})
	return m
}

func paths(m *Model) []string {
	out := make([]string, len(m.rows))
	for i, row := range m.rows {
		out[i] = row.node.path
	}
	return out
}

func TestDefaultExpansionAndFlattening(t *testing.T) {
	m := fixture()
	want := []string{"", "metadata", "metadata.browser", "metadata.nested", "events", "events[0]", "events[1]", "null"}
	if got := paths(m); !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	big := make(bson.A, 21)
	small := make(bson.A, 20)
	m.SetDoc(bson.D{{Key: "big", Value: big}, {Key: "small", Value: small}})
	if m.root.children[0].expanded || !m.root.children[1].expanded || len(m.rows) != 23 {
		t.Fatal("only top-level containers with >20 children should begin collapsed")
	}
	m.SetDoc(bson.D{{Key: "zero", Value: bson.A{}}, {Key: "empty", Value: bson.D{}}})
	if len(m.rows) != 3 || !m.rows[1].node.container || !m.rows[2].node.expanded {
		t.Fatal("empty containers must remain containers")
	}
}

func TestToggleExpandCollapseAndParent(t *testing.T) {
	m := fixture()
	m.Update(press("right"))
	if m.Path() != "metadata" {
		t.Fatal("right on expanded root must move to first child")
	}
	m.Update(press("enter"))
	if len(m.rows) != 6 || m.Path() != "metadata" {
		t.Fatal("enter must collapse container without moving cursor")
	}
	m.Update(press("l"))
	if len(m.rows) != 8 || m.Path() != "metadata" {
		t.Fatal("l must expand collapsed container")
	}
	m.Update(press("l"))
	if m.Path() != "metadata.browser" {
		t.Fatal("l on expanded container must move to first child")
	}
	m.Update(press("h"))
	if m.Path() != "metadata" {
		t.Fatal("h on scalar must move to parent")
	}
	m.Update(press("left"))
	m.Update(press("left"))
	if m.Path() != "" {
		t.Fatal("left on collapsed container must move to parent")
	}
	m.Update(press("space"))
	if len(m.rows) != 1 {
		t.Fatal("space must toggle root")
	}
	m.Update(press(" "))
	if len(m.rows) != 6 {
		t.Fatal("literal space must also toggle root")
	}
	m.Update(press("j"))
	m.Update(press("right"))
	m.Update(press("j"))
	before := len(m.rows)
	m.Update(press("enter"))
	if len(m.rows) != before {
		t.Fatal("toggle on scalar must have no effect")
	}
}

func TestExpandAllCollapseAllAndSelectedAncestor(t *testing.T) {
	m := fixture()
	m.Update(press("E"))
	if len(m.rows) != 10 {
		t.Fatalf("expand all must recursively expose nested nodes: %v", paths(m))
	}
	m.selectNode(m.root.children[0].children[1].children[0])
	if m.Path() != "metadata.nested.enabled" {
		t.Fatal("nested path missing")
	}
	m.Update(press("C"))
	if len(m.rows) != 4 || m.Path() != "metadata" || !m.root.expanded {
		t.Fatal("collapse all should keep root open and move hidden cursor to visible ancestor")
	}
	for _, child := range m.root.children {
		if child.expanded {
			t.Fatal("collapse all must collapse top-level containers")
		}
	}
	m.Update(press("E"))
	if len(m.rows) != 10 || m.Path() != "metadata" {
		t.Fatal("expand all must preserve visible selection")
	}
}

func TestNavigationKeysAndVisibility(t *testing.T) {
	cases := []struct {
		key         string
		start, want int
	}{
		{"j", 5, 6}, {"down", 5, 6}, {"k", 5, 4}, {"up", 5, 4},
		{"g", 5, 0}, {"home", 5, 0}, {"G", 5, 20}, {"end", 5, 20},
		{"ctrl+d", 5, 7}, {"ctrl+u", 5, 3}, {"pgdown", 5, 9}, {"pgup", 5, 1},
		{"k", 0, 0}, {"j", 20, 20},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%d", tc.key, tc.start), func(t *testing.T) {
			m := New()
			var doc bson.D
			for i := 0; i < 20; i++ {
				doc = append(doc, bson.E{Key: fmt.Sprintf("key-%02d", i), Value: i})
			}
			m.SetDoc(doc)
			m.SetSize(20, 4)
			m.cursor = tc.start
			if !m.Handles(press(tc.key)) {
				t.Fatal("navigation key must be consumed")
			}
			m.Update(press(tc.key))
			if m.cursor != tc.want || m.cursor < m.offset || m.cursor >= m.offset+m.height {
				t.Fatalf("cursor=%d offset=%d want=%d visible", m.cursor, m.offset, tc.want)
			}
		})
	}
}

func TestScalarFormattingAndGuides(t *testing.T) {
	id := primitive.NewObjectID()
	values := []interface{}{
		"文🚀", int32(7), int64(9), true, nil, id,
		primitive.DateTime(0), primitive.Regex{Pattern: "hello", Options: "i"},
		primitive.Binary{Subtype: 0, Data: []byte{1, 2, 3}}, primitive.Null{},
	}
	m := New()
	var doc bson.D
	for i, value := range values {
		doc = append(doc, bson.E{Key: fmt.Sprintf("field%d", i), Value: value})
	}
	m.SetDoc(doc)
	m.SetSize(140, len(values)+1)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, value := range values {
		if !strings.Contains(lines[i+1], mjson.Compact(value)) {
			t.Fatalf("scalar formatting changed: %q, want %q", lines[i+1], mjson.Compact(value))
		}
	}
	if !strings.HasPrefix(lines[1], "├─ ") || !strings.HasPrefix(lines[len(lines)-1], "└─ ") {
		t.Fatal("tree must render branch guides")
	}
	m = fixture()
	lines = strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(lines[0], "▾ {3}") || !strings.Contains(lines[1], "metadata: {2}") ||
		!strings.Contains(lines[2], "│  ├─") || !strings.Contains(lines[4], "events: [2]") ||
		!strings.Contains(lines[3], "▸ nested") {
		t.Fatalf("container counts/guides incorrect: %q", lines)
	}
}

func TestMapRawAndPlainArrayContainers(t *testing.T) {
	raw, err := bson.Marshal(bson.D{{Key: "inner", Value: 1}})
	if err != nil {
		t.Fatal(err)
	}
	m := New()
	m.SetDoc(bson.D{
		{Key: "map", Value: bson.M{"z": 2, "a": 1}},
		{Key: "plain", Value: map[string]interface{}{"inner": true}},
		{Key: "array", Value: []interface{}{1}},
		{Key: "raw", Value: bson.Raw(raw)},
	})
	want := []string{"", "map", "map.a", "map.z", "plain", "plain.inner", "array", "array[0]", "raw", "raw.inner"}
	if got := paths(m); !reflect.DeepEqual(got, want) {
		t.Fatalf("map, raw and plain-array structure = %v", got)
	}
}

func TestDocumentResetAndRouting(t *testing.T) {
	m := fixture()
	m.Update(press("G"))
	m.SetDoc(bson.D{{Key: "new", Value: "doc"}})
	if m.Path() != "" || m.cursor != 0 || m.offset != 0 || len(m.rows) != 2 {
		t.Fatal("new document must reset cursor and expansion state")
	}
	if m.Handles(press("q")) || m.Handles(press("esc")) || m.Update("not a key") != nil {
		t.Fatal("unbound keys and non-key messages belong to root")
	}
	m.Keys.ExpandAll = key.NewBinding(key.WithKeys("q"))
	if !m.Handles(press("q")) {
		t.Fatal("custom keymap must affect routing")
	}
	m = New()
	if m.Update(press("E")) != nil || m.Path() != "" {
		t.Fatal("uninitialized tree must safely ignore keys")
	}
}

func TestDimensionInvariantsAndSyntaxCursorStyles(t *testing.T) {
	old := theme.Current()
	t.Cleanup(func() { theme.Set(old) })
	for _, palette := range []theme.Palette{theme.Dark(), theme.Light(), theme.Mono()} {
		theme.Set(theme.New(palette))
		for state := 0; state < 4; state++ {
			m := New()
			if state != 0 {
				m.SetDoc(bson.D{
					{Key: "文🚀e\u0301\nkey", Value: bson.D{{Key: "a\tb", Value: "line\nbreak"}}},
					{Key: "bool", Value: true}, {Key: "👩‍💻🇺🇸e\u0301", Value: nil},
				})
				switch state {
				case 2:
					m.Update(press("C"))
				case 3:
					m.Update(press("E"))
					m.Update(press("G"))
				}
			}
			for w := 0; w <= 35; w++ {
				for h := 0; h <= 8; h++ {
					m.SetSize(w, h)
					view := m.View()
					if h == 0 {
						if view != "" {
							t.Fatal("zero height must render nothing")
						}
						continue
					}
					lines := strings.Split(view, "\n")
					if len(lines) != h {
						t.Fatalf("size %dx%d got %d lines", w, h, len(lines))
					}
					for _, line := range lines {
						if ansi.StringWidth(line) != w || ansi.StringWidth(ansi.Strip(line)) != w {
							t.Fatalf("size %dx%d got line %q", w, h, line)
						}
					}
				}
			}
		}
		m := New()
		m.SetDoc(bson.D{{Key: "text", Value: "hello"}})
		m.SetSize(40, 2)
		m.Update(press("j"))
		style := theme.Current().JSONString.Inherit(theme.Current().CursorRow)
		if !strings.Contains(strings.Split(m.View(), "\n")[1], style.Render(`"hello"`)) {
			t.Fatal("JSON syntax spans must retain cursor background")
		}
	}
	m := New()
	m.SetSize(-10, -10)
	if m.View() != "" {
		t.Fatal("negative sizes should clamp")
	}
}
