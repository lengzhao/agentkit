package media_test

import (
	"testing"

	"github.com/lengzhao/agentkit/cap/media"
)

func TestParseReadImagePath(t *testing.T) {
	t.Parallel()

	content := media.FormatReadImageResult("upload/shot.png", "image/png", 128)
	if got := media.ParseReadImagePath(content); got != "upload/shot.png" {
		t.Fatalf("path = %q", got)
	}
	if got := media.ParseReadImagePath("Cannot load image upload/big.png: too large"); got != "" {
		t.Fatalf("unexpected path from error: %q", got)
	}
}

func TestIsImage(t *testing.T) {
	t.Parallel()

	if !media.IsImage("image/png", "") {
		t.Fatal("expected mime match")
	}
	if !media.IsImage("", "photo.JPG") {
		t.Fatal("expected path match")
	}
	if media.IsImage("text/plain", "note.txt") {
		t.Fatal("expected non-image")
	}
}
