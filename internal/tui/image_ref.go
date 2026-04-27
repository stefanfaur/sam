package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stefanfaur/sam/internal/llm"
)

// imageRefPrefix is the syntax users type to attach an image by file path.
const imageRefPrefix = "@image:"

// parseImageSyntax extracts the file path from an @image:/path token. Returns
// (path, true) on a match. Trailing punctuation that is not part of a path
// (a closing parens, comma, period at end of sentence) is intentionally not
// trimmed — let the user be precise.
func parseImageSyntax(token string) (string, bool) {
	if !strings.HasPrefix(token, imageRefPrefix) {
		return "", false
	}
	path := strings.TrimSpace(strings.TrimPrefix(token, imageRefPrefix))
	if path == "" {
		return "", false
	}
	return path, true
}

// loadImageFromFile reads the file and infers media type from extension.
func loadImageFromFile(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", path, err)
	}
	mt := mimeFromExt(filepath.Ext(path))
	if mt == "" {
		return nil, "", fmt.Errorf("unsupported image extension: %s", filepath.Ext(path))
	}
	return data, mt, nil
}

func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

// attachImageRefs loads each path, runs the preprocess pipeline, and appends
// the result to m.imageAttachments. Vision capability is checked once up
// front; if the current model doesn't support vision the call refuses
// without partial-attaching anything.
func (m *Model) attachImageRefs(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	wire := ""
	if entry, ok := m.providers[m.status.provider]; ok {
		wire = entry.Wire
	}
	if !visionSupported(wire, m.status.model) {
		return fmt.Errorf("model %q (%s) doesn't support images. Vision-capable: %s",
			m.status.model, m.status.provider, joinModels(visionCapableModels()))
	}
	staged := make([]imageAttachment, 0, len(paths))
	for _, p := range paths {
		data, mt, err := loadImageFromFile(p)
		if err != nil {
			return err
		}
		processed, err := preprocessImage(data, mt)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		staged = append(staged, imageAttachment{
			data:      processed.data,
			mediaType: processed.mediaType,
			width:     processed.width,
			height:    processed.height,
			tokens:    estimateTokens(processed.width, processed.height, m.status.provider),
		})
	}
	m.imageAttachments = append(m.imageAttachments, staged...)
	return nil
}

// collectAttachments converts the TUI-side imageAttachment slice into the
// llm-level ImageAttachment shape consumed by Agent.SubmitWithAttachments.
func (m *Model) collectAttachments() []llm.ImageAttachment {
	if len(m.imageAttachments) == 0 {
		return nil
	}
	out := make([]llm.ImageAttachment, 0, len(m.imageAttachments))
	for _, a := range m.imageAttachments {
		out = append(out, llm.ImageAttachment{
			Data:      a.data,
			MediaType: a.mediaType,
			Width:     a.width,
			Height:    a.height,
		})
	}
	return out
}

// extractImageRefs scans text for @image:/path tokens, removes them from the
// outgoing message, and returns (cleanedText, refPaths). The cleaned text is
// what the agent sees; the paths are loaded and attached separately.
func extractImageRefs(text string) (string, []string) {
	if !strings.Contains(text, imageRefPrefix) {
		return text, nil
	}
	var paths []string
	var b strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		fields := strings.Fields(line)
		kept := make([]string, 0, len(fields))
		for _, f := range fields {
			if p, ok := parseImageSyntax(f); ok {
				paths = append(paths, p)
				continue
			}
			kept = append(kept, f)
		}
		b.WriteString(strings.Join(kept, " "))
	}
	return strings.TrimRight(b.String(), " \n"), paths
}
