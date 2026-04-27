package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/stefanfaur/sam/internal/agent"
	"github.com/stefanfaur/sam/internal/checkpoint"
	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

// initRepoForRewindTest mints a fresh repo with one initial commit.
func initRepoForRewindTest(t *testing.T) string {
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
	wt.Add("seed.txt")
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	if _, err := wt.Commit("seed", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func TestModel_OpenRewindPicker_NoCheckpoint(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	prov := fake.New([]llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	})
	a := agent.New(agent.Options{
		Provider: prov, Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		// No SessionID/Checkpoint → CheckpointEnabled = false.
	})
	a.Start()
	defer a.Close()
	m := New(a, nil, Options{})
	cmd, ok := m.openRewindPicker()
	if ok {
		t.Fatal("openRewindPicker should fail without checkpoint")
	}
	if cmd == nil {
		t.Fatal("expected info cmd describing the failure")
	}
	if m.rewind != nil {
		t.Fatal("picker must not be created")
	}
}

func TestModel_OpenAndRewind_FullFlow(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := initRepoForRewindTest(t)

	mgr, err := checkpoint.New(root, "rewind-flow")
	if err != nil {
		t.Fatal(err)
	}
	prov := fake.New(
		[]llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventMessageStop, StopReason: "end_turn"},
		},
		[]llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventMessageStop, StopReason: "end_turn"},
		},
	)
	a := agent.New(agent.Options{
		Provider: prov, Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		LaunchDir:  root,
		SessionID:  "rewind-flow",
		Checkpoint: mgr,
	})
	a.Start()
	defer a.Close()

	// Two turns to give the picker something to chew on. Drain each event channel.
	for _, msg := range []string{"first", "second"} {
		ch := a.Submit(context.Background(), msg)
		for range ch {
		}
		// Touch a working file between turns so the snapshots differ.
		os.WriteFile(filepath.Join(root, "f.txt"), []byte(msg), 0o644)
	}

	if a.TurnCount() != 2 {
		t.Fatalf("turn count = %d", a.TurnCount())
	}

	m := New(a, nil, Options{})
	cmd, ok := m.openRewindPicker()
	if !ok {
		t.Fatalf("openRewindPicker failed: cmd=%v", cmd)
	}
	if m.rewind == nil || len(m.rewind.entries) != 2 {
		t.Fatalf("picker entries = %d", len(m.rewind.entries))
	}

	// Move to turn 1, hit Enter → restore confirm modal.
	m.handleRewindPickerKey(tea.KeyMsg{Type: tea.KeyUp})
	m.handleRewindPickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.restoreUI == nil {
		t.Fatal("restore confirm modal not opened")
	}
	if m.restoreUI.turnIndex != 1 {
		t.Fatalf("restore turn = %d", m.restoreUI.turnIndex)
	}

	// Choose conv-only mode and confirm — expect history truncated, working
	// tree untouched, undo slot populated.
	m.restoreUI.SetMode(restoreModeConvOnly)
	if _, handled := m.handleRestoreConfirmKey(tea.KeyMsg{Type: tea.KeyEnter}); !handled {
		t.Fatal("confirm enter not handled")
	}
	if m.restoreUI != nil || m.rewind != nil {
		t.Fatal("overlays should be closed after confirm")
	}
	if a.TurnCount() != 0 {
		t.Fatalf("after rewind to turn 1 (conv-only): expected count 0 (= 1-1), got %d", a.TurnCount())
	}
	if m.undoRewind == nil {
		t.Fatal("undo slot must be populated")
	}

	// Undo — count restored, hint set.
	m.applyUndoRewind()
	if a.TurnCount() != 2 {
		t.Fatalf("undo: turn count = %d", a.TurnCount())
	}
	if m.undoRewind != nil {
		t.Fatal("undo slot should clear after applyUndoRewind")
	}
}

func TestRewindHint_TickClearsBanner(t *testing.T) {
	a := agent.New(agent.Options{
		Provider: fake.New(),
		Tools:    tools.NewRegistry(),
		Policy:   policy.AllowAll(),
	})
	a.Start()
	defer a.Close()
	m := New(a, nil, Options{})
	m.rewindHint = rewindHintState{Text: "x", Until: time.Now().Add(-time.Second)}
	if m.rewindHint.Active() {
		t.Fatal("expired hint reports active")
	}
	if got := m.renderRewindHint(); got != "" {
		t.Fatalf("expected empty render, got %q", got)
	}
}

func TestModel_RewindConvAndCode_RestoresWorkingTreeAndUndoes(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := initRepoForRewindTest(t)
	mgr, _ := checkpoint.New(root, "rewind-bcode")

	prov := fake.New(
		[]llm.StreamEvent{{Type: llm.EventMessageStart}, {Type: llm.EventMessageStop, StopReason: "end_turn"}},
		[]llm.StreamEvent{{Type: llm.EventMessageStart}, {Type: llm.EventMessageStop, StopReason: "end_turn"}},
	)
	a := agent.New(agent.Options{
		Provider: prov, Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
		LaunchDir: root, SessionID: "rewind-bcode", Checkpoint: mgr,
	})
	a.Start()
	defer a.Close()

	// Turn 1: feature.go = v1.
	os.WriteFile(filepath.Join(root, "feature.go"), []byte("v1"), 0o644)
	for range a.Submit(context.Background(), "t1") {
	}
	// Turn 2: feature.go = v2 + helper.go added.
	os.WriteFile(filepath.Join(root, "feature.go"), []byte("v2"), 0o644)
	os.WriteFile(filepath.Join(root, "helper.go"), []byte("h"), 0o644)
	for range a.Submit(context.Background(), "t2") {
	}

	m := New(a, nil, Options{})
	if cmd, ok := m.openRewindPicker(); !ok {
		t.Fatalf("openRewindPicker: cmd=%v", cmd)
	}
	// Move to turn 1 and Enter to open restore-confirm.
	m.handleRewindPickerKey(tea.KeyMsg{Type: tea.KeyUp})
	m.handleRewindPickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.restoreUI == nil || m.restoreUI.turnIndex != 1 {
		t.Fatalf("restore modal not open at turn 1: %+v", m.restoreUI)
	}
	// Pick code-mode via 'b', then Enter to confirm.
	m.handleRestoreConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m.handleRestoreConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.restoreUI != nil || m.rewind != nil {
		t.Fatal("overlays should close after confirm")
	}

	// Working tree must be at turn-1 state: feature.go=v1, helper.go gone.
	if data, _ := os.ReadFile(filepath.Join(root, "feature.go")); string(data) != "v1" {
		t.Fatalf("feature.go = %q, want v1", data)
	}
	if _, err := os.Stat(filepath.Join(root, "helper.go")); !os.IsNotExist(err) {
		t.Fatalf("helper.go should be deleted (err=%v)", err)
	}

	// Per fix C1+C2: ref/sidecar for turn 2 must be gone after rewind, so
	// MaxTurn now reports 0 (we kept-through 0 = targetTurn-1).
	if max, _ := mgr.MaxTurn(); max != 0 {
		t.Fatalf("after rewind to turn 1: MaxTurn = %d, want 0", max)
	}
	sr, _ := agent.LoadSession("rewind-bcode")
	if sr != nil && len(sr.Turns) != 0 {
		t.Fatalf("after rewind: sidecar turns = %d, want 0", len(sr.Turns))
	}

	// Undo restores forward: working tree pops to turn-2 state, refs +
	// sidecar resurrect with the saved snapshot SHAs.
	m.applyUndoRewind()
	if data, _ := os.ReadFile(filepath.Join(root, "feature.go")); string(data) != "v2" {
		t.Fatalf("undo: feature.go = %q, want v2", data)
	}
	if _, err := os.Stat(filepath.Join(root, "helper.go")); err != nil {
		t.Fatalf("undo: helper.go missing: %v", err)
	}
	if max, _ := mgr.MaxTurn(); max != 2 {
		t.Fatalf("undo: MaxTurn = %d, want 2", max)
	}
	sr2, err := agent.LoadSession("rewind-bcode")
	if err != nil || len(sr2.Turns) != 2 {
		t.Fatalf("undo: sidecar turns = %v err=%v", sr2, err)
	}
	if a.TurnCount() != 2 {
		t.Fatalf("undo: turn count = %d", a.TurnCount())
	}
}

func TestSubmitClearsUndoSlot(t *testing.T) {
	a := agent.New(agent.Options{
		Provider: fake.New([]llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventMessageStop, StopReason: "end_turn"},
		}),
		Tools: tools.NewRegistry(), Policy: policy.AllowAll(),
	})
	a.Start()
	defer a.Close()
	m := New(a, nil, Options{})
	m.undoRewind = &undoRewindSlot{}
	m.clearUndoOnNextSubmit()
	if m.undoRewind != nil {
		t.Fatal("undo not cleared")
	}
}
