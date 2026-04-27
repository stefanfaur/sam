package image

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func solidRGBA(w, h int, c color.NRGBA) *stdimage.NRGBA {
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func gradientRGBA(w, h int) *stdimage.NRGBA {
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x * 255) / w),
				G: uint8((y * 255) / h),
				B: uint8(((x + y) * 255) / (w + h)),
				A: 255,
			})
		}
	}
	return img
}

func encodePNG(t *testing.T, img stdimage.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img stdimage.Image, q int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestResizeDownscalesLongEdgeAbove1568(t *testing.T) {
	img := gradientRGBA(2000, 1000)
	data := encodePNG(t, img)

	out, err := ResizeImage(data, "image/png")
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	cfg, _, err := stdimage.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if cfg.Width != 1568 || cfg.Height != 784 {
		t.Errorf("expected 1568x784, got %dx%d", cfg.Width, cfg.Height)
	}
}

func TestResizeNoOpUnderThreshold(t *testing.T) {
	img := gradientRGBA(1200, 800)
	data := encodePNG(t, img)

	out, err := ResizeImage(data, "image/png")
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}
	if !bytes.Equal(out, data) {
		t.Errorf("under-threshold image was modified (got %d bytes, want %d)", len(out), len(data))
	}
}

func TestResizeTallImage(t *testing.T) {
	img := gradientRGBA(800, 3136)
	data := encodePNG(t, img)

	out, err := ResizeImage(data, "image/png")
	if err != nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}
	cfg, _, err := stdimage.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if cfg.Height != 1568 {
		t.Errorf("height: got %d, want 1568", cfg.Height)
	}
	if cfg.Width != 400 {
		t.Errorf("width: got %d, want 400", cfg.Width)
	}
}

func TestSelectFormatWithAlpha(t *testing.T) {
	img := solidRGBA(64, 64, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	if got := SelectFormat(img); got != "image/png" {
		t.Errorf("alpha image: got %s, want image/png", got)
	}
}

func TestSelectFormatOpaqueGradient(t *testing.T) {
	img := gradientRGBA(256, 256)
	if got := SelectFormat(img); got != "image/jpeg" {
		t.Errorf("opaque gradient: got %s, want image/jpeg", got)
	}
}

func TestSelectFormatScreenshotLikeDiagram(t *testing.T) {
	// Solid two-color image: very low color count -> screenshot heuristic.
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			if (x/16+y/16)%2 == 0 {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 255})
			}
		}
	}
	if got := SelectFormat(img); got != "image/png" {
		t.Errorf("screenshot-like diagram: got %s, want image/png", got)
	}
}

func TestStripMetadataReencodes(t *testing.T) {
	img := gradientRGBA(128, 128)
	data := encodeJPEG(t, img, 95)

	out, err := StripMetadata(data, "image/jpeg")
	if err != nil {
		t.Fatalf("StripMetadata failed: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("output not decodable: %v", err)
	}
}
