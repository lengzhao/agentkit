package media

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	_ "image/png"
	"strings"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// DefaultMaxVisionPayloadBytes is used when FitForVision is enabled (optional).
const DefaultMaxVisionPayloadBytes = 1 << 20

// DefaultMaxVisionEdgePixels limits the longest image edge when FitForVision is enabled.
const DefaultMaxVisionEdgePixels = 2048

// DefaultMaxWorkspaceImageReadBytes is the largest workspace image file tools and hydrate may load.
const DefaultMaxWorkspaceImageReadBytes = 10 << 20

// DefaultMaxWorkspaceImageBytes caps raw image bytes sent to vision models.
const DefaultMaxWorkspaceImageBytes = 10 << 20

// VisionFitOptions controls downscaling and re-encoding for LLM vision input.
type VisionFitOptions struct {
	MaxBytes int
	MaxEdge  int
}

// DefaultVisionFitOptions returns the standard vision payload limits.
func DefaultVisionFitOptions() VisionFitOptions {
	return VisionFitOptions{
		MaxBytes: DefaultMaxVisionPayloadBytes,
		MaxEdge:  DefaultMaxVisionEdgePixels,
	}
}

// FitForVision downscales and re-encodes images that exceed vision limits.
// When the input is already within limits, it is returned unchanged.
// If decoding or re-encoding fails, the original bytes are returned unchanged.
func FitForVision(data []byte, mime string, opt VisionFitOptions) ([]byte, string, error) {
	if len(data) == 0 {
		return data, mime, nil
	}
	origMIME := mime
	if opt.MaxBytes <= 0 {
		opt.MaxBytes = DefaultMaxVisionPayloadBytes
	}
	if opt.MaxEdge <= 0 {
		opt.MaxEdge = DefaultMaxVisionEdgePixels
	}

	img, _, err := decodeImage(data, mime)
	if err != nil {
		return visionOriginalFallback(data, origMIME)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	longest := w
	if h > longest {
		longest = h
	}
	withinEdge := longest <= opt.MaxEdge
	withinBytes := len(data) <= opt.MaxBytes
	if withinEdge && withinBytes {
		if mime == "" {
			mime = DetectMIME("", data)
		}
		return data, mime, nil
	}

	scaled := img
	if !withinEdge {
		scale := float64(opt.MaxEdge) / float64(longest)
		nw := max(1, int(float64(w)*scale))
		nh := max(1, int(float64(h)*scale))
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
		scaled = dst
	}

	out, outMIME, err := encodeUnderByteLimit(scaled, opt.MaxBytes)
	if err != nil {
		return visionOriginalFallback(data, origMIME)
	}
	return out, outMIME, nil
}

func visionOriginalFallback(data []byte, mime string) ([]byte, string, error) {
	if mime == "" {
		mime = DetectMIME("", data)
	}
	return data, mime, nil
}

func decodeImage(data []byte, mime string) (image.Image, string, error) {
	mime = stringsTrimImageMIME(mime, data)
	switch mime {
	case "image/gif":
		g, err := gif.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, mime, err
		}
		return g, mime, nil
	default:
		img, format, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, mime, err
		}
		if mime == "" || mime == "application/octet-stream" {
			mime = mimeFromImageFormat(format)
		}
		return img, mime, nil
	}
}

func stringsTrimImageMIME(mime string, data []byte) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if mime != "" && mime != "application/octet-stream" {
		return mime
	}
	return DetectMIME("", data)
}

func mimeFromImageFormat(format string) string {
	switch format {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "bmp":
		return "image/bmp"
	default:
		return "image/jpeg"
	}
}

func encodeUnderByteLimit(img image.Image, maxBytes int) ([]byte, string, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxVisionPayloadBytes
	}
	qualities := []int{85, 75, 65, 55, 45, 35, 25}
	for _, q := range qualities {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return nil, "", err
		}
		if buf.Len() <= maxBytes {
			return buf.Bytes(), "image/jpeg", nil
		}
	}
	// Last resort: shrink dimensions and retry once.
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 1 && h <= 1 {
		return nil, "", fmt.Errorf("vision image still exceeds %d bytes after compression", maxBytes)
	}
	nw := max(1, w*3/4)
	nh := max(1, h*3/4)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	for _, q := range qualities {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: q}); err != nil {
			return nil, "", err
		}
		if buf.Len() <= maxBytes {
			return buf.Bytes(), "image/jpeg", nil
		}
	}
	return nil, "", fmt.Errorf("vision image still exceeds %d bytes after compression", maxBytes)
}
