package fs

import (
	"fmt"
	"os"

	"github.com/lengzhao/agentkit/cap/media"
)

const maxInlineImageBytes = 4 << 20

func readImageToolResult(abs, path string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("not a file: %s", path)
	}
	if info.Size() > maxInlineImageBytes {
		return fmt.Sprintf("Image %s is too large to inline (%d bytes; max %d). Ask the user to attach it in chat or use a smaller file.", path, info.Size(), maxInlineImageBytes), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return formatImageToolResult(path, data)
}

func formatImageToolResult(path string, data []byte) (string, error) {
	mime := media.DetectMIME(path, data)
	return fmt.Sprintf("Image file: %s\nMIME: %s\nSize: %d bytes\nData: %s", path, mime, len(data), media.DataURL(mime, data)), nil
}
