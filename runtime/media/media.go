package media

import (
	"encoding/base64"
	"path/filepath"
	"strings"

	capmedia "github.com/lengzhao/agentkit/cap/media"
)

// ContentTypeAttachmentRef is persisted in session history for stripped attachments.
const ContentTypeAttachmentRef = capmedia.ContentTypeAttachmentRef

// IsImage reports whether a file looks like an image from MIME type and/or path.
func IsImage(mimeType, path string) bool {
	if IsImageMIME(mimeType) {
		return true
	}
	return IsImagePath(path)
}

// IsImagePath reports whether a workspace-relative path looks like an image file.
func IsImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".heic", ".heif":
		return true
	default:
		return false
	}
}

// IsImageMIME reports whether a MIME type is an image.
func IsImageMIME(mime string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(mime)), "image/")
}

// DataURL builds a data URL for vision models.
func DataURL(mime string, data []byte) string {
	if mime == "" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// DetectMIME guesses image MIME type from path and magic bytes.
func DetectMIME(path string, data []byte) string {
	if ext := filepath.Ext(path); ext != "" {
		if mime := mimeByExt(ext); mime != "" {
			return mime
		}
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png"
	}
	if len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a") {
		return "image/gif"
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	return "image/png"
}

// NormalizeWorkRel strips leading slashes and an optional work/ prefix.
func NormalizeWorkRel(rel string) string {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.TrimPrefix(rel, "/")
	return strings.TrimPrefix(rel, "work/")
}

func mimeByExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".heic":
		return "image/heic"
	case ".heif":
		return "image/heif"
	default:
		return ""
	}
}
