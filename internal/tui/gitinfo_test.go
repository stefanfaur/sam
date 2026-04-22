package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

func TestProbeGitNonRepo(t *testing.T) {
	dir := t.TempDir()
	g := probeGit(dir)
	if g.branch != "" {
		t.Errorf("non-repo should have empty branch, got %q", g.branch)
	}
	if g.dirty {
		t.Error("non-repo should not be dirty")
	}
}

func TestProbeGitCleanRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", "main")
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "user.name", "Test")
	f := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(f, []byte("hi"), 0o644)
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")

	g := probeGit(dir)
	if g.branch != "main" {
		t.Errorf("branch = %q, want main", g.branch)
	}
	if g.dirty {
		t.Error("clean repo should not be dirty")
	}
}

func TestProbeGitDirtyRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", "main")
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "user.name", "Test")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644)
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new"), 0o644)

	g := probeGit(dir)
	if !g.dirty {
		t.Error("expected dirty repo")
	}
}
