package openapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// copyLocalToGlobal copies the file at rel (local-scoped or bare local-relative) into the
// global workspace at the same relative path, then returns the global file's absolute path.
func copyLocalToGlobal(ctx context.Context, fs filesystem.Service, ws workspace.Service, rel string) (string, error) {
	localRel, globalRel, unchanged := localToGlobalRels(rel)
	if unchanged {
		return storeAbsPath(ctx, ws, rel)
	}

	data, err := fs.Read(ctx, localRel)
	if err != nil {
		return "", fmt.Errorf("copy %s to global: %w", rel, err)
	}
	if err := fs.Write(ctx, globalRel, data); err != nil {
		return "", fmt.Errorf("copy %s to global: %w", rel, err)
	}
	return storeAbsPath(ctx, ws, globalRel)
}

// localToGlobalRels maps a workspace path to local/global scoped pairs.
// unchanged is true when rel is empty or already global-scoped.
func localToGlobalRels(rel string) (localRel, globalRel string, unchanged bool) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", "", true
	}
	scope, path, scoped := workspace.ParseScoped(rel)
	if scoped && scope == workspace.ScopeGlobal {
		return "", rel, true
	}
	if scoped && scope != workspace.ScopeLocal {
		return "", rel, true
	}
	if !scoped {
		localRel = workspace.ScopeLocal + ":" + rel
		path = rel
	} else {
		localRel = rel
	}
	return localRel, workspace.ScopeGlobal + ":" + path, false
}
