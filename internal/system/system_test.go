package system

import (
	"strings"
	"testing"
)

func TestDefaultDir_SAMHome(t *testing.T) {
	t.Setenv("SAM_HOME", "/tmp/foo")
	got := DefaultDir()
	if got != "/tmp/foo/system" {
		t.Fatalf("DefaultDir() = %q, want %q", got, "/tmp/foo/system")
	}
}

func TestDefaultDir_DefaultHome(t *testing.T) {
	t.Setenv("SAM_HOME", "")
	got := DefaultDir()
	if !strings.HasSuffix(got, "/.sam/system") && !strings.HasSuffix(got, ".sam/system") {
		t.Fatalf("DefaultDir() = %q, want suffix .sam/system", got)
	}
}

func TestEmbeddedPrompt_NonEmpty(t *testing.T) {
	p := EmbeddedPrompt()
	if p == "" {
		t.Fatal("EmbeddedPrompt() empty")
	}
	if !strings.Contains(p, "CAVEMAN SPEECH") {
		t.Fatalf("prompt missing CAVEMAN SPEECH marker: %q", p)
	}
}

func TestEmbeddedToolDescription_AllFour(t *testing.T) {
	cases := map[string]string{
		"Read":  "Read the contents of a file from disk. Returns the file contents with line numbers. Use this when you need to see the content of a file. Supports offset and limit parameters for large files.",
		"Write": "Write content to a file. The file must be absolute path.",
		"Edit":  "Edit a file by replacing exact text. Must Read file first.",
		"Bash":  "Execute a shell command via bash -c. Stdout, stderr, and a non-zero exit code are returned in the output. Use for anything that needs the shell.",
	}
	for name, want := range cases {
		got := EmbeddedToolDescription(name)
		if got != want {
			t.Errorf("EmbeddedToolDescription(%q) mismatch:\n got: %q\nwant: %q", name, got, want)
		}
	}
}
