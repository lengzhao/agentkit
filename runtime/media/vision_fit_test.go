package media_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

func TestFitForVisionLeavesSmallImageUnchanged(t *testing.T) {
	t.Parallel()

	data := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	out, mime, err := rtmedia.FitForVision(data, "image/png", rtmedia.DefaultVisionFitOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, data) || mime != "image/png" {
		t.Fatalf("got mime=%q len=%d", mime, len(out))
	}
}

func TestFitForVisionDownscalesAndCompresses(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 3200, 2400))
	for y := 0; y < 2400; y++ {
		for x := 0; x < 3200; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}

	out, mime, err := rtmedia.FitForVision(raw.Bytes(), "image/jpeg", rtmedia.DefaultVisionFitOptions())
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/jpeg" {
		t.Fatalf("mime = %q", mime)
	}
	if len(out) > rtmedia.DefaultMaxVisionPayloadBytes {
		t.Fatalf("payload %d exceeds %d", len(out), rtmedia.DefaultMaxVisionPayloadBytes)
	}
	decoded, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	b := decoded.Bounds()
	longest := b.Dx()
	if b.Dy() > longest {
		longest = b.Dy()
	}
	if longest > rtmedia.DefaultMaxVisionEdgePixels {
		t.Fatalf("longest edge %d > %d", longest, rtmedia.DefaultMaxVisionEdgePixels)
	}
}
