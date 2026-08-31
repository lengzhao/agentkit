package fs

import (
	"fmt"
	"os"

	"github.com/lengzhao/agentkit/cap/media"
)

func readImageToolResult(abs, path string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("not a file: %s", path)
	}
	if info.Size() > media.DefaultMaxWorkspaceImageBytes {
		return media.FormatReadImageTooLarge(path, info.Size(), media.DefaultMaxWorkspaceImageBytes), nil
	}
	return media.FormatReadImageResult(path, media.DetectMIME(path, nil), info.Size()), nil
}
