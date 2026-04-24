package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadToolIsParallelSafe(t *testing.T) {
	tool := NewRead(NewReadTracker(), nil, "")
	if !tool.ParallelSafe() {
		t.Error("Read must be ParallelSafe")
	}
}

func TestReadRelativePath(t *testing.T) {
	tracker := NewReadTracker()
	tool := NewRead(tracker, nil, "test")

	result, err := tool.Run(context.Background(), []byte(`{"file_path": "relative/path.go"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for relative path")
	}
}

func TestReadNonExistent(t *testing.T) {
	tracker := NewReadTracker()
	tool := NewRead(tracker, nil, "test")

	result, err := tool.Run(context.Background(), []byte(`{"file_path": "/nonexistent/file.go"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent file")
	}
}

func TestReadSuccess(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	tracker := NewReadTracker()
	tool := NewRead(tracker, nil, "test")

	result, err := tool.Run(context.Background(), []byte(`{"file_path": "`+tmpFile+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Output)
	}
	if len(result.Output) == 0 {
		t.Error("expected output")
	}

	// Verify tracker was updated
	if !tracker.Seen(tmpFile) {
		t.Error("expected file to be tracked as read")
	}
}

func TestReadOffsetLimit(t *testing.T) {
	// Create temp file with many lines
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, "line"+string(rune('0'+i%10)))
	}
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	for i := 11; i <= 100; i++ {
		content += "line" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + "\n"
	}
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	tracker := NewReadTracker()
	tool := NewRead(tracker, nil, "test")

	// Read with offset 3, limit 5
	result, err := tool.Run(context.Background(), []byte(`{"file_path": "`+tmpFile+`", "offset": 3, "limit": 5}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Output)
	}
	_ = lines
}

func TestReadRTKDefaultWindowUsesRTK(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(tmp, []byte("a\nb\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rt := &fakeRTK{enabled: true, readOut: []byte("1 | a\n2 | b")}
	tracker := NewReadTracker()
	tool := NewRead(tracker, rt, "test")

	r, err := tool.Run(context.Background(), []byte(`{"file_path":"`+tmp+`"}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if !rt.readCalled {
		t.Fatal("rtk.Read must be called for default window")
	}
	if r.Output != "1 | a\n2 | b" {
		t.Fatalf("expected verbatim rtk output, got %q", r.Output)
	}
	if !tracker.Seen(tmp) {
		t.Fatal("tracker must be marked on rtk path")
	}
}

func TestReadRTKRawBypasses(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(tmp, []byte("a\nb\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rt := &fakeRTK{enabled: true, readOut: []byte("never")}
	tool := NewRead(NewReadTracker(), rt, "test")

	r, err := tool.Run(context.Background(), []byte(`{"file_path":"`+tmp+`","raw":true}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if rt.readCalled {
		t.Fatal("rtk must not be called when raw=true")
	}
	if !strings.Contains(r.Output, "a") {
		t.Fatalf("expected native output, got %q", r.Output)
	}
}

func TestReadRTKExplicitOffsetBypasses(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(tmp, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rt := &fakeRTK{enabled: true, readOut: []byte("never")}
	tool := NewRead(NewReadTracker(), rt, "test")

	r, err := tool.Run(context.Background(), []byte(`{"file_path":"`+tmp+`","offset":2}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if rt.readCalled {
		t.Fatal("rtk must not be called with explicit offset")
	}
	if strings.Contains(r.Output, "\ta\n") {
		t.Fatalf("expected offset to skip first line, got %q", r.Output)
	}
}

func TestReadRTKExplicitLimitBypasses(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(tmp, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rt := &fakeRTK{enabled: true, readOut: []byte("never")}
	tool := NewRead(NewReadTracker(), rt, "test")

	_, err := tool.Run(context.Background(), []byte(`{"file_path":"`+tmp+`","limit":1}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if rt.readCalled {
		t.Fatal("rtk must not be called with explicit limit")
	}
}

func TestReadRTKDisabledUsesNative(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(tmp, []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rt := &fakeRTK{enabled: false, readOut: []byte("never")}
	tool := NewRead(NewReadTracker(), rt, "test")

	_, err := tool.Run(context.Background(), []byte(`{"file_path":"`+tmp+`"}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if rt.readCalled {
		t.Fatal("rtk must not be called when disabled")
	}
}

func TestReadRTKError(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(tmp, []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rt := &fakeRTK{enabled: true, readErr: errors.New("rtk crashed")}
	tool := NewRead(NewReadTracker(), rt, "test")

	r, err := tool.Run(context.Background(), []byte(`{"file_path":"`+tmp+`"}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if !r.IsError {
		t.Fatal("expected IsError on rtk failure")
	}
	if !strings.Contains(r.Output, "rtk read failed") {
		t.Fatalf("expected rtk error message, got %q", r.Output)
	}
}

func TestReadBinaryFile(t *testing.T) {
	// Create binary file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "binary.bin")
	binaryContent := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	if err := os.WriteFile(tmpFile, binaryContent, 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	tracker := NewReadTracker()
	tool := NewRead(tracker, nil, "test")

	result, err := tool.Run(context.Background(), []byte(`{"file_path": "`+tmpFile+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for binary file")
	}
}
