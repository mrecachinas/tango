package metrics

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/state"
)

func TestFormatDuration(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Minute, "5m"},
		{2*time.Hour + 30*time.Minute, "2h30m"},
		{3*24*time.Hour + 5*time.Hour + 12*time.Minute, "3d5h12m"},
		{0, "0m"},
	} {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%s) = %s, want %s", tc.d, got, tc.want)
		}
	}
}

func TestToFloat64(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want float64
		ok   bool
	}{
		{float64(3.14), 3.14, true}, {int64(42), 42, true},
		{int32(7), 7, true}, {int(99), 99, true},
		{"hello", 0, false}, {nil, 0, false},
	} {
		got, ok := state.ToFloat64(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ToFloat64(%v) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestExtractionHelpers(t *testing.T) {
	ss := bson.M{
		"uptime":      int64(90000),
		"connections": bson.M{"current": int32(3), "available": int64(97)},
		"wiredTiger": bson.M{"cache": bson.M{
			"bytes currently in the cache": int64(25), "maximum bytes configured": float64(100),
		}},
		"network":    bson.M{"bytesIn": int64(1024), "bytesOut": int32(2048)},
		"opcounters": bson.M{"insert": int32(1), "query": int64(2), "command": float64(3)},
	}
	if got := extractUptime(ss); got != "1d1h0m" {
		t.Errorf("uptime = %s", got)
	}
	if cur, avail := extractConnections(ss); cur != 3 || avail != 97 {
		t.Errorf("connections = %d/%d", cur, avail)
	}
	if got := extractCachePercent(ss); got != 25 {
		t.Errorf("cache = %v", got)
	}
	if v, ok := networkBytesIn(ss); !ok || v != 1024 {
		t.Errorf("bytesIn = (%v, %v)", v, ok)
	}
	if v, ok := networkBytesOut(ss); !ok || v != 2048 {
		t.Errorf("bytesOut = (%v, %v)", v, ok)
	}
	if v, ok := sumOpcounters(ss); !ok || v != 6 {
		t.Errorf("ops = (%v, %v)", v, ok)
	}
	if extractUptime(nil) != "?" || extractCachePercent(nil) != 0 {
		t.Error("missing status should yield unknown uptime and zero cache")
	}
	if cur, avail := extractConnections(nil); cur != 0 || avail != 0 {
		t.Errorf("missing connections = %d/%d", cur, avail)
	}
	if _, ok := networkBytesIn(nil); ok {
		t.Error("missing bytesIn is not a rate baseline")
	}
	if _, ok := networkBytesOut(nil); ok {
		t.Error("missing bytesOut is not a rate baseline")
	}
	if _, ok := sumOpcounters(nil); ok {
		t.Error("missing opcounters is not a rate baseline")
	}
}
