package feishu

import (
	"strings"
	"testing"
)

func TestDetectMimeTypeJSONNotImage(t *testing.T) {
	t.Parallel()
	got := detectMimeType([]byte(`{"observations":[]}`))
	if strings.HasPrefix(got, "image/") {
		t.Fatalf("mime = %q, want non-image", got)
	}
}

func TestDetectMimeTypePNG(t *testing.T) {
	t.Parallel()
	got := detectMimeType([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	if got != "image/png" {
		t.Fatalf("mime = %q", got)
	}
}
