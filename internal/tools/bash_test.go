package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func runBashTool(t *testing.T, cwd string, in string) Result {
	t.Helper()
	r, err := NewBash(cwd, nil, "test").Run(context.Background(), []byte(in))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	return r
}

// fakeRTK implements RTKClient for tool tests.
type fakeRTK struct {
	enabled        bool
	rewriteOut     string
	rewriteSupport bool
	rewriteErr     error
	rewriteCalled  bool
	readOut        []byte
	readErr        error
	readCalled     bool
}

func (f *fakeRTK) Enabled() bool { return f.enabled }
func (f *fakeRTK) Rewrite(ctx context.Context, cmd string) (string, bool, error) {
	f.rewriteCalled = true
	return f.rewriteOut, f.rewriteSupport, f.rewriteErr
}
func (f *fakeRTK) Read(ctx context.Context, path string) ([]byte, error) {
	f.readCalled = true
	return f.readOut, f.readErr
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

func TestBashRawBypassesRTK(t *testing.T) {
	rt := &fakeRTK{enabled: true, rewriteOut: "echo replaced", rewriteSupport: true}
	r, err := NewBash(t.TempDir(), rt, "test").Run(context.Background(),
		[]byte(`{"command":"echo hi","raw":true}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if rt.rewriteCalled {
		t.Fatal("rtk rewrite must not be called when raw=true")
	}
	if !strings.Contains(r.Output, "hi") {
		t.Fatalf("expected original stdout, got %q", r.Output)
	}
	if r.Rewritten != "" {
		t.Fatalf("Rewritten must be empty when raw=true, got %q", r.Rewritten)
	}
}

func TestBashRTKDisabledSkipsRewrite(t *testing.T) {
	rt := &fakeRTK{enabled: false, rewriteOut: "echo replaced", rewriteSupport: true}
	r, err := NewBash(t.TempDir(), rt, "test").Run(context.Background(),
		[]byte(`{"command":"echo hi"}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if rt.rewriteCalled {
		t.Fatal("rtk rewrite must not be called when disabled")
	}
	if !strings.Contains(r.Output, "hi") {
		t.Fatalf("expected original stdout, got %q", r.Output)
	}
}

func TestBashRTKRewriteReplacesCommand(t *testing.T) {
	rt := &fakeRTK{enabled: true, rewriteOut: "echo rewritten", rewriteSupport: true}
	r, err := NewBash(t.TempDir(), rt, "test").Run(context.Background(),
		[]byte(`{"command":"echo original"}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if !rt.rewriteCalled {
		t.Fatal("rtk rewrite must be called")
	}
	if !strings.Contains(r.Output, "rewritten") {
		t.Fatalf("expected rewritten command to execute, got %q", r.Output)
	}
	if r.Rewritten != "echo rewritten" {
		t.Fatalf("Rewritten should carry the rewrite, got %q", r.Rewritten)
	}
}

func TestBashRTKUnsupportedKeepsOriginal(t *testing.T) {
	rt := &fakeRTK{enabled: true, rewriteSupport: false}
	r, err := NewBash(t.TempDir(), rt, "test").Run(context.Background(),
		[]byte(`{"command":"echo original"}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if !rt.rewriteCalled {
		t.Fatal("rtk rewrite must be called")
	}
	if !strings.Contains(r.Output, "original") {
		t.Fatalf("expected original stdout, got %q", r.Output)
	}
	if r.Rewritten != "" {
		t.Fatalf("Rewritten must be empty when rtk unsupported, got %q", r.Rewritten)
	}
}

func TestBashRTKRewriteError(t *testing.T) {
	rt := &fakeRTK{enabled: true, rewriteErr: errors.New("rtk crashed")}
	r, err := NewBash(t.TempDir(), rt, "test").Run(context.Background(),
		[]byte(`{"command":"echo original"}`))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if !r.IsError {
		t.Fatal("expected IsError when rtk rewrite fails")
	}
	if !strings.Contains(r.Output, "rtk rewrite failed") {
		t.Fatalf("expected rtk error message, got %q", r.Output)
	}
}
