package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
)

// CLICurrentLinkName is a symlink in the session store dir pointing at the
// JSONL file CLI should attach to on the next start.
const CLICurrentLinkName = "cli_current.jsonl"

// CLICurrentStore tracks which CLI session is active across process restarts.
type CLICurrentStore interface {
	ResolveCLICurrent(ctx context.Context) (agentkit.SessionID, error)
	SetCLICurrent(ctx context.Context, id agentkit.SessionID) error
}

// ResolveCLICurrent returns the session id behind cli_current.jsonl, or
// DefaultCLISessionID when the link is missing. A missing link is also written
// so the default session file is discoverable without knowing the id format.
func (s *Store) ResolveCLICurrent(ctx context.Context) (agentkit.SessionID, error) {
	dir, err := s.workspace.Resolve(ctx, s.relDir)
	if err != nil {
		return "", err
	}
	linkPath := filepath.Join(dir, CLICurrentLinkName)
	target, err := os.Readlink(linkPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := s.SetCLICurrent(ctx, DefaultCLISessionID); err != nil {
				return DefaultCLISessionID, err
			}
			return DefaultCLISessionID, nil
		}
		return "", err
	}
	id, err := sessionIDFromFileName(filepath.Base(target))
	if err != nil {
		return DefaultCLISessionID, nil
	}
	return id, nil
}

// SetCLICurrent updates cli_current.jsonl to point at the JSONL for id.
func (s *Store) SetCLICurrent(ctx context.Context, id agentkit.SessionID) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	dir, err := s.workspace.Resolve(ctx, s.relDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	targetName, err := safeSessionFileName(id)
	if err != nil {
		return err
	}
	linkPath := filepath.Join(dir, CLICurrentLinkName)
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(targetName, linkPath)
}

func sessionIDFromFileName(name string) (agentkit.SessionID, error) {
	if !strings.HasSuffix(name, ".jsonl") {
		return "", fmt.Errorf("not a session file: %q", name)
	}
	base := strings.TrimSuffix(name, ".jsonl")
	if base == "cli_default" {
		return DefaultCLISessionID, nil
	}
	if !strings.HasPrefix(base, "cli_") {
		return "", fmt.Errorf("not a cli session file: %q", name)
	}
	rest := strings.TrimPrefix(base, "cli_")
	if idx := strings.LastIndex(rest, "_"); idx > 0 {
		rest = rest[:idx] + "." + rest[idx+1:]
	}
	return agentkit.SessionID("cli:" + rest), nil
}
