// Package advisor turns MongoDB explain output, profiler entries and index
// statistics into findings and index recommendations. It is pure logic: the
// caller fetches the documents and renders the results.
package advisor

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"tango/internal/mjson"
)

// get reads key from d. A dotted key walks subdocuments unless d has a field
// with that exact name (index names such as "a.b_1" contain dots).
func get(d bson.D, key string) interface{} {
	if v, ok := mjson.Get(d, key); ok {
		return v
	}
	for i := strings.IndexByte(key, '.'); i > 0; i = indexFrom(key, '.', i+1) {
		if sub := toD(get(d, key[:i])); sub != nil {
			if v := get(sub, key[i+1:]); v != nil {
				return v
			}
		}
	}
	return nil
}

func indexFrom(s string, c byte, from int) int {
	if j := strings.IndexByte(s[from:], c); j >= 0 {
		return from + j
	}
	return -1
}

func getDoc(d bson.D, key string) bson.D {
	return toD(get(d, key))
}

func getArr(d bson.D, key string) bson.A {
	switch v := get(d, key).(type) {
	case bson.A:
		return v
	case []interface{}:
		return bson.A(v)
	}
	return nil
}

func getStr(d bson.D, key string) string {
	s, _ := get(d, key).(string)
	return s
}

func getBool(d bson.D, key string) bool {
	switch v := get(d, key).(type) {
	case bool:
		return v
	default:
		n, ok := num(v)
		return ok && n != 0
	}
}

func getNum(d bson.D, key string) (float64, bool) {
	return num(get(d, key))
}

func getInt(d bson.D, key string) int64 {
	n, _ := getNum(d, key)
	return int64(n)
}

func num(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case int:
		return float64(n), true
	case primitive.Decimal128:
		f, err := strconv.ParseFloat(n.String(), 64)
		return f, err == nil
	}
	return 0, false
}

func toD(v interface{}) bson.D {
	switch d := v.(type) {
	case bson.D:
		return d
	case bson.M:
		out := make(bson.D, 0, len(d))
		for k, val := range d {
			out = append(out, bson.E{Key: k, Value: val})
		}
		return out
	}
	return nil
}

func asDocs(a bson.A) []bson.D {
	out := make([]bson.D, 0, len(a))
	for _, v := range a {
		if d := toD(v); d != nil {
			out = append(out, d)
		}
	}
	return out
}

func timeOf(v interface{}) (time.Time, bool) {
	switch t := v.(type) {
	case primitive.DateTime:
		return t.Time(), true
	case time.Time:
		return t, true
	case primitive.Timestamp:
		return time.Unix(int64(t.T), 0), true
	}
	return time.Time{}, false
}

func compact(v interface{}, n int) string {
	s := strings.ReplaceAll(mjson.Compact(v), "\n", " ")
	if r := []rune(s); n > 0 && len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Count formats an integer with thousands separators.
func Count(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// Millis formats a duration in milliseconds compactly.
func Millis(ms float64) string {
	switch {
	case ms >= 60_000:
		return fmt.Sprintf("%.1fm", ms/60_000)
	case ms >= 1000:
		return fmt.Sprintf("%.1fs", ms/1000)
	case ms >= 10 || ms == float64(int64(ms)):
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.1fms", ms)
}

// Bytes formats a byte count.
func Bytes(n float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f B", n)
	}
	return fmt.Sprintf("%.1f %s", n, units[i])
}

func ratio(a, b int64) string {
	if b <= 0 {
		b = 1
	}
	r := float64(a) / float64(b)
	if r >= 10 {
		return Count(int64(r+0.5)) + ":1"
	}
	return strconv.FormatFloat(r, 'f', 1, 64) + ":1"
}

// CollRef renders db.<coll> in shell syntax, falling back to getCollection
// for names that are not plain identifiers.
func CollRef(coll string) string {
	ident := coll != ""
	for i, r := range coll {
		letter := r == '_' || r == '$' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !letter && (i == 0 || r < '0' || r > '9') {
			ident = false
			break
		}
	}
	switch coll {
	case "getCollection", "getSiblingDB", "getName", "runCommand", "aggregate", "watch", "stats":
		ident = false
	}
	if ident {
		return "db." + coll
	}
	return "db.getCollection(" + strconv.Quote(coll) + ")"
}
