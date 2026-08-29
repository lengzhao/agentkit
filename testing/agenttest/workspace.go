package agenttest

import (
	"context"
	"os"

	"github.com/lengzhao/agentkit/cap/workspace"
)

// DirWorkspace maps scoped rel paths (e.g. local:agents) to directories.
type DirWorkspace map[string]string

func (w DirWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	if dir, ok := w[rel]; ok {
		return dir, nil
	}
	return "", os.ErrNotExist
}

var _ workspace.Service = DirWorkspace{}
