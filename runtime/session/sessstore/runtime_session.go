package sessstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/filesystem"
)

const sessionRuntimeFileName = "runtime.json"

// sessionRuntimeData holds per-session dynamic overrides (agent route, LLM model).
type sessionRuntimeData struct {
	AgentID agentkit.AgentID `json:"agentId,omitempty"`
	Cwd     string           `json:"cwd,omitempty"`
	Model   string           `json:"model,omitempty"`
}

func sessionRuntimeFilePath(storeDir string, id agentkit.SessionID) (string, error) {
	dir, err := sessionWorkDir(storeDir, id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionRuntimeFileName), nil
}

func loadSessionRuntime(storeDir string, id agentkit.SessionID) (sessionRuntimeData, error) {
	path, err := sessionRuntimeFilePath(storeDir, id)
	if err != nil {
		return sessionRuntimeData{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sessionRuntimeData{}, nil
		}
		return sessionRuntimeData{}, err
	}
	var data sessionRuntimeData
	if err := json.Unmarshal(raw, &data); err != nil {
		return sessionRuntimeData{}, err
	}
	data.Model = strings.TrimSpace(data.Model)
	data.Cwd = strings.TrimSpace(data.Cwd)
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(data.AgentID)))
	return data, nil
}

func saveSessionRuntime(storeDir string, id agentkit.SessionID, data sessionRuntimeData) error {
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(data.AgentID)))
	data.Model = strings.TrimSpace(data.Model)
	data.Cwd = strings.TrimSpace(data.Cwd)

	path, err := sessionRuntimeFilePath(storeDir, id)
	if err != nil {
		return err
	}
	if data.AgentID == "" && data.Model == "" && data.Cwd == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return filesystem.WriteAtomic(path, raw, 0o644)
}

// Runtime snapshots are shared by resolved file path, including across Store
// instances. Reads are memory-only after the first load. Writes publish a new
// snapshot only after the entire file has been persisted successfully, and are
// merged from the file on disk so a snapshot that predates another writer's
// update cannot replay stale (or empty) values over it.
type runtimeSnapshot struct {
	mu     sync.Mutex
	loaded bool
	data   sessionRuntimeData
}

var runtimeSnapshots sync.Map // absolute runtime.json path -> *runtimeSnapshot

func sessionRuntimeSnapshot(dir string, id agentkit.SessionID) (*runtimeSnapshot, error) {
	path, err := sessionRuntimeFilePath(dir, id)
	if err != nil {
		return nil, err
	}
	value, _ := runtimeSnapshots.LoadOrStore(path, &runtimeSnapshot{})
	return value.(*runtimeSnapshot), nil
}
func (s *runtimeSnapshot) load(dir string, id agentkit.SessionID) error {
	if s.loaded {
		return nil
	}
	data, err := loadSessionRuntime(dir, id)
	if err != nil {
		return err
	}
	s.data = data
	s.loaded = true
	return nil
}
func readSessionRuntimeSnapshot(dir string, id agentkit.SessionID) (sessionRuntimeData, error) {
	state, err := sessionRuntimeSnapshot(dir, id)
	if err != nil {
		return sessionRuntimeData{}, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := state.load(dir, id); err != nil {
		return sessionRuntimeData{}, err
	}
	return state.data, nil
}
func updateSessionRuntime(dir string, id agentkit.SessionID, update func(*sessionRuntimeData)) error {
	state, err := sessionRuntimeSnapshot(dir, id)
	if err != nil {
		return err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := state.load(dir, id); err != nil {
		return err
	}
	// The file is the shared authority between processes and between snapshots
	// that were primed at different times: merge from disk, then apply the one
	// field this call owns. Replaying a stale snapshot instead would drop a
	// binding another writer just persisted, and an all-empty result deletes
	// runtime.json outright.
	base, err := loadSessionRuntime(dir, id)
	if err != nil {
		// Unreadable or corrupt file: keep the in-memory view rather than
		// overwriting data we cannot parse.
		base = state.data
	}
	next := base
	update(&next)
	if err := saveSessionRuntime(dir, id, next); err != nil {
		return err
	}
	state.data = next
	return nil
}
func setSessionRuntimeAgent(dir string, id agentkit.SessionID, agent agentkit.AgentID) error {
	return updateSessionRuntime(dir, id, func(data *sessionRuntimeData) { data.AgentID = agentkit.AgentID(strings.TrimSpace(string(agent))) })
}
func setSessionRuntimeModel(dir string, id agentkit.SessionID, model string) error {
	return updateSessionRuntime(dir, id, func(data *sessionRuntimeData) { data.Model = strings.TrimSpace(model) })
}
func setSessionRuntimeCwd(dir string, id agentkit.SessionID, cwd string) error {
	return updateSessionRuntime(dir, id, func(data *sessionRuntimeData) { data.Cwd = strings.TrimSpace(cwd) })
}
