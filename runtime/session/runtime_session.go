package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/configfile"
)

const sessionRuntimeFileName = "runtime.json"

// SessionRuntimeData holds per-session dynamic overrides (agent route, LLM model).
type SessionRuntimeData struct {
	AgentID agentkit.AgentID `json:"agentId,omitempty"`
	Model   string           `json:"model,omitempty"`
}

func sessionRuntimeFilePath(storeDir string, id agentkit.SessionID) (string, error) {
	dir, err := sessionWorkDir(storeDir, id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionRuntimeFileName), nil
}

func loadSessionRuntime(storeDir string, id agentkit.SessionID) (SessionRuntimeData, error) {
	path, err := sessionRuntimeFilePath(storeDir, id)
	if err != nil {
		return SessionRuntimeData{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SessionRuntimeData{}, nil
		}
		return SessionRuntimeData{}, err
	}
	var data SessionRuntimeData
	if err := json.Unmarshal(raw, &data); err != nil {
		return SessionRuntimeData{}, err
	}
	data.Model = strings.TrimSpace(data.Model)
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(data.AgentID)))
	return data, nil
}

func saveSessionRuntime(storeDir string, id agentkit.SessionID, data SessionRuntimeData) error {
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(data.AgentID)))
	data.Model = strings.TrimSpace(data.Model)

	path, err := sessionRuntimeFilePath(storeDir, id)
	if err != nil {
		return err
	}
	if data.AgentID == "" && data.Model == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return configfile.WriteAtomic(path, raw, 0o644)
}

func setSessionRuntimeAgent(storeDir string, id agentkit.SessionID, agent agentkit.AgentID) error {
	data, err := loadSessionRuntime(storeDir, id)
	if err != nil {
		return err
	}
	data.AgentID = agentkit.AgentID(strings.TrimSpace(string(agent)))
	return saveSessionRuntime(storeDir, id, data)
}

func setSessionRuntimeModel(storeDir string, id agentkit.SessionID, model string) error {
	data, err := loadSessionRuntime(storeDir, id)
	if err != nil {
		return err
	}
	data.Model = strings.TrimSpace(model)
	return saveSessionRuntime(storeDir, id, data)
}
