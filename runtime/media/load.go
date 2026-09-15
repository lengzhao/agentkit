package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lengzhao/agentkit/cap/workspace"
)

// LoadWorkspaceImage reads an image from the tenant work tree for vision models.
// workRel is relative to work/ (e.g. upload/foo.png or work/upload/foo.png).
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
	if info.Size() > int64(DefaultMaxWorkspaceImageReadBytes) {
		return nil, "", nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	if len(data) > DefaultMaxWorkspaceImageReadBytes {
		return nil, "", nil
	}
	if !IsImagePath(workRel) && !LooksLikeImageData(data) {
		return nil, "", nil
	}
	if len(data) > maxBytes {
		return nil, "", nil
	}
	mime := DetectMIME(workRel, data)
	return data, mime, nil
}
