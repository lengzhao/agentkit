package media

import (
	"fmt"
	"strings"
)

// FormatReadImageResult is the read-tool metadata format hydrated before LLM calls.
func FormatReadImageResult(path, mime string, size int64) string {
	if mime == "" {
		mime = DetectMIME(path, nil)
	}
	return fmt.Sprintf("Image: %s\nMIME: %s\nSize: %d bytes", path, mime, size)
}

// FormatReadImageTooLarge explains why a workspace image cannot be viewed.
func FormatReadImageTooLarge(path string, size, max int64) string {
	return fmt.Sprintf("Cannot load image %s: too large (%d bytes; max %d). Ask the user to attach it in chat or use a smaller file.", path, size, max)
}

// ParseReadImagePath extracts a workspace image path from a read-tool result.
func ParseReadImagePath(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Image: ") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, "Image: "))
		if path != "" {
			return path
		}
	}
	return ""
}
