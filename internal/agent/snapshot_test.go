package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/stefanfaur/sam/internal/checkpoint"
	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

// initRepoForTest initializes a git repo at tmp with one initial commit.
func initRepoForTest(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	repo, err := git.PlainInit(tmp, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add("seed.txt"); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	if _, err := wt.Commit("seed", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func TestAgent_GracefulTurnSnapshots(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := initRepoForTest(t)

	mgr, err := checkpoint.New(root, "agent-test-1")
	if err != nil {
		t.Fatal(err)
	}

	prov := fake.New([]llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, Text: "ok"},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	})
	a := New(Options{
		Provider:   prov,
		Tools:      tools.NewRegistry(),
		Policy:     policy.AllowAll(),
		LaunchDir:  root,
		SessionID:  "agent-test-1",
		Checkpoint: mgr,
	})
	a.Start()
	defer a.Close()

	if _, err := collectEvents(a.Submit(context.Background(), "hi")); err != nil {
		t.Fatal(err)
	}

	if a.TurnCount() != 1 {
		t.Fatalf("turn count = %d", a.TurnCount())
	}
	if mgr.SnapshotSHA(1) == "" {
		t.Fatal("turn 1 snapshot ref missing")
	}
	sr, err := LoadSession("agent-test-1")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if len(sr.Turns) != 1 {
		t.Fatalf("turns = %d", len(sr.Turns))
	}
	if sr.Turns[0].StoppedBy != "end_turn" || sr.Turns[0].UserMsg != "hi" {
		t.Fatalf("turn record wrong: %+v", sr.Turns[0])
	}
	if sr.Turns[0].SnapshotSHA == "" {
		t.Fatal("snapshot SHA empty in record")
	}
}

func TestAgent_CancelledTurnSkipsSnapshot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := initRepoForTest(t)

	mgr, _ := checkpoint.New(root, "agent-test-cancel")

	// Provider hangs so cancel takes effect mid-stream (no message_stop).
	prov := fake.New([]llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, Text: "..."},
	})
	a := New(Options{
		Provider:   prov,
		Tools:      tools.NewRegistry(),
		Policy:     policy.AllowAll(),
		LaunchDir:  root,
		SessionID:  "agent-test-cancel",
		Checkpoint: mgr,
	})
	a.Start()
	defer a.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := collectEvents(a.Submit(ctx, "go")); err != nil {
		t.Fatal(err)
	}

	if a.TurnCount() != 0 {
		t.Fatalf("cancelled turn must not increment count; got %d", a.TurnCount())
	}
	if mgr.SnapshotSHA(1) != "" {
		t.Fatal("cancelled turn must not snapshot")
	}
	if _, err := LoadSession("agent-test-cancel"); err == nil {
		t.Fatal("cancelled turn must not write sidecar")
	}
}

func TestAgent_SnapshotDisabledWhenNoCheckpoint(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	prov := fake.New([]llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, Text: "ok"},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	})
	a := New(Options{
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Policy:   policy.AllowAll(),
		// no SessionID, no Checkpoint
	})
	a.Start()
	defer a.Close()

	if _, err := collectEvents(a.Submit(context.Background(), "hi")); err != nil {
		t.Fatal(err)
	}
	// Turn count still increments (so callers/UI can reason about turn N).
	if a.TurnCount() != 1 {
		t.Fatalf("count = %d", a.TurnCount())
	}
	if a.CheckpointEnabled() {
		t.Fatal("CheckpointEnabled should be false")
	}
}
