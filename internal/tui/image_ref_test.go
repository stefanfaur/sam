package tui

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseImageSyntax(t *testing.T) {
	cases := []struct {
		in       string
		wantPath string
		wantOK   bool
	}{
		{"@image:/abs/file.png", "/abs/file.png", true},
		{"@image:./rel/img.jpg", "./rel/img.jpg", true},
		{"@image: /padded.png", "/padded.png", true},
		{"@image:", "", false},
		{"@file:foo.go", "", false},
		{"plain text", "", false},
	}
	for _, tc := range cases {
		p, ok := parseImageSyntax(tc.in)
		if ok != tc.wantOK {
			t.Errorf("parseImageSyntax(%q): ok=%v, want %v", tc.in, ok, tc.wantOK)
		}
		if ok && p != tc.wantPath {
			t.Errorf("parseImageSyntax(%q): path=%q, want %q", tc.in, p, tc.wantPath)
		}
	}
}

func TestExtractImageRefsRemovesTokens(t *testing.T) {
	text := "look @image:/a.png at this @image:./b.jpg please"
	clean, paths := extractImageRefs(text)
	if clean != "look at this please" {
		t.Errorf("clean: %q", clean)
	}
	if !reflect.DeepEqual(paths, []string{"/a.png", "./b.jpg"}) {
		t.Errorf("paths: %v", paths)
	}
}

func TestExtractImageRefsNoMatches(t *testing.T) {
	text := "no references here"
	clean, paths := extractImageRefs(text)
	if clean != text {
		t.Errorf("text mutated: %q", clean)
	}
	if len(paths) != 0 {
		t.Errorf("paths: %v", paths)
	}
}

func TestExtractImageRefsMultiline(t *testing.T) {
	text := "first line\nsecond @image:/x.png line"
	clean, paths := extractImageRefs(text)
	if clean != "first line\nsecond line" {
		t.Errorf("clean: %q", clean)
	}
	if len(paths) != 1 || paths[0] != "/x.png" {
		t.Errorf("paths: %v", paths)
	}
}

func TestLoadImageFromFilePNG(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.png")
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		img.SetNRGBA(x, x, color.NRGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, mt, err := loadImageFromFile(p)
	if err != nil {
		t.Fatalf("loadImageFromFile: %v", err)
	}
	if mt != "image/png" {
		t.Errorf("mime: %q", mt)
	}
	if !bytes.Equal(data, buf.Bytes()) {
		t.Error("data mismatch")
	}
}

func TestLoadImageFromFileUnsupportedExt(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.bmp")
	if err := os.WriteFile(p, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := loadImageFromFile(p); err == nil {
		t.Error("expected error for .bmp")
	}
}
