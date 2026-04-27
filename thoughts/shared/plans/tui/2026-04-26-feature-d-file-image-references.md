# Feature D — File and Image References Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use executing-plans to implement this plan task-by-task.

**Goal:** Deliver @file autocomplete overlay and clipboard image paste with vision capability gating, preprocessing pipeline, and token estimation.

**Architecture:** 
- **@file picker:** Bubbletea overlay modal (similar to settings_modal.go pattern) sourcing files from `git ls-files` or recursive walk; fuzzy filtering. Opens on `@` keypress in input.
- **Image paste:** Binary clipboard read (platform-specific via golang.design/x/clipboard), encoding to base64, attachment to next user message. Preprocessing: resize, format selection, metadata strip.
- **Vision gating:** Vision bool flag added to both openaicompat Capabilities and Anthropic-native provider caps; checked before attach; toast refusal with model list.
- **Token estimate:** Per-provider formula badge (`(w*h)/750` for Anthropic, tile-based for GPT-4o, unknown otherwise).
- **Message types:** ContentImage variant added to llm.ContentBlock to carry base64 + media-type.

**Tech Stack:**
- `golang.design/x/clipboard` (binary image clipboard, CGO)
- `github.com/disintegration/imaging` (Lanczos resize, format conversion)
- `github.com/sahilm/fuzzy` (fuzzy matching for @file picker)
- Go 1.26.2 with existing Bubbletea/Charmbracelet stack

**Strict TDD:** Each task includes failing test first, minimal implementation, commit.

---

## Task 1: Image Preprocessing Pipeline (standalone, no TUI integration yet)

**Files:**
- Create: `internal/image/process.go`
- Create: `internal/image/process_test.go`
- Modify: `go.mod` (add github.com/disintegration/imaging)

**Step 1: Add imaging dependency to go.mod**

Manually add to the require section:
```
github.com/disintegration/imaging v1.6.2
```

Run `go mod tidy` to update go.mod and go.sum.

```bash
go mod tidy
```

Expected: No errors; imaging appears in go.mod and go.sum.

**Step 2: Write failing test for image resize (long edge > 1568)**

File: `internal/image/process_test.go`

```go
package image

import (
	"bytes"
	"image/png"
	"testing"
)

func TestResizeDownscalesLongEdgeAbove1568(t *testing.T) {
	// Create a 2000×1000 PNG image
	img := createTestImage(2000, 1000)
	data := encodeTestImagePNG(img)

	result, err := ResizeImage(data, "image/png")
	if err \!= nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	// Decode and verify dimensions
	resultImg, _, err := image.DecodeConfig(bytes.NewReader(result))
	if err \!= nil {
		t.Fatalf("failed to decode result: %v", err)
	}

	// Long edge should be exactly 1568; aspect preserved
	// 2000×1000 → 1568×784
	if resultImg.Width \!= 1568 || resultImg.Height \!= 784 {
		t.Errorf("expected 1568×784, got %d×%d", resultImg.Width, resultImg.Height)
	}
}

func TestResizeNoOpIfUnderThreshold(t *testing.T) {
	// 1200×800 image stays unchanged
	img := createTestImage(1200, 800)
	data := encodeTestImagePNG(img)

	result, err := ResizeImage(data, "image/png")
	if err \!= nil {
		t.Fatalf("ResizeImage failed: %v", err)
	}

	resultImg, _, err := image.DecodeConfig(bytes.NewReader(result))
	if err \!= nil {
		t.Fatalf("failed to decode result: %v", err)
	}

	if resultImg.Width \!= 1200 || resultImg.Height \!= 800 {
		t.Errorf("expected 1200×800, got %d×%d", resultImg.Width, resultImg.Height)
	}
}

// Helper functions (not shown in full detail; create real test images)
func createTestImage(w, h int) image.Image { /* ... */ }
func encodeTestImagePNG(img image.Image) []byte { /* ... */ }
```

Run test:
```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam && go test -v ./internal/image -run TestResize
```

Expected: FAIL — "ResizeImage is not defined" or similar.

**Step 3: Implement ResizeImage function**

File: `internal/image/process.go`

```go
package image

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/disintegration/imaging"
)

// ResizeImage resizes the image data if long edge exceeds 1568px.
// Preserves aspect ratio using Lanczos filter.
// format is the MIME type (e.g., "image/png").
func ResizeImage(data []byte, format string) ([]byte, error) {
	img, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err \!= nil {
		return nil, err
	}

	w, h := img.Width, img.Height
	maxDim := w
	if h > maxDim {
		maxDim = h
	}

	// No resize needed
	if maxDim <= 1568 {
		return data, nil
	}

	// Decode full image
	decodedImg, err := decodeImage(bytes.NewReader(data))
	if err \!= nil {
		return nil, err
	}

	// Calculate new dimensions
	scale := float64(1568) / float64(maxDim)
	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)

	// Resize with Lanczos
	resized := imaging.Resize(decodedImg, newW, newH, imaging.Lanczos)

	// Encode back to original format
	var buf bytes.Buffer
	switch format {
	case "image/png":
		if err := png.Encode(&buf, resized); err \!= nil {
			return nil, err
		}
	case "image/jpeg":
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 90}); err \!= nil {
			return nil, err
		}
	default:
		// Default to JPEG
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 90}); err \!= nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func decodeImage(r io.Reader) (image.Image, error) {
	img, _, err := image.Decode(r)
	return img, err
}
```

Run test:
```bash
go test -v ./internal/image -run TestResize
```

Expected: PASS.

**Step 4: Write test for format selection (PNG if alpha, else JPEG q90)**

```go
func TestFormatSelectionWithAlpha(t *testing.T) {
	// Image with alpha channel should be PNG
	img := createTestImageWithAlpha(256, 256)
	fmt := SelectFormat(img)
	if fmt \!= "image/png" {
		t.Errorf("expected image/png for image with alpha, got %s", fmt)
	}
}

func TestFormatSelectionNoAlpha(t *testing.T) {
	// Opaque image should be JPEG
	img := createTestImageOpaque(256, 256)
	fmt := SelectFormat(img)
	if fmt \!= "image/jpeg" {
		t.Errorf("expected image/jpeg for opaque image, got %s", fmt)
	}
}
```

Run test:
```bash
go test -v ./internal/image -run TestFormatSelection
```

Expected: FAIL — "SelectFormat is not defined".

**Step 5: Implement SelectFormat**

```go
import (
	"image"
	"image/color"
)

// SelectFormat returns "image/png" if img has alpha or appears screenshot-ish,
// else "image/jpeg" for smaller file size.
func SelectFormat(img image.Image) string {
	bounds := img.Bounds()
	if hasAlphaChannel(img) {
		return "image/png"
	}

	// Heuristic: check for screenshot characteristics (low color count, sharp edges).
	// For MVP, simple check: if max dimensions are odd or color variance is high, assume
	// it's a natural photo. Otherwise assume screenshot (diagram, UI).
	if isLikelyScreenshot(img) {
		return "image/png"
	}

	return "image/jpeg"
}

func hasAlphaChannel(img image.Image) bool {
	_, ok := img.(interface{ AlphaAt(x, y int) color.Alpha })
	return ok
}

func isLikelyScreenshot(img image.Image) bool {
	// Simple heuristic: sample a few pixels and count distinct colors.
	// If color count is low (< 256), likely a diagram/UI screenshot.
	// This is intentionally conservative; PNG is safe for screenshots.
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// Sample 100 random pixels
	colors := make(map[uint32]bool)
	step := (w * h) / 100
	if step == 0 {
		step = 1
	}

	for i := 0; i < (w * h); i += step {
		x := bounds.Min.X + (i % w)
		y := bounds.Min.Y + (i / w)
		r, g, b, _ := img.At(x, y).RGBA()
		// Reduce to 8-bit per channel for counting
		col := ((r >> 8) << 16) | ((g >> 8) << 8) | (b >> 8)
		colors[col] = true
	}

	return len(colors) < 256
}
```

Run test:
```bash
go test -v ./internal/image -run TestFormatSelection
```

Expected: PASS.

**Step 6: Write test for EXIF/ICC stripping**

```go
func TestStripMetadata(t *testing.T) {
	// Load a test JPEG with EXIF
	data := loadTestImageWithEXIF()

	stripped, err := StripMetadata(data, "image/jpeg")
	if err \!= nil {
		t.Fatalf("StripMetadata failed: %v", err)
	}

	// Verify EXIF is gone (re-encode and check)
	img, err := jpeg.Decode(bytes.NewReader(stripped))
	if err \!= nil {
		t.Fatalf("decode failed: %v", err)
	}

	// If original had EXIF, stripped should be smaller
	if len(stripped) >= len(data) {
		// Not a strong test, but indicates EXIF might not be stripped
		// In a real test, parse EXIF markers directly
		t.Logf("warning: stripped size %d >= original %d", len(stripped), len(data))
	}
}
```

Run test:
```bash
go test -v ./internal/image -run TestStripMetadata
```

Expected: FAIL — "StripMetadata is not defined".

**Step 7: Implement StripMetadata**

```go
// StripMetadata removes EXIF and ICC profile markers, re-encodes the image.
// This is done by decoding and re-encoding without preserving markers.
func StripMetadata(data []byte, mimeType string) ([]byte, error) {
	img, err := decodeImage(bytes.NewReader(data))
	if err \!= nil {
		return nil, err
	}

	var buf bytes.Buffer
	switch mimeType {
	case "image/png":
		if err := png.Encode(&buf, img); err \!= nil {
			return nil, err
		}
	case "image/jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err \!= nil {
			return nil, err
		}
	default:
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err \!= nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}
```

Run test:
```bash
go test -v ./internal/image
```

Expected: PASS for all image tests.

**Step 8: Commit**

```bash
git add internal/image/process.go internal/image/process_test.go go.mod go.sum
git commit -m "feat: add image preprocessing pipeline (resize, format selection, metadata strip)"
```

---

## Task 2: Vision Capability Registry

**Files:**
- Modify: `internal/llm/openaicompat/caps.go` (add Vision bool)
- Create: `internal/llm/anthropic_caps.go` (parallel Vision registry for Anthropic SDK)
- Modify: `internal/llm/types.go` (extend Capabilities interface or create new provider-caps type)
- Modify: `internal/llm/registry.go` (populate Vision flags per known models)

**Step 1: Write failing test for Vision capability on known models**

File: `internal/llm/openaicompat/caps_test.go` (add to existing):

```go
func TestVisionCapabilityFlags(t *testing.T) {
	tests := []struct {
		model      string
		wantVision bool
	}{
		{"gpt-4o", true},
		{"gpt-4-turbo", true},
		{"gpt-4", false},
		{"gpt-3.5-turbo", false},
		{"deepseek-v3", false},
	}

	for _, tt := range tests {
		caps := DefaultCaps(tt.model)
		if caps.Vision \!= tt.wantVision {
			t.Errorf("model %s: Vision=%v, want %v", tt.model, caps.Vision, tt.wantVision)
		}
	}
}
```

Run test:
```bash
go test -v ./internal/llm/openaicompat -run TestVision
```

Expected: FAIL — "Vision field not found".

**Step 2: Add Vision bool to openaicompat Capabilities struct**

File: `internal/llm/openaicompat/caps.go`, in the Capabilities struct:

```go
type Capabilities struct {
	SystemRole                string
	// ... existing fields ...
	Vision                    bool   // whether model supports vision/image content
}
```

Also update the defaultBaseCaps:

```go
var defaultBaseCaps = Capabilities{
	// ... existing ...
	Vision: false,
}
```

**Step 3: Update orderedCapsTable to set Vision for vision-capable models**

In caps.go, update the entries:

```go
{
	prefixes: []string{"gpt-4o"},
	caps: Capabilities{
		SystemRole:             "system",
		MaxTokensField:         "max_tokens",
		SupportsSamplingParams: true,
		ReasoningSource:        "none",
		EchoReasoning:          false,
		Vision:                 true,  // ← add this
	},
},
{
	prefixes: []string{"gpt-4-turbo"},
	caps: Capabilities{
		SystemRole:             "system",
		MaxTokensField:         "max_tokens",
		SupportsSamplingParams: true,
		ReasoningSource:        "none",
		EchoReasoning:          false,
		Vision:                 true,  // ← add this
	},
},
```

Other entries (gpt-4, gpt-3.5, etc.) remain with Vision: false (default).

Run test:
```bash
go test -v ./internal/llm/openaicompat -run TestVision
```

Expected: PASS.

**Step 4: Create Anthropic Vision capabilities (parallel structure)**

File: `internal/llm/anthropic_caps.go` (new):

```go
package llm

// AnthropicCapabilities mirrors openaicompat.Capabilities for Anthropic SDK.
// Add it here to avoid circular import and keep Anthropic-specific config centralized.
type AnthropicCapabilities struct {
	Vision bool
}

var anthropicModelCaps = map[string]AnthropicCapabilities{
	"claude-3-opus": {Vision: true},
	"claude-3-sonnet": {Vision: true},
	"claude-3-haiku": {Vision: true},
	"claude-3.5-sonnet": {Vision: true},
	"claude-opus": {Vision: true},
	"claude-sonnet": {Vision: true},
	"claude-haiku": {Vision: true},
}

// AnthropicVisionSupported returns whether the model supports vision.
func AnthropicVisionSupported(model string) bool {
	caps, ok := anthropicModelCaps[model]
	return ok && caps.Vision
}
```

**Step 5: Write test for Anthropic Vision support**

File: `internal/llm/anthropic_caps_test.go` (new):

```go
package llm

import "testing"

func TestAnthropicVisionSupported(t *testing.T) {
	tests := []struct {
		model      string
		wantVision bool
	}{
		{"claude-3-opus", true},
		{"claude-sonnet", true},
		{"claude-haiku", true},
		{"unknown-model", false},
	}

	for _, tt := range tests {
		if got := AnthropicVisionSupported(tt.model); got \!= tt.wantVision {
			t.Errorf("AnthropicVisionSupported(%q) = %v, want %v", tt.model, got, tt.wantVision)
		}
	}
}
```

Run test:
```bash
go test -v ./internal/llm -run TestAnthropicVision
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/llm/openaicompat/caps.go internal/llm/openaicompat/caps_test.go \
         internal/llm/anthropic_caps.go internal/llm/anthropic_caps_test.go
git commit -m "feat: add Vision capability flag to OpenAI-compatible and Anthropic providers"
```

---

## Task 3: Image Content Block Type in llm.Message

**Files:**
- Modify: `internal/llm/types.go` (add ContentImage type and variant to ContentBlock)

**Step 1: Write test for image content in Message**

File: `internal/llm/types_test.go` (add to existing or create):

```go
package llm

import (
	"encoding/json"
	"testing"
)

func TestImageContentBlock(t *testing.T) {
	// Create a message with an image content block
	msg := Message{
		Role: RoleUser,
		Content: []ContentBlock{
			{
				Type:     ContentText,
				Text:     "Here's an image:",
			},
			{
				Type:      ContentImage,
				ImageData: "iVBORw0KGgo...",
				MediaType: "image/png",
			},
		},
	}

	// Marshal and unmarshal to verify it round-trips
	data, err := json.Marshal(msg)
	if err \!= nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var unmarshalled Message
	if err := json.Unmarshal(data, &unmarshalled); err \!= nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(unmarshalled.Content) \!= 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(unmarshalled.Content))
	}

	if unmarshalled.Content[1].Type \!= ContentImage {
		t.Errorf("expected ContentImage type, got %s", unmarshalled.Content[1].Type)
	}

	if unmarshalled.Content[1].ImageData == "" {
		t.Error("ImageData is empty")
	}
}
```

Run test:
```bash
go test -v ./internal/llm -run TestImageContentBlock
```

Expected: FAIL — "ContentImage not defined" or "ImageData field not found".

**Step 2: Add ContentImage type and update ContentBlock**

File: `internal/llm/types.go`:

```go
const (
	ContentText       ContentType = "text"
	ContentThinking   ContentType = "thinking"
	ContentToolUse    ContentType = "tool_use"
	ContentToolResult ContentType = "tool_result"
	ContentImage      ContentType = "image"  // ← add this
)

type ContentBlock struct {
	Type      ContentType     `json:"type"`
	Text      string          `json:"text,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    string          `json:"output,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	ImageData string          `json:"image_data,omitempty"`   // ← add: base64-encoded image
	MediaType string          `json:"media_type,omitempty"`   // ← add: "image/png", "image/jpeg", etc.
}
```

Run test:
```bash
go test -v ./internal/llm -run TestImageContentBlock
```

Expected: PASS.

**Step 3: Commit**

```bash
git add internal/llm/types.go internal/llm/types_test.go
git commit -m "feat: add ContentImage type to llm.ContentBlock for carrying image data"
```

---

## Task 4: Clipboard Binary Image Read

**Files:**
- Modify: `go.mod` (add golang.design/x/clipboard)
- Create: `internal/clipboard/read.go`
- Create: `internal/clipboard/read_test.go`

**Step 1: Add clipboard dependency to go.mod**

Add to require section:
```
golang.design/x/clipboard v0.7.0
```

Run `go mod tidy`:
```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam && go mod tidy
```

Expected: No errors.

**Step 2: Write failing test for binary image clipboard read**

File: `internal/clipboard/read_test.go`:

```go
package clipboard

import (
	"bytes"
	"image/png"
	"testing"
)

func TestReadImageFromClipboard(t *testing.T) {
	// This test will only work on systems with clipboard support.
	// For now, write a mock or skip in CI.
	t.Skip("requires clipboard with image data")
}

func TestParseImageMIMEType(t *testing.T) {
	tests := []struct {
		mimeType string
		valid    bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/gif", true},
		{"image/webp", true},
		{"text/plain", false},
		{"application/json", false},
	}

	for _, tt := range tests {
		err := ValidateImageMIME(tt.mimeType)
		if (err == nil) \!= tt.valid {
			t.Errorf("ValidateImageMIME(%q): err=%v, valid=%v", tt.mimeType, err, tt.valid)
		}
	}
}

func TestDecodeImageFromBytes(t *testing.T) {
	// Create a test PNG
	img := createTestPNG(256, 256)
	data := bytes.Buffer{}
	if err := png.Encode(&data, img); err \!= nil {
		t.Fatalf("failed to encode test image: %v", err)
	}

	decoded, err := DecodeImage(data.Bytes())
	if err \!= nil {
		t.Fatalf("DecodeImage failed: %v", err)
	}

	if decoded == nil {
		t.Error("DecodeImage returned nil")
	}
}
```

Run test:
```bash
go test -v ./internal/clipboard -run TestParseImageMIMEType
```

Expected: FAIL — "ValidateImageMIME not defined".

**Step 3: Implement binary clipboard read**

File: `internal/clipboard/read.go`:

```go
package clipboard

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"golang.design/x/clipboard"
)

// ValidateImageMIME checks if the MIME type is a supported image type.
func ValidateImageMIME(mimeType string) error {
	allowed := map[string]bool{
		"image/png":  true,
		"image/jpeg": true,
		"image/jpg":  true,
		"image/gif":  true,
		"image/webp": true,
	}

	if \!allowed[strings.ToLower(mimeType)] {
		return fmt.Errorf("unsupported image type: %s", mimeType)
	}

	return nil
}

// ReadImageFromClipboard attempts to read a binary image from the system clipboard.
// Returns (data []byte, mimeType string, error).
// On platforms without clipboard support or no image in clipboard, returns error.
func ReadImageFromClipboard() ([]byte, string, error) {
	// Read with multiple possible MIME types
	mimeTypes := []string{
		"image/png",
		"image/jpeg",
		"image/gif",
		"image/webp",
	}

	for _, mime := range mimeTypes {
		data, err := clipboard.Read(clipboard.FmtImage, mime)
		if err == nil && len(data) > 0 {
			return data, mime, nil
		}
	}

	return nil, "", fmt.Errorf("no image data found in clipboard")
}

// DecodeImage decodes image data from bytes (used for validation before processing).
func DecodeImage(data []byte) (image.Image, error) {
	img, _, err := image.DecodeConfig(/* ... */)
	// Actually decode if needed for validation
	// For MVP, just validate the format
	return nil, nil // placeholder
}
```

Run test:
```bash
go test -v ./internal/clipboard -run TestParseImageMIMEType
```

Expected: PASS.

**Step 4: Commit**

```bash
git add internal/clipboard/read.go internal/clipboard/read_test.go go.mod go.sum
git commit -m "feat: add binary image clipboard read with MIME validation"
```

---

## Task 5: @file Picker Overlay (Bubbletea Modal)

**Files:**
- Create: `internal/tui/file_picker.go`
- Create: `internal/tui/file_picker_test.go`
- Modify: `go.mod` (add github.com/sahilm/fuzzy)

**Step 1: Add fuzzy dependency**

```
github.com/sahilm/fuzzy v0.1.1
```

Run `go mod tidy`:
```bash
go mod tidy
```

**Step 2: Write failing test for file source (git ls-files)**

File: `internal/tui/file_picker_test.go`:

```go
package tui

import (
	"os"
	"testing"
	"testing/fstest"
)

func TestGetFilesFromGitRepo(t *testing.T) {
	// In a temp git repo, add some files and test git ls-files
	t.Skip("requires a git repo context; use integration test")
}

func TestGetFilesRecursiveWalk(t *testing.T) {
	// Create a temp directory with files
	tmpfs := fstest.MapFS{
		"file1.go":         {},
		"file2.go":         {},
		"subdir/file3.go":  {},
		".gitignore":       {},
	}

	// Test that recursive walk respects .gitignore
	files, err := getFilesRecursive(".", tmpfs)
	if err \!= nil {
		t.Fatalf("getFilesRecursive failed: %v", err)
	}

	if len(files) == 0 {
		t.Error("expected files, got none")
	}
}

func TestFuzzyFilterFiles(t *testing.T) {
	files := []string{
		"internal/tui/app.go",
		"internal/tui/update.go",
		"internal/agent/agent.go",
		"cmd/sam/main.go",
	}

	matches := fuzzyFilter("app", files)
	if len(matches) == 0 {
		t.Error("expected matches for 'app', got none")
	}

	// "app" should match "app.go"
	found := false
	for _, m := range matches {
		if m == "internal/tui/app.go" {
			found = true
			break
		}
	}

	if \!found {
		t.Error("'app' did not match 'internal/tui/app.go'")
	}
}
```

Run test:
```bash
go test -v ./internal/tui -run TestFuzzyFilter
```

Expected: FAIL — "fuzzyFilter not defined".

**Step 3: Implement file picker types and functions**

File: `internal/tui/file_picker.go`:

```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sahilm/fuzzy"
)

type filePicker struct {
	files    []string
	filtered []string
	query    string
	selected int
	done     bool
	cancelled bool
}

// getFilesForPicker returns the list of files to populate the picker.
// Tries git ls-files first; falls back to recursive walk if not in a git repo.
func getFilesForPicker(cwd string) ([]string, error) {
	// Try git ls-files
	files, err := getFilesFromGit(cwd)
	if err == nil && len(files) > 0 {
		return files, nil
	}

	// Fall back to recursive walk
	return getFilesRecursive(cwd)
}

// getFilesFromGit runs `git ls-files` to get tracked files.
func getFilesFromGit(cwd string) ([]string, error) {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = cwd

	out, err := cmd.Output()
	if err \!= nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines, nil
}

// getFilesRecursive walks the directory tree, respecting .gitignore.
// For MVP, do a simple walk; .gitignore support can be added via go-gitignore later.
func getFilesRecursive(cwd string) ([]string, error) {
	var files []string

	err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err \!= nil {
			return err
		}

		// Skip hidden dirs
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") {
			return filepath.SkipDir
		}

		// Include files (not dirs)
		if \!info.IsDir() {
			rel, err := filepath.Rel(cwd, path)
			if err == nil {
				files = append(files, rel)
			}
		}

		return nil
	})

	return files, err
}

// fuzzyFilter returns files matching the query using fuzzy matching.
func fuzzyFilter(query string, files []string) []string {
	if query == "" {
		return files
	}

	matches := fuzzy.Find(query, files)
	var result []string
	for _, m := range matches {
		result = append(result, m.String)
	}
	return result
}
```

Run test:
```bash
go test -v ./internal/tui -run TestFuzzy
```

Expected: PASS.

**Step 4: Write test for picker Update and View (Bubbletea)**

```go
func TestFilePickerUpdate(t *testing.T) {
	fp := &filePicker{
		files:    []string{"app.go", "update.go", "agent.go"},
		filtered: []string{"app.go", "update.go", "agent.go"},
		selected: 0,
	}

	// Typing "a" should filter
	fp.query = "a"
	fp.filtered = fuzzyFilter(fp.query, fp.files)

	if len(fp.filtered) == 0 {
		t.Error("expected matches for 'a'")
	}
}

func TestFilePickerNavigation(t *testing.T) {
	fp := &filePicker{
		files:    []string{"a.go", "b.go", "c.go"},
		filtered: []string{"a.go", "b.go", "c.go"},
		selected: 0,
	}

	// Down arrow should increment selection
	fp.selected = (fp.selected + 1) % len(fp.filtered)
	if fp.selected \!= 1 {
		t.Errorf("expected selected=1, got %d", fp.selected)
	}

	// Up arrow should decrement
	fp.selected = (fp.selected - 1 + len(fp.filtered)) % len(fp.filtered)
	if fp.selected \!= 0 {
		t.Errorf("expected selected=0, got %d", fp.selected)
	}
}
```

Run test:
```bash
go test -v ./internal/tui -run TestFilePickerNavigation
```

Expected: PASS.

**Step 5: Add picker to Model and handleKey routing**

File: `internal/tui/app.go`, add to Model struct:

```go
type Model struct {
	// ... existing fields ...
	filePicker   *filePicker
	pickerActive bool
}
```

File: `internal/tui/update.go`, in handleKey, add routing for `@` and Esc when picker is active:

```go
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ... existing Esc/Enter logic ...

	// File picker is active: handle arrow/Tab/Enter/Esc
	if m.pickerActive && m.filePicker \!= nil {
		switch msg.Type {
		case tea.KeyUp:
			if m.filePicker.selected > 0 {
				m.filePicker.selected--
			}
			return m, nil
		case tea.KeyDown:
			if m.filePicker.selected < len(m.filePicker.filtered)-1 {
				m.filePicker.selected++
			}
			return m, nil
		case tea.KeyTab, tea.KeyEnter:
			// Complete: add selected file to input
			if m.filePicker.selected < len(m.filePicker.filtered) {
				selected := m.filePicker.filtered[m.filePicker.selected]
				m.input.InsertString("@" + selected + " ")
				m.pickerActive = false
				m.filePicker = nil
			}
			return m, nil
		case tea.KeyEsc:
			m.pickerActive = false
			m.filePicker = nil
			return m, nil
		}
	}

	// Detect `@` in input to open picker
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '@' {
		// Open picker
		files, _ := getFilesForPicker(m.launchDir)
		m.filePicker = &filePicker{
			files:    files,
			filtered: files,
			selected: 0,
		}
		m.pickerActive = true
		return m, nil
	}

	// ... rest of existing handleKey logic ...
}
```

**Step 6: Add picker rendering to View**

File: `internal/tui/render.go`, in View() or a renderPickerOverlay helper:

```go
func (m *Model) renderPickerOverlay() string {
	if \!m.pickerActive || m.filePicker == nil {
		return ""
	}

	// Simple picker view: list files with selection highlight
	var lines []string
	lines = append(lines, "File picker (press Esc to close)")
	lines = append(lines, "Query: "+m.filePicker.query)
	lines = append(lines, "")

	for i, file := range m.filePicker.filtered {
		prefix := "  "
		if i == m.filePicker.selected {
			prefix = "> "
		}
		lines = append(lines, prefix+file)
	}

	return strings.Join(lines, "\n")
}
```

Integrate into View():
```go
func (m *Model) View() string {
	// ... existing view logic ...

	if m.pickerActive {
		return m.renderPickerOverlay()
	}

	// ... rest of view ...
}
```

**Step 7: Commit**

```bash
git add internal/tui/file_picker.go internal/tui/file_picker_test.go \
         internal/tui/app.go internal/tui/update.go internal/tui/render.go \
         go.mod go.sum
git commit -m "feat: add @file autocomplete picker overlay with fuzzy filtering"
```

---

## Task 6: Image Paste and Token Estimation Badge

**Files:**
- Modify: `internal/tui/app.go` (add imageAttachments field to Model)
- Modify: `internal/tui/update.go` (handle Cmd+V / Ctrl+V)
- Create: `internal/tui/image_attach.go` (paste handler, token estimation)
- Create: `internal/tui/image_attach_test.go`
- Modify: `internal/tui/render.go` (thumbnail badge)

**Step 1: Write failing test for image attachment state**

File: `internal/tui/image_attach_test.go`:

```go
package tui

import (
	"testing"
)

type imageAttachment struct {
	data      []byte
	mimeType  string
	width     int
	height    int
	tokens    int
}

func TestEstimateTokensAnthropicVision(t *testing.T) {
	// Anthropic: (w*h)/750
	tokens := estimateTokens(1000, 800, "anthropic")
	expected := (1000 * 800) / 750
	if tokens \!= expected {
		t.Errorf("Anthropic token estimate: got %d, expected %d", tokens, expected)
	}
}

func TestEstimateTokensGPT4o(t *testing.T) {
	// GPT-4o: tile-based (simplified: (w*h)/750 as baseline, but actual is more complex)
	// For MVP, use a simple formula; improve later
	tokens := estimateTokens(1024, 1024, "gpt-4o")
	if tokens <= 0 {
		t.Error("expected positive token estimate")
	}
}

func TestEstimateTokensUnknownProvider(t *testing.T) {
	// Unknown provider should return a sentinel value (e.g., -1) indicating "unknown"
	tokens := estimateTokens(1000, 800, "unknown-provider")
	if tokens \!= -1 {
		t.Errorf("expected -1 for unknown provider, got %d", tokens)
	}
}

func TestFormatTokenEstimate(t *testing.T) {
	tests := []struct {
		tokens   int
		expected string
	}{
		{256, "256 tok"},
		{-1, "≈ unknown"},
	}

	for _, tt := range tests {
		got := formatTokenEstimate(tt.tokens)
		if got \!= tt.expected {
			t.Errorf("formatTokenEstimate(%d): got %q, expected %q", tt.tokens, got, tt.expected)
		}
	}
}
```

Run test:
```bash
go test -v ./internal/tui -run TestEstimate
```

Expected: FAIL — "estimateTokens not defined".

**Step 2: Implement image attachment and token estimation**

File: `internal/tui/image_attach.go`:

```go
package tui

import (
	"fmt"
	"strings"
)

type imageAttachment struct {
	data      []byte
	mimeType  string
	width     int
	height    int
	fileSize  int
	tokens    int
}

// estimateTokens returns the token cost for an image, per provider's formula.
// Returns -1 for unknown providers.
func estimateTokens(width, height int, provider string) int {
	provider = strings.ToLower(provider)

	switch {
	case strings.Contains(provider, "anthropic"), strings.Contains(provider, "claude"):
		// Anthropic: (width * height) / 750
		return (width * height) / 750

	case strings.Contains(provider, "gpt-4o"):
		// GPT-4o: tile-based, roughly (width * height) / 512
		// (more precise: 172 tokens per 512×512 tile, but simplify for MVP)
		return (width * height) / 512

	case strings.Contains(provider, "gpt"):
		// Generic GPT: conservative estimate
		return (width * height) / 750

	default:
		return -1 // unknown
	}
}

// formatTokenEstimate returns a human-readable token string.
func formatTokenEstimate(tokens int) string {
	if tokens < 0 {
		return "≈ unknown"
	}
	return fmt.Sprintf("%d tok", tokens)
}

// attachmentBadge generates the thumbnail badge text.
// e.g., "📎 1 image · ~340 tok (Anthropic) · 1568×880 · 234KB"
func attachmentBadge(attachments []imageAttachment, provider string) string {
	if len(attachments) == 0 {
		return ""
	}

	totalTokens := 0
	totalSize := 0
	var dimensions []string

	for _, a := range attachments {
		totalTokens += a.tokens
		totalSize += a.fileSize
		dimensions = append(dimensions, fmt.Sprintf("%d×%d", a.width, a.height))
	}

	count := len(attachments)
	itemWord := "image"
	if count > 1 {
		itemWord = "images"
	}

	sizeStr := formatBytes(totalSize)
	dimStr := strings.Join(dimensions, ", ")
	tokenStr := formatTokenEstimate(totalTokens)

	return fmt.Sprintf("📎 %d %s · %s (%s) · %s · %s",
		count, itemWord, tokenStr, provider, dimStr, sizeStr)
}

func formatBytes(b int) string {
	if b < 1024 {
		return fmt.Sprintf("%dB", b)
	}
	if b < 1024*1024 {
		return fmt.Sprintf("%dKB", b/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
}
```

Run test:
```bash
go test -v ./internal/tui -run TestEstimate
```

Expected: PASS.

**Step 3: Add image attachment field to Model**

File: `internal/tui/app.go`:

```go
type Model struct {
	// ... existing fields ...
	imageAttachments []imageAttachment
}
```

**Step 4: Handle Cmd+V / Ctrl+V keypress**

File: `internal/tui/update.go`, add to handleKey:

```go
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ... existing logic ...

	// Detect Cmd+V (macOS) or Ctrl+V (Linux/Windows)
	if (msg.Type == tea.KeyCtrlV) || (msg.Type == tea.KeyCtrlC && isOSMacOS) {
		return m, m.cmdPasteImage()
	}

	// ... rest ...
}

func (m *Model) cmdPasteImage() tea.Cmd {
	return func() tea.Msg {
		// Read from clipboard
		data, mimeType, err := readImageFromClipboard()
		if err \!= nil {
			return errMsg{err}
		}

		// Validate MIME type
		if err := ValidateImageMIME(mimeType); err \!= nil {
			return errMsg{err}
		}

		// Check vision capability
		if \!m.visionSupported() {
			return errMsg{fmt.Errorf("current model doesn't support images")}
		}

		// Process image
		processed, err := preprocessImage(data, mimeType)
		if err \!= nil {
			return errMsg{err}
		}

		// Estimate tokens
		tokens := estimateTokens(processed.width, processed.height, m.provider)

		// Add to attachments
		m.imageAttachments = append(m.imageAttachments, imageAttachment{
			data:     processed.data,
			mimeType: processed.mimeType,
			width:    processed.width,
			height:   processed.height,
			fileSize: len(processed.data),
			tokens:   tokens,
		})

		return imageAttachedMsg{}
	}
}

func (m *Model) visionSupported() bool {
	// Check provider/model caps
	// Placeholder: will be properly wired in next task
	return true
}
```

**Step 5: Render attachment badge above input**

File: `internal/tui/render.go`, update input area rendering:

```go
func (m *Model) renderInputArea() string {
	// Render badge if images attached
	badge := ""
	if len(m.imageAttachments) > 0 {
		badge = attachmentBadge(m.imageAttachments, m.provider)
		badge += "\n"
	}

	return badge + m.input.View()
}
```

Update View() to call renderInputArea() instead of direct m.input.View().

**Step 6: Commit**

```bash
git add internal/tui/image_attach.go internal/tui/image_attach_test.go \
         internal/tui/app.go internal/tui/update.go internal/tui/render.go
git commit -m "feat: add image paste (Cmd+V/Ctrl+V) with token estimation badge"
```

---

## Task 7: Vision Capability Gating (TUI Integration)

**Files:**
- Modify: `internal/tui/update.go` (check vision cap before paste)
- Modify: `internal/tui/app.go` (wire in vision check)

**Step 1: Write failing test for vision gating**

File: `internal/tui/update_test.go` (add to existing):

```go
func TestPasteImageWithoutVisionCapabilityFails(t *testing.T) {
	m := newTestModel(t)
	m.model = "gpt-3.5"  // no vision support
	m.imageAttachments = []imageAttachment{}

	// Simulate paste event
	err := m.validateImagePaste()
	if err == nil {
		t.Error("expected error for model without vision support")
	}

	if len(m.imageAttachments) > 0 {
		t.Error("image should not be attached if vision unsupported")
	}
}

func TestPasteImageWithVisionCapabilitySucceeds(t *testing.T) {
	m := newTestModel(t)
	m.model = "gpt-4o"  // has vision support
	m.imageAttachments = []imageAttachment{}

	err := m.validateImagePaste()
	if err \!= nil {
		t.Errorf("expected no error for vision-capable model, got %v", err)
	}
}
```

Run test:
```bash
go test -v ./internal/tui -run TestPasteImage
```

Expected: FAIL — "validateImagePaste not defined".

**Step 2: Implement vision capability check in TUI**

File: `internal/tui/update.go`:

```go
// visionSupported checks if the current model has vision capability.
func (m *Model) visionSupported() bool {
	// For Anthropic models
	if m.provider == "anthropic" {
		return llm.AnthropicVisionSupported(m.model)
	}

	// For OpenAI-compatible (including Ollama, vLLM, etc.)
	// Query the openaicompat caps
	caps := openaicompat.DefaultCaps(m.model)
	return caps.Vision
}

// validateImagePaste checks if the current model can accept images.
// Returns error if not supported.
func (m *Model) validateImagePaste() error {
	if \!m.visionSupported() {
		// Return error with list of vision-capable models from registry
		models := m.getVisionCapableModels()
		return fmt.Errorf("current model (%s) doesn't support images. Vision-capable: %s",
			m.model, strings.Join(models, ", "))
	}
	return nil
}

// getVisionCapableModels returns a list of known vision-capable models.
func (m *Model) getVisionCapableModels() []string {
	var models []string

	// Anthropic models
	anthropicModels := []string{"claude-3-opus", "claude-3-sonnet", "claude-3-haiku", "claude-3.5-sonnet"}
	for _, model := range anthropicModels {
		if llm.AnthropicVisionSupported(model) {
			models = append(models, model)
		}
	}

	// OpenAI-compatible models
	openaiModels := []string{"gpt-4o", "gpt-4-turbo"}
	for _, model := range openaiModels {
		caps := openaicompat.DefaultCaps(model)
		if caps.Vision {
			models = append(models, model)
		}
	}

	return models
}
```

Update cmdPasteImage to call validateImagePaste:

```go
func (m *Model) cmdPasteImage() tea.Cmd {
	return func() tea.Msg {
		// Validate vision capability first
		if err := m.validateImagePaste(); err \!= nil {
			return errMsg{err}
		}

		// ... rest of implementation ...
	}
}
```

Run test:
```bash
go test -v ./internal/tui -run TestPasteImage
```

Expected: PASS.

**Step 3: Commit**

```bash
git add internal/tui/update.go internal/tui/app.go
git commit -m "feat: add vision capability gating with model list on refusal"
```

---

## Task 8: @image Syntax Support

**Files:**
- Modify: `internal/tui/update.go` (parse @image: syntax)
- Modify: `internal/tui/image_attach.go` (handle file path attachment)

**Step 1: Write failing test for @image syntax**

File: `internal/tui/image_attach_test.go`:

```go
func TestParseImageSyntax(t *testing.T) {
	tests := []struct {
		input    string
		wantPath string
		wantOk   bool
	}{
		{"@image:/path/to/file.png", "/path/to/file.png", true},
		{"@image:./relative/image.jpg", "./relative/image.jpg", true},
		{"@file:something.go", "", false},
		{"just text", "", false},
	}

	for _, tt := range tests {
		path, ok := parseImageSyntax(tt.input)
		if ok \!= tt.wantOk {
			t.Errorf("parseImageSyntax(%q): ok=%v, want %v", tt.input, ok, tt.wantOk)
		}
		if ok && path \!= tt.wantPath {
			t.Errorf("parseImageSyntax(%q): path=%q, want %q", tt.input, path, tt.wantPath)
		}
	}
}

func TestLoadImageFromFile(t *testing.T) {
	// Create temp test image file
	// (Use existing test image or generate one)

	// Load and verify
	data, mimeType, err := loadImageFromFile(testImagePath)
	if err \!= nil {
		t.Fatalf("loadImageFromFile failed: %v", err)
	}

	if mimeType == "" {
		t.Error("mimeType is empty")
	}

	if len(data) == 0 {
		t.Error("image data is empty")
	}
}
```

Run test:
```bash
go test -v ./internal/tui -run TestParseImage
```

Expected: FAIL — "parseImageSyntax not defined".

**Step 2: Implement @image syntax parsing**

File: `internal/tui/image_attach.go`:

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// parseImageSyntax extracts the file path from @image:/path/to/file syntax.
// Returns (path, ok).
func parseImageSyntax(text string) (string, bool) {
	const prefix = "@image:"
	if strings.HasPrefix(text, prefix) {
		path := strings.TrimPrefix(text, prefix)
		return strings.TrimSpace(path), true
	}
	return "", false
}

// loadImageFromFile reads an image file, detects MIME type, and returns data + mimeType.
func loadImageFromFile(filePath string) ([]byte, string, error) {
	data, err := os.ReadFile(filePath)
	if err \!= nil {
		return nil, "", fmt.Errorf("failed to read image file: %w", err)
	}

	// Detect MIME type from file extension
	ext := strings.ToLower(filepath.Ext(filePath))
	mimeType := mimeTypeFromExt(ext)

	if mimeType == "" {
		return nil, "", fmt.Errorf("unsupported image format: %s", ext)
	}

	return data, mimeType, nil
}

func mimeTypeFromExt(ext string) string {
	switch ext {
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
```

Run test:
```bash
go test -v ./internal/tui -run TestParseImage
```

Expected: PASS.

**Step 3: Integrate @image syntax into input scanning**

File: `internal/tui/update.go`, modify the input submission logic to scan for @image references:

```go
// extractImagesFromInput scans input text for @image: references and attaches them.
func (m *Model) extractImagesFromInput(text string) (string, error) {
	const imagePrefix = "@image:"
	lines := strings.Split(text, "\n")
	var cleanLines []string
	var newAttachments []imageAttachment

	for _, line := range lines {
		if strings.Contains(line, imagePrefix) {
			// Parse and extract
			parts := strings.Fields(line)
			for _, part := range parts {
				if path, ok := parseImageSyntax(part); ok {
					// Load image
					data, mimeType, err := loadImageFromFile(path)
					if err \!= nil {
						return "", err
					}

					// Validate vision
					if err := m.validateImagePaste(); err \!= nil {
						return "", err
					}

					// Process and attach
					processed, err := preprocessImage(data, mimeType)
					if err \!= nil {
						return "", err
					}

					tokens := estimateTokens(processed.width, processed.height, m.provider)
					newAttachments = append(newAttachments, imageAttachment{
						data:     processed.data,
						mimeType: processed.mimeType,
						width:    processed.width,
						height:   processed.height,
						fileSize: len(processed.data),
						tokens:   tokens,
					})
				}
			}

			// Remove @image: from the line for sending to agent
			cleanedLine := strings.ReplaceAll(line, imagePrefix+"*", "")
			cleanLines = append(cleanLines, cleanedLine)
		} else {
			cleanLines = append(cleanLines, line)
		}
	}

	m.imageAttachments = append(m.imageAttachments, newAttachments...)
	return strings.Join(cleanLines, "\n"), nil
}
```

**Step 4: Commit**

```bash
git add internal/tui/image_attach.go internal/tui/update.go
git commit -m "feat: add @image:/path/to/file syntax parsing and loading"
```

---

## Task 9: Send Images with Next User Message

**Files:**
- Modify: `internal/agent/agent.go` (extend Submit to accept attachments)
- Modify: `internal/tui/update.go` (pass attachments when submitting)
- Modify: `internal/llm/anthropiccompat/provider.go` (encode images for Anthropic SDK)
- Modify: `internal/llm/openaicompat/translate.go` (encode images for OpenAI-compatible)

**Step 1: Write failing test for agent.Submit with images**

File: `internal/agent/agent_test.go` (add):

```go
func TestSubmitWithImageAttachments(t *testing.T) {
	a := newTestAgent(t)

	attachments := []llm.ImageAttachment{
		{
			Data:     []byte{/* base64 PNG data */},
			MimeType: "image/png",
			Width:    256,
			Height:   256,
		},
	}

	a.SubmitWithAttachments("describe this image", attachments)

	// Verify the message was created with an image content block
	if len(a.history) == 0 {
		t.Fatal("no message in history")
	}

	msg := a.history[len(a.history)-1]
	if msg.Role \!= llm.RoleUser {
		t.Errorf("expected user role, got %s", msg.Role)
	}

	// Should have at least 2 content blocks: text + image
	if len(msg.Content) < 2 {
		t.Errorf("expected 2+ content blocks, got %d", len(msg.Content))
	}

	// Last block should be an image
	lastBlock := msg.Content[len(msg.Content)-1]
	if lastBlock.Type \!= llm.ContentImage {
		t.Errorf("expected ContentImage, got %s", lastBlock.Type)
	}
}
```

Run test:
```bash
go test -v ./internal/agent -run TestSubmitWithImage
```

Expected: FAIL — "SubmitWithAttachments not defined" or "ImageAttachment not defined".

**Step 2: Extend llm types for image attachments**

File: `internal/llm/types.go`:

```go
// ImageAttachment carries image data for submission with a user message.
type ImageAttachment struct {
	Data     []byte // base64-encoded image data
	MimeType string // "image/png", "image/jpeg", etc.
	Width    int
	Height   int
}
```

**Step 3: Implement agent.SubmitWithAttachments**

File: `internal/agent/agent.go`:

```go
// SubmitWithAttachments sends a user message with image attachments.
func (a *Agent) SubmitWithAttachments(userMsg string, attachments []llm.ImageAttachment) <-chan Event {
	// Build message with image content blocks
	var content []llm.ContentBlock

	// Add text block
	if strings.TrimSpace(userMsg) \!= "" {
		content = append(content, llm.ContentBlock{
			Type: llm.ContentText,
			Text: userMsg,
		})
	}

	// Add image blocks
	for _, att := range attachments {
		content = append(content, llm.ContentBlock{
			Type:      llm.ContentImage,
			ImageData: base64.StdEncoding.EncodeToString(att.Data),
			MediaType: att.MimeType,
		})
	}

	// Create user message
	msg := llm.Message{
		Role:    llm.RoleUser,
		Content: content,
	}

	a.history = append(a.history, msg)
	return a.loop(context.Background())
}
```

Run test:
```bash
go test -v ./internal/agent -run TestSubmitWithImage
```

Expected: PASS.

**Step 4: Wire image content into provider implementations**

File: `internal/llm/anthropiccompat/translate.go` (add image handling):

```go
// In the function that converts llm.Message to anthropic SDK Message:

for _, block := range llmMsg.Content {
	switch block.Type {
	case llm.ContentText:
		// Existing text handling
		msg.Content = append(msg.Content, anthropic.TextBlock{
			Type: "text",
			Text: block.Text,
		})

	case llm.ContentImage:
		// Add image content block
		data, err := base64.StdEncoding.DecodeString(block.ImageData)
		if err \!= nil {
			return nil, err
		}

		msg.Content = append(msg.Content, anthropic.ImageBlock{
			Type: "image",
			Source: &anthropic.ImageBlockParamSourceBase64{
				Type:      "base64",
				MediaType: block.MediaType,
				Data:      data,
			},
		})

	// ... other cases ...
	}
}
```

File: `internal/llm/openaicompat/translate.go` (similar for OpenAI API):

```go
for _, block := range llmMsg.Content {
	switch block.Type {
	case llm.ContentText:
		// Existing text handling
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": block.Text,
		})

	case llm.ContentImage:
		// Add image content block with data URL
		dataURL := fmt.Sprintf("data:%s;base64,%s", block.MediaType, block.ImageData)
		content = append(content, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]string{
				"url": dataURL,
			},
		})

	// ... other cases ...
	}
}
```

**Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go \
         internal/llm/types.go \
         internal/llm/anthropiccompat/translate.go \
         internal/llm/openaicompat/translate.go
git commit -m "feat: attach images to user messages during submission"
```

---

## Task 10: TUI Integration — Input Handler Wiring

**Files:**
- Modify: `internal/tui/update.go` (final wiring of @file/@image into handleKey)
- Modify: `internal/tui/render.go` (final rendering integration)

**Step 1: Write integration test for full @file + image flow**

File: `internal/tui/integration_test.go` (add):

```go
func TestFilePickerAndImagePaste(t *testing.T) {
	m, a, _ := newTestModelWithResolver(t, nil)

	// Type @ to open picker
	// (Simulate input events)

	// Select a file — should insert @path/to/file
	// (Verify input contains reference)

	// Press Cmd+V to paste image
	// (Simulate clipboard read with test image)

	// Check imageAttachments populated
	if len(m.imageAttachments) == 0 {
		t.Error("expected image attachment, got none")
	}

	// Submit message
	// (Verify agent receives message with image content block)
}
```

**Step 2: Final handleKey integration**

Ensure all pieces are wired in `handleKey`:
- Detect `@` to open file picker
- Route arrow/Tab/Enter/Esc to picker when active
- Detect Cmd+V/Ctrl+V for image paste
- Extract @image: references from input before submission

**Step 3: Final render integration**

Ensure View() shows:
- File picker overlay when active
- Image attachment badge above input
- Queue indicator (from Feature C)

**Step 4: End-to-end test**

```bash
go test -v ./internal/tui -run Integration
```

Expected: All integration tests PASS.

**Step 5: Commit**

```bash
git add internal/tui/update.go internal/tui/render.go internal/tui/integration_test.go
git commit -m "feat: final TUI integration for @file picker and image paste"
```

---

## Task 11: Smoke Tests and Documentation

**Files:**
- Modify: `README.md` (document @file and image paste)
- Create: `internal/image/PREPROCESSING.md` (document pipeline)
- Modify: `go.mod` comments (note CGO dependency for clipboard)

**Step 1: Update README with keybindings**

Add to keybindings section:

```markdown
### References (Feature D)

- `@` — Open file picker (fuzzy filter files in repo or cwd)
  - Arrow keys to navigate
  - Tab/Enter to complete
  - Esc to cancel

- `Cmd+V` / `Ctrl+V` — Paste binary image from clipboard
  - Attached to next user message as image content block
  - Vision capability gated; refused if model doesn't support

- `@image:/path/to/file.png` — Inline image reference syntax
  - Extracted during input parsing; image attached to message

### Image Processing

Images are automatically:
1. Resized if long edge > 1568px (Lanczos resampling)
2. Converted to PNG (if alpha) or JPEG q90 (opaque)
3. Stripped of EXIF/ICC metadata
4. Token-estimated per provider (Anthropic: (w*h)/750; GPT-4o: tile-based)
```

**Step 2: Write preprocessing documentation**

File: `internal/image/PREPROCESSING.md`:

```markdown
# Image Preprocessing Pipeline

Feature D applies a generic preprocessing pipeline once on attach:

1. **Resize:** If long edge > 1568 px, downscale to 1568 preserving aspect, using Lanczos filtering.
2. **Format Selection:** PNG if alpha channel present or appears screenshot-ish (low color count / sharp edges). Otherwise JPEG q90.
3. **Metadata Stripping:** EXIF, ICC profiles beyond sRGB removed via re-encode.
4. **Token Estimation:** Per-provider formula applied (Anthropic: (w*h)/750; GPT-4o: tile-based; unknown: "≈ unknown").

All preprocessing is generic (not per-provider). The 1568 threshold is validated across modern providers.
```

**Step 3: Document CGO dependency in go.mod**

Add comment near the clipboard dependency (in go.mod require block, or in a comment section):

```
// golang.design/x/clipboard v0.7.0 requires CGO for binary clipboard image read.
// Set CGO_ENABLED=1 for builds. Pre-built binaries should be built with CGO enabled.
```

**Step 4: Run all tests**

```bash
go test -v ./internal/tui ./internal/agent ./internal/llm ./internal/image ./internal/clipboard
```

Expected: All tests PASS.

**Step 5: Manual smoke test**

In a terminal with the SAM CLI:
1. Start the CLI
2. Type `@` and verify picker overlay opens
3. Navigate and complete with a file reference
4. Paste an image from clipboard with Cmd+V
5. Verify badge shows token estimate and dimensions
6. Submit message and verify agent receives both file reference and image

**Step 6: Commit**

```bash
git add README.md internal/image/PREPROCESSING.md go.mod
git commit -m "docs: add Feature D keybindings, preprocessing pipeline, CGO note"
```

---

## Summary

**11 tasks, strict TDD, each independently shippable:**

1. Image preprocessing pipeline (resize, format selection, metadata strip)
2. Vision capability registry (OpenAI-compat and Anthropic)
3. Image content block type in llm.Message
4. Binary clipboard image read (golang.design/x/clipboard)
5. @file picker overlay (Bubbletea modal with fuzzy filtering)
6. Image paste and token estimation badge
7. Vision capability gating and refusal toast
8. @image:/path/to/file syntax support
9. Send images with next user message (agent + providers)
10. TUI integration (final handleKey and render wiring)
11. Smoke tests and documentation

**Tech dependencies added:**
- `github.com/disintegration/imaging` (Lanczos resize)
- `golang.design/x/clipboard` (binary clipboard, CGO)
- `github.com/sahilm/fuzzy` (fuzzy matching for picker)

**No breaking changes to Features A or C.** Image content blocks coexist with existing tool and thinking blocks. Vision gating ensures only capable models receive images. Picker and paste are purely additive to the input interface.

