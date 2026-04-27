// Package clipboard wraps golang.design/x/clipboard to read binary image
// payloads from the system clipboard. The upstream library always returns PNG
// bytes for FmtImage, so ReadImageFromClipboard reports media type
// "image/png".
package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"sync"

	xclip "golang.design/x/clipboard"
)

var (
	initOnce sync.Once
	initErr  error
)

// ErrNoImage is returned when the clipboard does not currently contain image
// data.
var ErrNoImage = errors.New("no image data on clipboard")

// allowedImageMIME is the set of mime types accepted by ValidateImageMIME.
var allowedImageMIME = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/jpg":  {},
	"image/gif":  {},
	"image/webp": {},
}

// ValidateImageMIME returns nil when mimeType names a supported image format.
func ValidateImageMIME(mimeType string) error {
	if _, ok := allowedImageMIME[strings.ToLower(mimeType)]; !ok {
		return fmt.Errorf("unsupported image type: %s", mimeType)
	}
	return nil
}

// DecodeImage decodes the bytes into a stdimage.Image, returning an error if
// the data is not a recognised image format.
func DecodeImage(data []byte) (stdimage.Image, error) {
	img, _, err := stdimage.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

// ReadImageFromClipboard returns the raw bytes and media type of an image on
// the system clipboard. The upstream library always emits PNG so the media
// type is hardcoded to "image/png".
func ReadImageFromClipboard() ([]byte, string, error) {
	if err := ensureInit(); err != nil {
		return nil, "", err
	}
	data := xclip.Read(xclip.FmtImage)
	if len(data) == 0 {
		return nil, "", ErrNoImage
	}
	return data, "image/png", nil
}

func ensureInit() error {
	initOnce.Do(func() {
		initErr = xclip.Init()
	})
	return initErr
}
