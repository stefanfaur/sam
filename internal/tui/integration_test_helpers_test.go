package tui

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/png"
	"os"
)

// minimalPNG returns a tiny valid PNG payload sufficient for the preprocess
// pipeline to decode + re-encode.
func minimalPNG() []byte {
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// writeFile is a tiny os.WriteFile wrapper kept here so integration_test.go
// stays focused on flow assertions.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
