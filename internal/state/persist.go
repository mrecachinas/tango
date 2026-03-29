package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type PersistedState struct {
	LastDB         string   `json:"last_db,omitempty"`
	LastCollection string   `json:"last_collection,omitempty"`
	PageSize       int      `json:"page_size,omitempty"`
	QueryHistory   []string `json:"query_history,omitempty"`
}

const maxQueryHistory = 50

func stateFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".tango.state.json")
}

func SaveState() {
	path := stateFilePath()
	if path == "" {
		return
	}

	ps := PersistedState{
		LastDB:         S.CurrentDB,
		LastCollection: S.CurrentColl,
		PageSize:       PageSize,
		QueryHistory:   S.QueryHistory,
	}

	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0600)
}

func LoadState() {
	path := stateFilePath()
	if path == "" {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return // file doesn't exist yet, that's fine
	}

	var ps PersistedState
	if err := json.Unmarshal(data, &ps); err != nil {
		return
	}

	if ps.PageSize > 0 && ps.PageSize <= 10000 {
		PageSize = ps.PageSize
	}
	S.LastDB = ps.LastDB
	S.LastCollection = ps.LastCollection
	S.QueryHistory = ps.QueryHistory
}

func AddToQueryHistory(query string) {
	if query == "" {
		return
	}
	// Don't add duplicates of the most recent entry
	if len(S.QueryHistory) > 0 && S.QueryHistory[len(S.QueryHistory)-1] == query {
		return
	}
	S.QueryHistory = append(S.QueryHistory, query)
	if len(S.QueryHistory) > maxQueryHistory {
		S.QueryHistory = S.QueryHistory[len(S.QueryHistory)-maxQueryHistory:]
	}
}
