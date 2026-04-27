package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/stefanfaur/sam/internal/checkpoint"
	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

// initRepoForIntegration creates a fresh git repo with one initial commit.
func initRepoForIntegration(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	repo, err := git.PlainInit(tmp, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	wt.Add("README.md")
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}
	return tmp
}

// rewindEndTurnScript is a one-iteration provider script.
func rewindEndTurnScript() []llm.StreamEvent {
	return []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	}
}

// TestRewindIntegration_MultiTurnRestore drives 3 graceful turns through the
// agent, mutating the working tree between turns to simulate agent edits,
// then exercises every public checkpoint API: snapshot existence, ListPaths,
// Restore (conv-only no paths, then conv+code), and OverlapPaths.
func TestRewindIntegration_MultiTurnRestore(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	root := initRepoForIntegration(t)
	mgr, err := checkpoint.New(root, "integration-1")
	if err != nil {
		t.Fatal(err)
	}

	prov := fake.New(rewindEndTurnScript(), rewindEndTurnScript(), rewindEndTurnScript())
	a := New(Options{
		Provider:   prov,
		Tools:      tools.NewRegistry(),
		Policy:     policy.AllowAll(),
		LaunchDir:  root,
		SessionID:  "integration-1",
		Checkpoint: mgr,
	})
	a.Start()
	defer a.Close()

	// Turn 1: agent creates feature.go.
	if err := os.WriteFile(filepath.Join(root, "feature.go"), []byte("package x // v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := collectEvents(a.Submit(context.Background(), "create feature")); err != nil {
		t.Fatal(err)
	}

	// Turn 2: agent edits feature.go and adds helper.go.
	if err := os.WriteFile(filepath.Join(root, "feature.go"), []byte("package x // v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "helper.go"), []byte("package x // helper\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := collectEvents(a.Submit(context.Background(), "expand feature")); err != nil {
		t.Fatal(err)
	}

	// Turn 3: agent further edits feature.go and adds util.go.
	if err := os.WriteFile(filepath.Join(root, "feature.go"), []byte("package x // v3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "util.go"), []byte("package x // util\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := collectEvents(a.Submit(context.Background(), "polish")); err != nil {
		t.Fatal(err)
	}

	// All three snapshots should exist.
	for n := 1; n <= 3; n++ {
		if mgr.SnapshotSHA(n) == "" {
			t.Fatalf("snapshot %d missing", n)
		}
	}
	if got, _ := mgr.MaxTurn(); got != 3 {
		t.Fatalf("MaxTurn = %d, want 3", got)
	}

	// Sidecar must contain 3 turns with snapshot SHAs and stop reasons.
	sr, err := LoadSession("integration-1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(sr.Turns) != 3 {
		t.Fatalf("turns = %d", len(sr.Turns))
	}
	for i, turn := range sr.Turns {
		if turn.SnapshotSHA == "" {
			t.Errorf("turn %d: snapshot SHA empty", i+1)
		}
		if turn.StoppedBy != "end_turn" {
			t.Errorf("turn %d: stopped_by = %q", i+1, turn.StoppedBy)
		}
	}

	// ListPaths(1) should report the union of paths different between turn 1
	// and turn 3 — feature.go, helper.go, util.go.
	paths, err := mgr.ListPaths(1)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"feature.go": true, "helper.go": true, "util.go": true}
	if len(paths) != 3 {
		t.Fatalf("ListPaths = %v", paths)
	}
	for _, p := range paths {
		if !want[p] {
			t.Errorf("unexpected path %q", p)
		}
	}

	// Conv-only restore: TUI passes nil paths → checkpoint.Restore is a no-op,
	// working tree stays at the latest state.
	if err := mgr.Restore(1, nil); err != nil {
		t.Fatalf("conv-only restore: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "feature.go")); string(data) != "package x // v3\n" {
		t.Fatalf("conv-only must not touch working tree: %q", data)
	}

	// Now actually restore code (conv+code mode).
	if err := mgr.Restore(1, paths); err != nil {
		t.Fatalf("code restore: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "feature.go")); string(data) != "package x // v1\n" {
		t.Fatalf("feature.go after restore: %q", data)
	}
	for _, p := range []string{"helper.go", "util.go"} {
		if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
			t.Errorf("%s should be deleted after rewind to turn 1, err=%v", p, err)
		}
	}
}

// TestRewindIntegration_OverlapDetection verifies user edits made AFTER the
// last snapshot are flagged so the TUI can warn before clobber.
func TestRewindIntegration_OverlapDetection(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := initRepoForIntegration(t)
	mgr, _ := checkpoint.New(root, "overlap-int")

	prov := fake.New(rewindEndTurnScript(), rewindEndTurnScript())
	a := New(Options{
		Provider: prov, Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		LaunchDir: root, SessionID: "overlap-int", Checkpoint: mgr,
	})
	a.Start()
	defer a.Close()

	os.WriteFile(filepath.Join(root, "a.go"), []byte("v1"), 0o644)
	collectEvents(a.Submit(context.Background(), "t1"))

	os.WriteFile(filepath.Join(root, "a.go"), []byte("v2"), 0o644)
	os.WriteFile(filepath.Join(root, "b.go"), []byte("b"), 0o644)
	collectEvents(a.Submit(context.Background(), "t2"))

	// Simulate user editing a.go by hand after the agent's last turn.
	os.WriteFile(filepath.Join(root, "a.go"), []byte("USER-WAS-HERE"), 0o644)

	paths, _ := mgr.ListPaths(1)
	overlaps, err := mgr.OverlapPaths(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlaps) != 1 || overlaps[0] != "a.go" {
		t.Fatalf("overlaps = %v, want [a.go]", overlaps)
	}
}

// TestRewindIntegration_CancelDoesNotSnapshot pins the Feature A contract:
// a turn the user aborts mid-flight must not produce a checkpoint ref or
// sidecar entry (otherwise rewind targets cancelled / partial states).
func TestRewindIntegration_CancelDoesNotSnapshot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := initRepoForIntegration(t)
	mgr, _ := checkpoint.New(root, "cancel-int")

	// Provider hangs (no message_stop) so the context cancel kicks in
	// mid-stream. The agent treats this as a cancelled turn and must not
	// snapshot.
	prov := fake.New([]llm.StreamEvent{{Type: llm.EventMessageStart}})
	a := New(Options{
		Provider: prov, Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		LaunchDir: root, SessionID: "cancel-int", Checkpoint: mgr,
	})
	a.Start()
	defer a.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	collectEvents(a.Submit(ctx, "go"))

	if a.TurnCount() != 0 {
		t.Fatalf("cancelled turn must not bump count: got %d", a.TurnCount())
	}
	if mgr.SnapshotSHA(1) != "" {
		t.Fatal("cancelled turn must not write a snapshot ref")
	}
	if _, err := LoadSession("cancel-int"); err == nil {
		t.Fatal("cancelled turn must not write a sidecar")
	}
}

// TestRewindIntegration_DeleteSessionScrubs verifies cleanup-on-exit nukes
// every snapshot ref so refs/sam/checkpoints/ does not grow over time.
func TestRewindIntegration_DeleteSessionScrubs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := initRepoForIntegration(t)

	repo, _ := git.PlainOpen(root)
	mgr, _ := checkpoint.New(root, "scrub-int")
	prov := fake.New(rewindEndTurnScript(), rewindEndTurnScript())
	a := New(Options{
		Provider: prov, Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		LaunchDir: root, SessionID: "scrub-int", Checkpoint: mgr,
	})
	a.Start()
	collectEvents(a.Submit(context.Background(), "t1"))
	collectEvents(a.Submit(context.Background(), "t2"))
	a.Close()

	if err := mgr.DeleteSession(); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSession("scrub-int"); err != nil {
		t.Fatal(err)
	}
	iter, _ := repo.References()
	defer iter.Close()
	count := 0
	_ = iter.ForEach(func(r *plumbing.Reference) error {
		if hasPrefix(string(r.Name()), checkpoint.RefPrefix+"scrub-int/") {
			count++
		}
		return nil
	})
	if count != 0 {
		t.Fatalf("refs remained after DeleteSession: %d", count)
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
