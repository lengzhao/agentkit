package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	cw "github.com/lengzhao/agentkit/cap/workspace"
)

// CopyLocalToGlobal copies the file at rel (local-scoped or bare local-relative) into the
// global workspace at the same relative path (local:api/foo.yaml → global:api/foo.yaml).
// Already-global paths are returned unchanged.
func CopyLocalToGlobal(ctx context.Context, ws cw.Service, rel string) (string, error) {
	localRel, globalRel, unchanged := localToGlobalRels(rel)
	if unchanged {
		return strings.TrimSpace(rel), nil
	}

	src, err := ws.Resolve(ctx, localRel)
	if err != nil {
		return "", err
	}
	dst, err := ws.Resolve(ctx, globalRel)
	if err != nil {
		return "", err
	}
	if filepath.Clean(src) == filepath.Clean(dst) {
		return globalRel, nil
	}

	if err := copyRegularFile(src, dst); err != nil {
		return "", fmt.Errorf("copy %s to global: %w", rel, err)
	}
	return globalRel, nil
}

// localToGlobalRels maps a workspace path to local/global scoped pairs.
// unchanged is true when rel is empty or already global-scoped.
func localToGlobalRels(rel string) (localRel, globalRel string, unchanged bool) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", "", true
	}
	scope, path, scoped := ParseScoped(rel)
	if scoped && scope == cw.ScopeGlobal {
		return "", rel, true
	}
	if scoped && scope != cw.ScopeLocal {
		return "", rel, true
	}
	if !scoped {
		localRel = cw.ScopeLocal + ":" + rel
		path = rel
	} else {
		localRel = rel
	}
	return localRel, cw.ScopeGlobal + ":" + path, false
}

func copyRegularFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", src)
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
