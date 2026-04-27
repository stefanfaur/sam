package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	"github.com/disintegration/imaging"
)

const MaxLongEdge = 1568

// ResizeImage decodes data, downscales when the long edge exceeds MaxLongEdge
// using Lanczos resampling, and re-encodes in the requested mime format.
// If the image is already within bounds the input bytes are returned unchanged.
func ResizeImage(data []byte, mimeType string) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	w, h := cfg.Width, cfg.Height
	maxDim := w
	if h > maxDim {
		maxDim = h
	}
	if maxDim <= MaxLongEdge {
		return data, nil
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	scale := float64(MaxLongEdge) / float64(maxDim)
	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)
	resized := imaging.Resize(src, newW, newH, imaging.Lanczos)

	return encode(resized, mimeType)
}

// SelectFormat chooses the best output mime for an image: PNG when the image
// has alpha or looks like a screenshot/diagram, otherwise JPEG.
func SelectFormat(img image.Image) string {
	if hasAlphaChannel(img) {
		return "image/png"
	}
	if isLikelyScreenshot(img) {
		return "image/png"
	}
	return "image/jpeg"
}

// StripMetadata re-encodes the image into mimeType, dropping EXIF/ICC and any
// other markers that aren't part of the pixel data.
func StripMetadata(data []byte, mimeType string) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return encode(src, mimeType)
}

func encode(img image.Image, mimeType string) ([]byte, error) {
	var buf bytes.Buffer
	switch mimeType {
	case "image/png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
	case "image/jpeg", "image/jpg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, err
		}
	default:
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func hasAlphaChannel(img image.Image) bool {
	switch m := img.(type) {
	case *image.NRGBA, *image.RGBA, *image.NRGBA64, *image.RGBA64:
		bounds := img.Bounds()
		// Sample corners and centre; if any pixel has alpha < 255, treat as transparent.
		points := [][2]int{
			{bounds.Min.X, bounds.Min.Y},
			{bounds.Max.X - 1, bounds.Min.Y},
			{bounds.Min.X, bounds.Max.Y - 1},
			{bounds.Max.X - 1, bounds.Max.Y - 1},
			{(bounds.Min.X + bounds.Max.X) / 2, (bounds.Min.Y + bounds.Max.Y) / 2},
		}
		for _, p := range points {
			_, _, _, a := m.At(p[0], p[1]).RGBA()
			if a < 0xFFFF {
				return true
			}
		}
		return false
	default:
		_ = color.Alpha{}
		return false
	}
}

// isLikelyScreenshot is a cheap heuristic: sample up to 100 pixels and treat
// images with very few distinct colors as diagrams/UI screenshots, where PNG
// preserves crisp edges better than JPEG.
func isLikelyScreenshot(img image.Image) bool {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w == 0 || h == 0 {
		return false
	}

	step := (w * h) / 100
	if step == 0 {
		step = 1
	}

	colors := make(map[uint32]struct{})
	for i := 0; i < w*h; i += step {
		x := bounds.Min.X + (i % w)
		y := bounds.Min.Y + (i / w)
		r, g, b, _ := img.At(x, y).RGBA()
		col := ((r >> 8) << 16) | ((g >> 8) << 8) | (b >> 8)
		colors[col] = struct{}{}
	}
	return len(colors) < 16
}
