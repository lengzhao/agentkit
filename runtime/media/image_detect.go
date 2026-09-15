package media

import (
	"context"
	"os"
	"strings"

	"github.com/lengzhao/agentkit/cap/workspace"
)

const fileHeadPeekSize = 512

// LooksLikeImageData reports whether bytes begin with a known image signature.
func LooksLikeImageData(data []byte) bool {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return true
	}
	if len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return true
	}
	if len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a") {
		return true
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return true
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		return true
	}
	return false
}

// ExtensionForMIME maps an image MIME type to a file extension including the dot.
func ExtensionForMIME(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/heic":
		return ".heic"
	case "image/heif":
		return ".heif"
	default:
		return ""
	}
}

// EnsureInboundFileName adds a standard image extension when MIME or content indicates an image.
func EnsureInboundFileName(name, mime string, data []byte) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	if IsImagePath(name) {
		return name
	}
	if !IsImageMIME(mime) && !LooksLikeImageData(data) {
		return name
	}
	ext := ExtensionForMIME(mime)
	if ext == "" && len(data) > 0 {
		ext = ExtensionForMIME(DetectMIME(name, data))
	}
	if ext == "" {
		ext = ".jpg"
	}
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ext) {
		return name
	}
	return name + ext
}

// ReadFileHead reads up to n bytes from a file for content sniffing.
func ReadFileHead(path string, n int) ([]byte, error) {
	if n <= 0 {
		n = fileHeadPeekSize
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	read, err := f.Read(buf)
	if err != nil && read == 0 {
		return nil, err
	}
	return buf[:read], nil
}

// WorkspaceFileMayBeImage reports whether a work-relative path should be hydrated as vision.
func WorkspaceFileMayBeImage(ctx context.Context, ws workspace.Service, workRel string) (bool, error) {
	workRel = NormalizeWorkRel(ws, workRel)
	if workRel == "" {
		return false, nil
	}
	if IsImagePath(workRel) {
		return true, nil
	}
	if ws == nil {
		return false, nil
	}
	abs, err := resolveFileAbs(ctx, ws, workRel)
	if err != nil {
		return false, err
	}
	head, err := ReadFileHead(abs, fileHeadPeekSize)
	if err != nil {
		return false, err
	}
	return LooksLikeImageData(head), nil
}
