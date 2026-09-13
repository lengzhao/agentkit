package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lengzhao/agentkit/runtime/configfile"
)

// NudgeStore persists per-session memory review cadence.
type NudgeStore struct {
	Path string
}

func (s *NudgeStore) LoadSession(sessionID string) (NudgeSessionState, bool, error) {
	if sessionID == "" {
		return NudgeSessionState{}, false, fmt.Errorf("session id is required")
	}
	file, err := s.loadFile()
	if err != nil {
		return NudgeSessionState{}, false, err
	}
	st, ok := file.Sessions[sessionID]
	return st, ok, nil
}

func (s *NudgeStore) SaveSession(sessionID string, st NudgeSessionState) error {
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	file, err := s.loadFile()
	if err != nil {
		return err
	}
	if file.Sessions == nil {
		file.Sessions = make(map[string]NudgeSessionState)
	}
	file.Sessions[sessionID] = st
	return s.saveFile(file)
}

func (s *NudgeStore) loadFile() (NudgeFile, error) {
	if s.Path == "" {
		return NudgeFile{}, fmt.Errorf("nudge path is required")
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return NudgeFile{Sessions: map[string]NudgeSessionState{}}, nil
		}
		return NudgeFile{}, err
	}
	var file NudgeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return NudgeFile{}, err
	}
	if file.Sessions == nil {
		file.Sessions = map[string]NudgeSessionState{}
	}
	return file, nil
}

func (s *NudgeStore) saveFile(file NudgeFile) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return configfile.WriteAtomic(s.Path, data, 0o644)
}
