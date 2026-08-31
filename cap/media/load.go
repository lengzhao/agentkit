package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lengzhao/agentkit/cap/workspace"
)

// DefaultMaxWorkspaceImageBytes caps workspace images loaded for vision.
const DefaultMaxWorkspaceImageBytes = 10 << 20

// LoadWorkspaceImage reads an image from the tenant work tree.
// workRel is relative to work/ (e.g. upload/foo.png).
func LoadWorkspaceImage(ctx context.Context, ws workspace.Service, workRel string, maxBytes int) ([]byte, string, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxWorkspaceImageBytes
	}
	workRel = NormalizeWorkRel(workRel)
	abs, err := ws.Resolve(ctx, filepath.Join("work", workRel))
	if err != nil {
		return nil, "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, "", err
	}
	if info.IsDir() {
		return nil, "", fmt.Errorf("not a file: %s", workRel)
	}
	if info.Size() > int64(maxBytes) {
		return nil, "", nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxBytes {
		return nil, "", nil
	}
	return data, DetectMIME(workRel, data), nil
}
