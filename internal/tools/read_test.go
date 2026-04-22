package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRelativePath(t *testing.T) {
	tracker := NewReadTracker()
	tool := NewRead(tracker)
	
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
	tool := NewRead(tracker)
	
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
	tool := NewRead(tracker)
	
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
	tool := NewRead(tracker)
	
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

func TestReadBinaryFile(t *testing.T) {
	// Create binary file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "binary.bin")
	binaryContent := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	if err := os.WriteFile(tmpFile, binaryContent, 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	
	tracker := NewReadTracker()
	tool := NewRead(tracker)
	
	result, err := tool.Run(context.Background(), []byte(`{"file_path": "`+tmpFile+`"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for binary file")
	}
}
