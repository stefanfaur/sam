package clipboard

import (
	"bytes"
	"image/color"
	stdimage "image"
	"image/png"
	"testing"
)

func TestValidateImageMIME(t *testing.T) {
	cases := []struct {
		mime  string
		valid bool
	}{
		{"image/png", true},
		{"IMAGE/PNG", true},
		{"image/jpeg", true},
		{"image/jpg", true},
		{"image/gif", true},
		{"image/webp", true},
		{"text/plain", false},
		{"application/json", false},
		{"", false},
	}
	for _, tc := range cases {
		err := ValidateImageMIME(tc.mime)
		if (err == nil) != tc.valid {
			t.Errorf("ValidateImageMIME(%q): err=%v, want valid=%v", tc.mime, err, tc.valid)
		}
	}
}

func TestDecodeImagePNG(t *testing.T) {
	src := stdimage.NewNRGBA(stdimage.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 50, G: 100, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeImage(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if got == nil {
		t.Fatal("DecodeImage returned nil image")
	}
	if got.Bounds().Dx() != 8 || got.Bounds().Dy() != 8 {
		t.Errorf("dims: got %v, want 8x8", got.Bounds())
	}
}

func TestDecodeImageInvalid(t *testing.T) {
	if _, err := DecodeImage([]byte("not an image")); err == nil {
		t.Error("expected error decoding garbage bytes")
	}
}
