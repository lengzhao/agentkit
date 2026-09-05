package fs

import (
	"fmt"
	"os"

	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

func readImageToolResult(abs, path string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("not a file: %s", path)
	}
	if info.Size() > rtmedia.DefaultMaxWorkspaceImageBytes {
		return rtmedia.FormatReadImageTooLarge(path, info.Size(), rtmedia.DefaultMaxWorkspaceImageBytes), nil
	}
	return rtmedia.FormatReadImageResult(path, rtmedia.DetectMIME(path, nil), info.Size()), nil
}
