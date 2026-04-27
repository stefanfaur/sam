package tui

import (
	"bytes"
	"fmt"
	stdimage "image"
	"strings"

	imgpkg "github.com/stefanfaur/sam/internal/image"
)

// imageAttachment holds a processed image queued for the next user turn.
// data is the raw (non-base64) bytes; base64 encoding happens at submission
// time when constructing the llm.ContentImage block.
type imageAttachment struct {
	data      []byte
	mediaType string
	width     int
	height    int
	tokens    int // -1 means provider has no published formula
}

// estimateTokens returns the token cost of an image of given pixel size for
// a provider, or -1 when the provider has no published formula.
func estimateTokens(width, height int, provider string) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	p := strings.ToLower(provider)
	switch {
	case strings.Contains(p, "anthropic"), strings.Contains(p, "claude"), strings.Contains(p, "deepseek"):
		return (width * height) / 750
	case strings.Contains(p, "gpt-4o"), strings.Contains(p, "openai"), strings.Contains(p, "gpt"):
		// OpenAI tile-based: 170 tokens per 512x512 tile + 85 base.
		// Approximate as ceil(w/512) * ceil(h/512) * 170 + 85.
		tilesW := (width + 511) / 512
		tilesH := (height + 511) / 512
		return tilesW*tilesH*170 + 85
	default:
		return -1
	}
}

// formatTokenEstimate renders a token count for the badge. -1 surfaces as
// "≈ unknown" so the user knows the provider cost wasn't computed.
func formatTokenEstimate(tokens int) string {
	if tokens < 0 {
		return "≈ unknown"
	}
	return fmt.Sprintf("%d tok", tokens)
}

// formatBytes returns a 2-3 char human size for the badge.
func formatBytes(b int) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%dKB", b/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
	}
}

// attachmentBadge renders a one-line summary above the input area. Empty
// string when no attachments are pending.
func attachmentBadge(atts []imageAttachment, provider string) string {
	if len(atts) == 0 {
		return ""
	}
	totalTokens := 0
	totalSize := 0
	dims := make([]string, 0, len(atts))
	unknown := false
	for _, a := range atts {
		if a.tokens < 0 {
			unknown = true
		} else {
			totalTokens += a.tokens
		}
		totalSize += len(a.data)
		dims = append(dims, fmt.Sprintf("%dx%d", a.width, a.height))
	}
	word := "image"
	if len(atts) > 1 {
		word = "images"
	}
	tokenStr := formatTokenEstimate(totalTokens)
	if unknown {
		tokenStr = "≈ unknown"
	}
	provLabel := provider
	if provLabel == "" {
		provLabel = "?"
	}
	return fmt.Sprintf("📎 %d %s · %s (%s) · %s · %s",
		len(atts), word, tokenStr, provLabel, strings.Join(dims, ", "), formatBytes(totalSize))
}

// processedImage carries the raw bytes plus metadata produced by preprocessImage.
type processedImage struct {
	data      []byte
	mediaType string
	width     int
	height    int
}

// preprocessImage decodes the input bytes, picks an output format, downscales
// when the long edge exceeds the threshold, strips metadata via re-encode,
// and returns the final bytes plus dimensions/mime. It is provider-agnostic.
func preprocessImage(data []byte, mediaType string) (processedImage, error) {
	if mediaType == "" {
		mediaType = "image/png"
	}

	// Resize first; resize either returns the original bytes (no-op) or a
	// fresh re-encode in mediaType.
	resized, err := imgpkg.ResizeImage(data, mediaType)
	if err != nil {
		return processedImage{}, err
	}

	// Decode for SelectFormat / strip metadata.
	src, _, err := stdimage.Decode(bytes.NewReader(resized))
	if err != nil {
		return processedImage{}, fmt.Errorf("decode: %w", err)
	}

	// Pick output format based on alpha/screenshot heuristic when the input
	// is PNG (where re-encoding to JPEG would shrink the payload). For JPEG
	// inputs we keep JPEG.
	out := mediaType
	if mediaType == "image/png" {
		out = imgpkg.SelectFormat(src)
	}

	stripped, err := imgpkg.StripMetadata(resized, out)
	if err != nil {
		return processedImage{}, err
	}

	cfg, _, err := stdimage.DecodeConfig(bytes.NewReader(stripped))
	if err != nil {
		return processedImage{}, fmt.Errorf("decode config: %w", err)
	}
	return processedImage{
		data:      stripped,
		mediaType: out,
		width:     cfg.Width,
		height:    cfg.Height,
	}, nil
}
