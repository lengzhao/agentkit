package sessbind

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// CwdBinding shares session/channel keys with the catalog commands. No global,
// thread or user override is exposed for cwd.
type CwdBinding struct{}

func (CwdBinding) Key() string { return "cwd" }
func (CwdBinding) Read(ctx context.Context, level Level, key agentkit.SessionID, _ workspace.Service, store agentkit.SessionStore) (string, error) {
	if level != LevelSession && level != LevelChannel || key == "" {
		return "", nil
	}
	rs, ok := store.(agentkit.SessionCwdStore)
	if !ok {
		return "", nil
	}
	if level == LevelChannel {
		key = channelBindKey("cwd", key)
	}
	return rs.CwdBind(ctx, key)
}
func (CwdBinding) Write(ctx context.Context, level Level, key agentkit.SessionID, _ workspace.Service, store agentkit.SessionStore, value string) error {
	if level != LevelSession && level != LevelChannel {
		return fmt.Errorf("cwd supports session and channel bindings only")
	}
	if key == "" {
		return fmt.Errorf("%s key is required", level)
	}
	rs, ok := store.(agentkit.SessionCwdStore)
	if !ok {
		return fmt.Errorf("session store does not support cwd bindings")
	}
	if level == LevelChannel {
		key = channelBindKey("cwd", key)
	}
	return rs.SetCwdBind(ctx, key, value)
}

// ValidateCwd resolves using the host workspace policy and requires an existing
// directory. It never creates a directory or changes the process working directory.
func ValidateCwd(ctx context.Context, ws workspace.Service, path string) (string, error) {
	if ws == nil {
		return "", fmt.Errorf("workspace is required for cwd binding")
	}
	cwd, err := ws.Resolve(ctx, path)
	if err != nil {
		return "", err
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return "", fmt.Errorf("cwd: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cwd is not a directory: %s", cwd)
	}
	return cwd, nil
}
