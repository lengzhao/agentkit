package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
)

const agentBindFileName = "agent.json"
const activeSessionFileName = "current.json"

// AgentBindData is stored in each session work dir as agent.json.
type AgentBindData struct {
	AgentID agentkit.AgentID `json:"agentId"`
}

type ActiveSessionData struct {
	SessionID agentkit.SessionID `json:"sessionId"`
}

func sessionWorkDir(storeDir string, id agentkit.SessionID) (string, error) {
	name, err := safeSessionName(id)
	if err != nil {
		return "", err
	}
	path := filepath.Join(storeDir, name)
	absDir, err := filepath.Abs(storeDir)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absPath, absDir+string(os.PathSeparator)) && absPath != absDir {
		return "", fmt.Errorf("session path escapes store dir")
	}
	return absPath, nil
}

func agentBindFilePath(storeDir string, id agentkit.SessionID) (string, error) {
	dir, err := sessionWorkDir(storeDir, id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, agentBindFileName), nil
}

func activeSessionFilePath(storeDir string, id agentkit.SessionID) (string, error) {
	dir, err := sessionWorkDir(storeDir, id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, activeSessionFileName), nil
}

func readAgentBindFile(path string) (agentkit.AgentID, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var bind AgentBindData
	if err := json.Unmarshal(data, &bind); err != nil {
		return "", err
	}
	return bind.AgentID, nil
}

func writeAgentBindFile(path string, id agentkit.AgentID) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(AgentBindData{AgentID: id})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func readActiveSessionFile(path string) (agentkit.SessionID, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var active ActiveSessionData
	if err := json.Unmarshal(data, &active); err != nil {
		return "", err
	}
	return active.SessionID, nil
}

func writeActiveSessionFile(path string, id agentkit.SessionID) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(ActiveSessionData{SessionID: id})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
