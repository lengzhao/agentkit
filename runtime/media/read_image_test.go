package media_test

import (
	"testing"

	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

func TestParseReadImagePath(t *testing.T) {
	t.Parallel()

	content := rtmedia.FormatReadImageResult("upload/shot.png", "image/png", 128)
	if got := rtmedia.ParseReadImagePath(content); got != "upload/shot.png" {
		t.Fatalf("path = %q", got)
	}
	if got := rtmedia.ParseReadImagePath("Cannot load image upload/big.png: too large"); got != "" {
		t.Fatalf("unexpected path from error: %q", got)
	}
}

func TestIsImage(t *testing.T) {
	t.Parallel()

	if !rtmedia.IsImage("image/png", "") {
		t.Fatal("expected mime match")
	}
	if !rtmedia.IsImage("", "photo.JPG") {
		t.Fatal("expected path match")
	}
	if rtmedia.IsImage("text/plain", "note.txt") {
		t.Fatal("expected non-image")
	}
}
