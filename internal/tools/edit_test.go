package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func editSetup(t *testing.T, body string) (string, *ReadTracker) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	tr := NewReadTracker()
	tr.Mark(p)
	return p, tr
}

func TestEditRequiresRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	_ = os.WriteFile(p, []byte("hi"), 0644)
	tool := NewEdit(NewReadTracker(), "test")
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"`+p+`","old_string":"hi","new_string":"bye"}`))
	if !r.IsError {
		t.Fatal("expected error when file not read")
	}
}

func TestEditUniqueMatch(t *testing.T) {
	p, tr := editSetup(t, "hello world\n")
	tool := NewEdit(tr, "test")
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"`+p+`","old_string":"hello","new_string":"howdy"}`))
	if r.IsError {
		t.Fatalf("unexpected error: %s", r.Output)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "howdy world\n" {
		t.Fatalf("wrong content: %q", string(b))
	}
}

func TestEditMultipleMatchesError(t *testing.T) {
	p, tr := editSetup(t, "aa aa aa")
	tool := NewEdit(tr, "test")
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"`+p+`","old_string":"aa","new_string":"b"}`))
	if !r.IsError {
		t.Fatal("expected multi-match error")
	}
}

func TestEditReplaceAll(t *testing.T) {
	p, tr := editSetup(t, "aa aa aa")
	tool := NewEdit(tr, "test")
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"`+p+`","old_string":"aa","new_string":"b","replace_all":true}`))
	if r.IsError {
		t.Fatalf("unexpected error: %s", r.Output)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "b b b" {
		t.Fatalf("got %q", string(b))
	}
}

func TestEditNoOpRejected(t *testing.T) {
	p, tr := editSetup(t, "x")
	tool := NewEdit(tr, "test")
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"`+p+`","old_string":"x","new_string":"x"}`))
	if !r.IsError {
		t.Fatal("expected no-op error")
	}
}

func TestEditNotFound(t *testing.T) {
	p, tr := editSetup(t, "hello")
	tool := NewEdit(tr, "test")
	r, _ := tool.Run(context.Background(), []byte(`{"file_path":"`+p+`","old_string":"zzz","new_string":"q"}`))
	if !r.IsError {
		t.Fatal("expected not-found error")
	}
}
