package workspace

import (
	"context"
	"os"
	"path/filepath"

	cw "github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

// WalkLocalTenants invokes fn once per tenant local root. Pinned tenant entries run first;
// remaining subdirectories of localBase are discovered automatically.
func (s *TenantService) WalkLocalTenants(ctx context.Context, fn func(context.Context) error) error {
	seenRoots := map[string]bool{}
	for key := range s.roots {
		tctx := session.WithWorkspace(ctx, key)
		if err := fn(tctx); err != nil {
			return err
		}
	}
	for _, root := range s.roots {
		seenRoots[root] = true
	}
	entries, err := os.ReadDir(s.localBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		root := filepath.Join(s.localBase, ent.Name())
		if seenRoots[root] {
			continue
		}
		key := session.WorkspaceKeyFromLocalDir(ent.Name(), s.omitPlatformPrefix)
		tctx := session.WithWorkspace(ctx, key)
		if err := fn(tctx); err != nil {
			return err
		}
	}
	return nil
}

var _ cw.LocalTenantWalker = (*TenantService)(nil)
