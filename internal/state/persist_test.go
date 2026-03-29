package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	S = &AppState{
		QueryHistory: []string{},
	}
	os.Exit(m.Run())
}

// resetState resets the global state to clean defaults for each test.
func resetState() {
	S = &AppState{
		QueryHistory: []string{},
	}
	PageSize = 50
}

// --- AddToQueryHistory ---

func TestAddToQueryHistory_Appends(t *testing.T) {
	resetState()
	AddToQueryHistory("db.users.find()")
	if len(S.QueryHistory) != 1 || S.QueryHistory[0] != "db.users.find()" {
		t.Fatalf("expected [db.users.find()], got %v", S.QueryHistory)
	}
	AddToQueryHistory("{\"name\": \"Alice\"}")
	if len(S.QueryHistory) != 2 || S.QueryHistory[1] != "{\"name\": \"Alice\"}" {
		t.Fatalf("expected 2 entries, got %v", S.QueryHistory)
	}
}

func TestAddToQueryHistory_EmptyIsNoop(t *testing.T) {
	resetState()
	AddToQueryHistory("")
	if len(S.QueryHistory) != 0 {
		t.Fatalf("expected empty history, got %v", S.QueryHistory)
	}
}

func TestAddToQueryHistory_SkipConsecutiveDuplicate(t *testing.T) {
	resetState()
	AddToQueryHistory("q1")
	AddToQueryHistory("q1")
	if len(S.QueryHistory) != 1 {
		t.Fatalf("expected 1 entry, got %v", S.QueryHistory)
	}
}

func TestAddToQueryHistory_AllowNonConsecutiveDuplicate(t *testing.T) {
	resetState()
	AddToQueryHistory("q1")
	AddToQueryHistory("q2")
	AddToQueryHistory("q1")
	if len(S.QueryHistory) != 3 {
		t.Fatalf("expected 3 entries, got %v", S.QueryHistory)
	}
}

func TestAddToQueryHistory_CapsAtMax(t *testing.T) {
	resetState()
	for i := 0; i < maxQueryHistory+10; i++ {
		AddToQueryHistory(string(rune('A'+i%26)) + string(rune('0'+i/26)))
	}
	if len(S.QueryHistory) != maxQueryHistory {
		t.Fatalf("expected %d entries, got %d", maxQueryHistory, len(S.QueryHistory))
	}
	// Oldest entries should have been dropped; last entry is the most recent.
	last := S.QueryHistory[maxQueryHistory-1]
	expected := string(rune('A'+(maxQueryHistory+9)%26)) + string(rune('0'+(maxQueryHistory+9)/26))
	if last != expected {
		t.Fatalf("expected last entry %q, got %q", expected, last)
	}
}

// --- PersistedState JSON serialization ---

func TestPersistedState_JSONRoundTrip(t *testing.T) {
	ps := PersistedState{
		LastDB:         "mydb",
		LastCollection: "users",
		PageSize:       25,
		QueryHistory:   []string{"{}", "{\"a\":1}"},
	}
	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var got PersistedState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.LastDB != ps.LastDB || got.LastCollection != ps.LastCollection ||
		got.PageSize != ps.PageSize || len(got.QueryHistory) != len(ps.QueryHistory) {
		t.Fatalf("round-trip mismatch: got %+v", got)
	}
	for i := range ps.QueryHistory {
		if got.QueryHistory[i] != ps.QueryHistory[i] {
			t.Fatalf("history[%d]: expected %q, got %q", i, ps.QueryHistory[i], got.QueryHistory[i])
		}
	}
}

func TestPersistedState_OmitsEmptyFields(t *testing.T) {
	ps := PersistedState{}
	data, _ := json.Marshal(ps)
	if string(data) != "{}" {
		t.Fatalf("expected {}, got %s", data)
	}
}

// --- SaveState / LoadState round-trip ---

func TestSaveLoadState_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	resetState()
	S.CurrentDB = "testdb"
	S.CurrentColl = "testcoll"
	PageSize = 42
	S.QueryHistory = []string{"q1", "q2", "q3"}

	SaveState()

	// Verify file was created
	fp := filepath.Join(tmpDir, ".tango.state.json")
	if _, err := os.Stat(fp); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	// Reset and reload
	resetState()
	LoadState()

	if S.LastDB != "testdb" {
		t.Errorf("LastDB: expected %q, got %q", "testdb", S.LastDB)
	}
	if S.LastCollection != "testcoll" {
		t.Errorf("LastCollection: expected %q, got %q", "testcoll", S.LastCollection)
	}
	if PageSize != 42 {
		t.Errorf("PageSize: expected 42, got %d", PageSize)
	}
	if len(S.QueryHistory) != 3 || S.QueryHistory[0] != "q1" || S.QueryHistory[2] != "q3" {
		t.Errorf("QueryHistory: expected [q1 q2 q3], got %v", S.QueryHistory)
	}
}

// --- LoadState with missing file ---

func TestLoadState_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	resetState()
	LoadState() // should not error or panic

	if S.LastDB != "" || S.LastCollection != "" {
		t.Errorf("expected empty defaults, got db=%q coll=%q", S.LastDB, S.LastCollection)
	}
	if PageSize != 50 {
		t.Errorf("expected default PageSize 50, got %d", PageSize)
	}
}

// --- LoadState with corrupt JSON ---

func TestLoadState_CorruptJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	fp := filepath.Join(tmpDir, ".tango.state.json")
	_ = os.WriteFile(fp, []byte("{not valid json!!!"), 0600)

	resetState()
	LoadState() // should not error or panic

	if S.LastDB != "" || S.LastCollection != "" {
		t.Errorf("expected empty defaults after corrupt JSON, got db=%q coll=%q", S.LastDB, S.LastCollection)
	}
	if PageSize != 50 {
		t.Errorf("expected default PageSize 50 after corrupt JSON, got %d", PageSize)
	}
}

// --- PageSize bounds checking ---

func TestLoadState_PageSizeZeroIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	fp := filepath.Join(tmpDir, ".tango.state.json")
	data, _ := json.Marshal(PersistedState{PageSize: 0, LastDB: "db1"})
	_ = os.WriteFile(fp, data, 0600)

	resetState()
	LoadState()

	if PageSize != 50 {
		t.Errorf("expected default PageSize 50 when persisted is 0, got %d", PageSize)
	}
	if S.LastDB != "db1" {
		t.Errorf("expected LastDB=db1, got %q", S.LastDB)
	}
}

func TestLoadState_PageSizeTooLargeIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	fp := filepath.Join(tmpDir, ".tango.state.json")
	data, _ := json.Marshal(PersistedState{PageSize: 10001, LastDB: "db2"})
	_ = os.WriteFile(fp, data, 0600)

	resetState()
	LoadState()

	if PageSize != 50 {
		t.Errorf("expected default PageSize 50 when persisted is 10001, got %d", PageSize)
	}
	if S.LastDB != "db2" {
		t.Errorf("expected LastDB=db2, got %q", S.LastDB)
	}
}

func TestLoadState_PageSizeAtBounds(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Lower bound: 1 should be accepted
	fp := filepath.Join(tmpDir, ".tango.state.json")
	data, _ := json.Marshal(PersistedState{PageSize: 1})
	_ = os.WriteFile(fp, data, 0600)

	resetState()
	LoadState()
	if PageSize != 1 {
		t.Errorf("expected PageSize 1, got %d", PageSize)
	}

	// Upper bound: 10000 should be accepted
	data, _ = json.Marshal(PersistedState{PageSize: 10000})
	_ = os.WriteFile(fp, data, 0600)

	resetState()
	LoadState()
	if PageSize != 10000 {
		t.Errorf("expected PageSize 10000, got %d", PageSize)
	}
}
