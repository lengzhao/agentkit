package sessstore

import (
	"context"
	"fmt"
	"os"

	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/workspace/workpath"
)

// ensureTenantLayout resolves the sessions dir and the tenant work subtree
// (via workspace.Layout) so session-adjacent tooling has a place to write.
func ensureTenantLayout(ctx context.Context, ws cw.Service, relSessionsDir string) (string, error) {
	sessionsDir, err := ws.Resolve(ctx, relSessionsDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		return "", err
	}
	workRel, _ := workpath.WorkLayout(ws)
	if workRel != "" {
		workDir, err := ws.Resolve(ctx, workRel)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir work dir: %w", err)
		}
	}
	return sessionsDir, nil
}
