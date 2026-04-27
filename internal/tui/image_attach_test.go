package tui

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/png"
	"testing"
)

func TestEstimateTokensAnthropic(t *testing.T) {
	got := estimateTokens(1000, 800, "anthropic")
	want := (1000 * 800) / 750
	if got != want {
		t.Errorf("anthropic: got %d, want %d", got, want)
	}
	if estimateTokens(1500, 1000, "claude-sonnet") <= 0 {
		t.Error("claude-sonnet token estimate <= 0")
	}
}

func TestEstimateTokensGPT4o(t *testing.T) {
	got := estimateTokens(1024, 1024, "gpt-4o")
	want := 4*170 + 85
	if got != want {
		t.Errorf("gpt-4o tile-based: got %d, want %d", got, want)
	}
}

func TestEstimateTokensUnknownProvider(t *testing.T) {
	if got := estimateTokens(1000, 800, "weird-vendor"); got != -1 {
		t.Errorf("unknown provider: got %d, want -1", got)
	}
}

func TestFormatTokenEstimate(t *testing.T) {
	cases := []struct {
		tokens int
		want   string
	}{
		{0, "0 tok"},
		{256, "256 tok"},
		{-1, "≈ unknown"},
	}
	for _, tc := range cases {
		if got := formatTokenEstimate(tc.tokens); got != tc.want {
			t.Errorf("formatTokenEstimate(%d): got %q, want %q", tc.tokens, got, tc.want)
		}
	}
}

func TestAttachmentBadgeEmpty(t *testing.T) {
	if got := attachmentBadge(nil, "anthropic"); got != "" {
		t.Errorf("empty: got %q", got)
	}
}

func TestAttachmentBadgeSingle(t *testing.T) {
	atts := []imageAttachment{
		{data: bytes.Repeat([]byte{0}, 234*1024), mediaType: "image/png", width: 1568, height: 880, tokens: 1840},
	}
	got := attachmentBadge(atts, "anthropic")
	if got == "" {
		t.Fatal("expected non-empty badge")
	}
	for _, want := range []string{"1 image", "1840", "anthropic", "1568x880", "234KB"} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("badge missing %q: %q", want, got)
		}
	}
}

func TestAttachmentBadgeUnknownTokens(t *testing.T) {
	atts := []imageAttachment{
		{data: []byte("x"), mediaType: "image/png", width: 100, height: 100, tokens: -1},
	}
	got := attachmentBadge(atts, "weird-vendor")
	if !contains(got, "≈ unknown") {
		t.Errorf("badge missing unknown marker: %q", got)
	}
}

func TestPreprocessImagePNGRoundTrip(t *testing.T) {
	src := stdimage.NewNRGBA(stdimage.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := preprocessImage(buf.Bytes(), "image/png")
	if err != nil {
		t.Fatalf("preprocessImage: %v", err)
	}
	if out.width != 64 || out.height != 64 {
		t.Errorf("dims: got %dx%d, want 64x64", out.width, out.height)
	}
	if out.mediaType == "" {
		t.Error("empty mediaType")
	}
}

