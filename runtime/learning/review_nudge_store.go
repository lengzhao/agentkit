package learning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/lengzhao/agentkit/cap/filesystem"
)

// NudgeStore persists per-session memory review cadence.
// Path is a filesystem.Service-relative path.
type NudgeStore struct {
	FS   filesystem.Service
	Path string
}

func (s *NudgeStore) LoadSession(ctx context.Context, sessionID string) (NudgeSessionState, bool, error) {
	if sessionID == "" {
		return NudgeSessionState{}, false, fmt.Errorf("session id is required")
	}
	file, err := s.loadFile(ctx)
	if err != nil {
		return NudgeSessionState{}, false, err
	}
	st, ok := file.Sessions[sessionID]
	return st, ok, nil
}

func (s *NudgeStore) SaveSession(ctx context.Context, sessionID string, st NudgeSessionState) error {
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	file, err := s.loadFile(ctx)
	if err != nil {
		return err
	}
	if file.Sessions == nil {
		file.Sessions = make(map[string]NudgeSessionState)
	}
	file.Sessions[sessionID] = st
	return s.saveFile(ctx, file)
}

func (s *NudgeStore) loadFile(ctx context.Context) (NudgeFile, error) {
	if s.Path == "" {
		return NudgeFile{}, fmt.Errorf("nudge path is required")
	}
	data, err := s.FS.Read(ctx, s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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

func (s *NudgeStore) saveFile(ctx context.Context, file NudgeFile) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return s.FS.Write(ctx, s.Path, data)
}
