package media

import (
	"context"
	"fmt"
	"os"

	"github.com/lengzhao/agentkit/cap/workspace"
)

// LoadWorkspaceImage reads a workspace image and tries FitForVision before LLM vision input.
// When fitting fails, the original file bytes are returned (up to DefaultMaxWorkspaceImageReadBytes).
// workRel is relative to work/ (e.g. upload/foo.png or work/upload/foo.png).
// maxPayloadBytes caps the returned payload after fitting; 0 uses DefaultMaxVisionPayloadBytes.
// Files larger than DefaultMaxWorkspaceImageReadBytes are skipped (empty result).
func LoadWorkspaceImage(ctx context.Context, ws workspace.Service, workRel string, maxPayloadBytes int) ([]byte, string, error) {
	data, mime, err := loadWorkspaceImageRaw(ctx, ws, workRel)
	if err != nil || len(data) == 0 {
		return data, mime, err
	}
	opt := DefaultVisionFitOptions()
	if maxPayloadBytes > 0 {
		opt.MaxBytes = maxPayloadBytes
	}
	return FitForVision(data, mime, opt)
}

func loadWorkspaceImageRaw(ctx context.Context, ws workspace.Service, workRel string) ([]byte, string, error) {
	abs, err := resolveFileAbs(ctx, ws, workRel)
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
	mime := DetectMIME(workRel, data)
	return data, mime, nil
}
