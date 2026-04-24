package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteToolIsNotParallelSafe(t *testing.T) {
	tool := NewWrite(NewReadTracker(), "")
	if tool.ParallelSafe() {
		t.Error("Write must not be ParallelSafe")
	}
}

func TestWriteRelativePath(t *testing.T) {
	tool := NewWrite(NewReadTracker(), "test")
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"rel.txt","content":"x"}`))
	if !r.IsError {
		t.Fatal("expected error for relative path")
	}
}

func TestWriteNewFile(t *testing.T) {
	tr := NewReadTracker()
	tool := NewWrite(tr, "test")
	p := filepath.Join(t.TempDir(), "new.txt")
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"`+p+`","content":"hello"}`))
	if r.IsError {
		t.Fatalf("write new file failed: %s", r.Output)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "hello" {
		t.Fatalf("wrote wrong content: %q", string(b))
	}
	if !tr.Seen(p) {
		t.Fatal("write should mark path as seen")
	}
}

func TestWriteRefuseOverwriteWithoutRead(t *testing.T) {
	tr := NewReadTracker()
	tool := NewWrite(tr, "test")
	p := filepath.Join(t.TempDir(), "exists.txt")
	if err := os.WriteFile(p, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"`+p+`","content":"new"}`))
	if !r.IsError {
		t.Fatal("expected error overwriting unread file")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "old" {
		t.Fatal("file should not have been overwritten")
	}
}

func TestWriteOverwriteAfterRead(t *testing.T) {
	tr := NewReadTracker()
	tool := NewWrite(tr, "test")
	p := filepath.Join(t.TempDir(), "exists.txt")
	if err := os.WriteFile(p, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	tr.Mark(p)
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"`+p+`","content":"new"}`))
	if r.IsError {
		t.Fatalf("unexpected error: %s", r.Output)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "new" {
		t.Fatalf("expected 'new', got %q", string(b))
	}
}

func TestWriteMissingParent(t *testing.T) {
	tool := NewWrite(NewReadTracker(), "test")
	p := filepath.Join(t.TempDir(), "nope", "x.txt")
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"`+p+`","content":"x"}`))
	if !r.IsError {
		t.Fatal("expected error when parent missing")
	}
}
