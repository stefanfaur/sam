package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func runBashTool(t *testing.T, cwd string, in string) Result {
	t.Helper()
	r, err := NewBash(cwd).Run(context.Background(), []byte(in))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	return r
}

func TestBashStdout(t *testing.T) {
	r := runBashTool(t, t.TempDir(), `{"command":"echo hi"}`)
	if r.IsError {
		t.Fatalf("unexpected error: %s", r.Output)
	}
	if !strings.Contains(r.Output, "hi") {
		t.Fatalf("missing stdout: %q", r.Output)
	}
}

func TestBashNonZeroExit(t *testing.T) {
	r := runBashTool(t, t.TempDir(), `{"command":"exit 7"}`)
	if r.IsError {
		t.Fatalf("non-zero exit should not set IsError, got: %s", r.Output)
	}
	if !strings.Contains(r.Output, "<exit>7</exit>") {
		t.Fatalf("missing exit tag: %q", r.Output)
	}
}

func TestBashStderr(t *testing.T) {
	r := runBashTool(t, t.TempDir(), `{"command":"echo oops 1>&2"}`)
	if !strings.Contains(r.Output, "<stderr>") || !strings.Contains(r.Output, "oops") {
		t.Fatalf("stderr missing: %q", r.Output)
	}
}

func TestBashCwd(t *testing.T) {
	dir := t.TempDir()
	r := runBashTool(t, dir, `{"command":"pwd"}`)
	if !strings.Contains(r.Output, dir) {
		t.Fatalf("wrong cwd: %q want %q", r.Output, dir)
	}
}

func TestBashTimeout(t *testing.T) {
	start := time.Now()
	r := runBashTool(t, t.TempDir(), `{"command":"sleep 5","timeout_ms":200}`)
	elapsed := time.Since(start)
	if !r.IsError {
		t.Fatal("expected IsError on timeout")
	}
	if !strings.Contains(r.Output, "<timeout>true</timeout>") {
		t.Fatalf("missing timeout tag: %q", r.Output)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("timeout did not kill process: elapsed=%v", elapsed)
	}
}

func TestBashTruncation(t *testing.T) {
	// Emit well over 30KiB.
	r := runBashTool(t, t.TempDir(), `{"command":"yes x | head -c 200000"}`)
	if !strings.Contains(r.Output, "<truncated>true</truncated>") {
		t.Fatalf("missing truncation tag: len=%d", len(r.Output))
	}
}

func TestBashEmptyCommand(t *testing.T) {
	r := runBashTool(t, t.TempDir(), `{"command":""}`)
	if !r.IsError {
		t.Fatal("empty command should error")
	}
}
