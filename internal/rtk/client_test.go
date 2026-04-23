package rtk

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// scriptedExecer returns exec.Cmd backed by `bash -c` so Run() actually
// produces scripted stdout/stderr/exit codes.
type scriptedExecer struct {
	versionStdout string
	versionExit   int
	rewriteStdout string
	rewriteExit   int
	readStdout    string
	readExit      int
	// readScript, if non-empty, is used instead of readStdout to generate
	// arbitrarily large output via shell (avoids ARG_MAX limits).
	readScript string
	delay      time.Duration
	calls      [][]string
}

func (s *scriptedExecer) Command(ctx context.Context, name string, arg ...string) *exec.Cmd {
	s.calls = append(s.calls, append([]string{name}, arg...))

	var script string
	if s.delay > 0 {
		script = "sleep " + durSecs(s.delay) + ";"
	}
	switch {
	case len(arg) == 1 && arg[0] == "--version":
		script += quoteEcho(s.versionStdout) + "; exit " + itoa(s.versionExit)
	case len(arg) >= 1 && arg[0] == "rewrite":
		script += quoteEcho(s.rewriteStdout) + "; exit " + itoa(s.rewriteExit)
	case len(arg) >= 1 && arg[0] == "read":
		if s.readScript != "" {
			script += s.readScript + "; exit " + itoa(s.readExit)
		} else {
			script += quoteEcho(s.readStdout) + "; exit " + itoa(s.readExit)
		}
	default:
		script += "exit 0"
	}
	return exec.CommandContext(ctx, "bash", "-c", script)
}

func quoteEcho(s string) string {
	return "printf '%s' " + shellQuote(s)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var out []byte
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	if neg {
		out = append([]byte{'-'}, out...)
	}
	return string(out)
}

func durSecs(d time.Duration) string {
	ms := int(d / time.Millisecond)
	return itoa(ms/1000) + "." + itoa(ms%1000)
}

func TestDetectModeOff(t *testing.T) {
	c := New(ModeOff)
	c.execer = &scriptedExecer{}
	if err := c.Detect(context.Background()); err != nil {
		t.Fatalf("Detect mode=off should not error: %v", err)
	}
	if c.Enabled() {
		t.Fatal("expected disabled with mode=off")
	}
}

func TestDetectModeAutoMissing(t *testing.T) {
	c := New(ModeAuto)
	c.execer = &scriptedExecer{versionExit: 127}
	if err := c.Detect(context.Background()); err != nil {
		t.Fatalf("auto missing should not error: %v", err)
	}
	if c.Enabled() {
		t.Fatal("expected disabled when auto + missing")
	}
}

func TestDetectModeOnMissing(t *testing.T) {
	c := New(ModeOn)
	c.execer = &scriptedExecer{versionExit: 127}
	if err := c.Detect(context.Background()); err == nil {
		t.Fatal("expected error when mode=on + missing")
	}
}

func TestDetectModeAutoFound(t *testing.T) {
	c := New(ModeAuto)
	c.execer = &scriptedExecer{versionStdout: "rtk 0.36.0\n", versionExit: 0}
	if err := c.Detect(context.Background()); err != nil {
		t.Fatalf("auto found should not error: %v", err)
	}
	if !c.Enabled() {
		t.Fatal("expected enabled when binary found")
	}
	if c.Version() != "rtk 0.36.0" {
		t.Fatalf("expected version 'rtk 0.36.0', got %q", c.Version())
	}
}

func TestRewriteWhenDisabled(t *testing.T) {
	c := New(ModeOff)
	c.execer = &scriptedExecer{}
	out, supported, err := c.Rewrite(context.Background(), "git status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if supported || out != "" {
		t.Fatal("disabled client should return ('', false, nil)")
	}
}

func TestRewriteExit0(t *testing.T) {
	c := New(ModeOn)
	c.execer = &scriptedExecer{rewriteStdout: "git log -1 --format=%h", rewriteExit: 0}
	c.enabled = true
	out, supported, err := c.Rewrite(context.Background(), "git status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !supported {
		t.Fatal("expected supported=true on exit 0")
	}
	if out != "git log -1 --format=%h" {
		t.Fatalf("expected rewritten command, got %q", out)
	}
}

func TestRewriteExit1(t *testing.T) {
	c := New(ModeOn)
	c.execer = &scriptedExecer{rewriteExit: 1}
	c.enabled = true
	_, supported, err := c.Rewrite(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("exit 1 must not error: %v", err)
	}
	if supported {
		t.Fatal("exit 1 means unsupported")
	}
}

func TestRewriteExitNonZero(t *testing.T) {
	c := New(ModeOn)
	c.execer = &scriptedExecer{rewriteExit: 2}
	c.enabled = true
	_, supported, err := c.Rewrite(context.Background(), "git status")
	if err == nil {
		t.Fatal("expected error on exit code != 0,1")
	}
	if supported {
		t.Fatal("supported must be false when error")
	}
}

func TestRewriteEmptyOutputErrors(t *testing.T) {
	c := New(ModeOn)
	c.execer = &scriptedExecer{rewriteStdout: "", rewriteExit: 0}
	c.enabled = true
	_, supported, err := c.Rewrite(context.Background(), "git status")
	if err == nil {
		t.Fatal("empty stdout on exit 0 should error")
	}
	if supported {
		t.Fatal("supported must be false on error")
	}
}

func TestRewriteContextTimeout(t *testing.T) {
	c := New(ModeOn)
	c.execer = &scriptedExecer{delay: 3 * time.Second, rewriteStdout: "x"}
	c.enabled = true
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _, err := c.Rewrite(ctx, "git status")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestReadWhenDisabled(t *testing.T) {
	c := New(ModeOff)
	c.execer = &scriptedExecer{}
	if _, err := c.Read(context.Background(), "/some/file"); err == nil {
		t.Fatal("disabled Read must error")
	}
}

func TestReadSuccess(t *testing.T) {
	c := New(ModeOn)
	c.execer = &scriptedExecer{readStdout: "1 | line1\n2 | line2", readExit: 0}
	c.enabled = true
	out, err := c.Read(context.Background(), "/tmp/test.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "1 | line1\n2 | line2" {
		t.Fatalf("expected verbatim stdout, got %q", string(out))
	}
}

func TestReadNonZeroErrors(t *testing.T) {
	c := New(ModeOn)
	c.execer = &scriptedExecer{readExit: 3}
	c.enabled = true
	if _, err := c.Read(context.Background(), "/tmp/test.txt"); err == nil {
		t.Fatal("expected error on non-zero exit")
	}
}

func TestReadExceedsMaxBytes(t *testing.T) {
	// Emit rtkReadMaxBytes+1 bytes via shell generator to dodge ARG_MAX.
	n := rtkReadMaxBytes + 1
	script := "yes x | head -c " + itoa(n)
	c := New(ModeOn)
	c.execer = &scriptedExecer{readScript: script, readExit: 0}
	c.enabled = true
	_, err := c.Read(context.Background(), "/tmp/test.txt")
	if err == nil {
		t.Fatal("expected cap-exceeded error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected 'exceeds' in error, got %v", err)
	}
}
