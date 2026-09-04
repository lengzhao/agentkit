package session

import (
	"context"
	"fmt"
	"os"

	"github.com/lengzhao/agentkit/cap/workspace"
)

// TenantToolWorkDir is the workspace-relative directory for shell cwd and temp artifacts.
const TenantToolWorkDir = "work"

func ensureTenantLayout(ctx context.Context, ws workspace.Service, relSessionsDir string) (string, error) {
	sessionsDir, err := ws.Resolve(ctx, relSessionsDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return "", err
	}
	workDir, err := ws.Resolve(ctx, TenantToolWorkDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir work dir: %w", err)
	}
	return sessionsDir, nil
}
