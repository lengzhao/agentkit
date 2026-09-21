package fs

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit/cap/filesystem"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

func readImageToolResult(ctx context.Context, store filesystem.Service, path, display string) (string, error) {
	info, err := store.Stat(ctx, path)
	if err != nil {
		return "", err
	}
	if info.IsDir {
		return "", fmt.Errorf("not a file: %s", path)
	}
	if info.Size > int64(rtmedia.DefaultMaxWorkspaceImageReadBytes) {
		return rtmedia.FormatReadImageTooLarge(display, info.Size, int64(rtmedia.DefaultMaxWorkspaceImageReadBytes)), nil
	}
	data, err := store.Read(ctx, path)
	if err != nil {
		return "", err
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	return rtmedia.FormatReadImageResult(display, rtmedia.DetectMIME(path, head), info.Size), nil
}
