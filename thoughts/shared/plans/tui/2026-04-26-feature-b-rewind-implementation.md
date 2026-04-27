# Feature B — Esc-Esc Rewind (Optional Code Restore): Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use executing-plans to implement this plan task-by-task.

**Goal:** Implement time-travel rewind through conversation history with optional, surgical code restore via git checkpoints, allowing users to undo multi-turn agent work and selectively restore code only to paths the agent touched.

**Architecture:**
- Add `internal/checkpoint/` package (NEW) with `Snapshot(turn int)`, `Restore(turn int, paths []string)`, `ListPaths(turn int)`, `DeleteSession()`, `SweepOrphans()`, `IsGitRepo()` APIs using github.com/go-git/go-git/v5 for pure-Go git operations
- Add `internal/agent/turn_record.go` (NEW) for durable per-turn snapshots: user msg index, tool calls (name+input+result+IsError), snapshot SHA; persist as JSON sidecar in `$XDG_STATE_HOME/sam/sessions/<session-id>.json`
- Extend `Agent` to assign per-session ID at startup (unix-sec-6byte-hex), expose turn boundaries and tool-call records, hook snapshot trigger at graceful turn completion (Feature A's end-of-turn site only)
- Add `internal/tui/picker.go` (NEW) and `internal/tui/restore_confirm.go` (NEW): full-screen split Bubbletea components for turn selection, code review, and restore confirmation
- Add idle Esc-Esc detection to `internal/tui/app.go`, Ctrl+Z undo handler, `/unrewind` slash command
- Extend `internal/tui/renderer.go` with transient hint after rewind
- Wire `cmd/sam/main.go`: assign session ID, git-repo check, startup orphan sweep, cleanup-on-exit handlers
- Update `README.md` to document `refs/sam/checkpoints/` namespace and keybindings

**Tech Stack:** Go 1.26.2, github.com/go-git/go-git/v5, Charmbracelet Bubbletea, stdlib difflib-style rendering, lipgloss for colors, JSON for durable per-turn records

---

## Task 1: Implement Checkpoint Package (git operations foundation)

**Files:**
- Create: `internal/checkpoint/checkpoint.go`
- Create: `internal/checkpoint/checkpoint_test.go`
- Modify: `go.mod` (add github.com/go-git/go-git/v5)

**Summary:** Implement core git-based checkpoint creation, session management, ref lifecycle, and orphan cleanup. Use go-git for pure-Go (CGO-free) operations on shadow refs under `refs/sam/checkpoints/`.

---

## Task 2: Implement Turn Record Durability (per-turn metadata)

**Files:**
- Create: `internal/agent/turn_record.go`
- Create: `internal/agent/turn_record_test.go`

**Summary:** Define durable turn record schema (versioned) with tool calls, snapshot SHA, timestamps. Implement JSON persistence to `$XDG_STATE_HOME/sam/sessions/<session-id>.json` for picker preview durability and orphan-sweep identification.

---

## Task 3: Extend Agent to Record Turns and Trigger Snapshots

**Files:**
- Modify: `internal/agent/agent.go` (add SessionID field, checkpoint manager)
- Modify: `internal/agent/loop.go` (snapshot hook at graceful completion line ~100-110)
- Create: `internal/agent/snapshot_test.go`

**Summary:** Assign SessionID at agent startup, initialize checkpoint manager, call `snapshotTurn()` only after graceful completion (stop=="end_turn" or natural loop exit). Ensure cancelled turns do NOT snapshot.

---

## Task 4: Implement Checkpoint Restore and Overlap Detection

**Files:**
- Modify: `internal/checkpoint/checkpoint.go` (implement Restore, ListPaths, DeleteSession, SweepOrphans)
- Modify: `internal/checkpoint/checkpoint_test.go` (add restore/overlap/cleanup tests)

**Summary:** Implement surgical restore from checkpoint trees, overlap detection heuristic (compare working-copy hash vs cumulative agent edits), path diffing between checkpoint snapshots, session cleanup, and orphan sweep (age + sidecar checks).

---

## Task 5: Implement Picker and Restore Confirm Components (Bubbletea TUI)

**Files:**
- Create: `internal/tui/picker.go`
- Create: `internal/tui/picker_test.go`
- Create: `internal/tui/restore_confirm.go`
- Create: `internal/tui/restore_confirm_test.go`

**Summary:** Build full-screen split history picker (left: turn list with filter, right: preview with message + tools + files + bash warning). Build restore-mode chooser ([c] conversation-only, [b] conversation+code with diff preview) and confirm modal. Reuse lipgloss/Bubbletea patterns from existing settings_modal.go.

---

## Task 6: Wire Esc-Esc Idle Detection and Ctrl+Z Undo in TUI

**Files:**
- Modify: `internal/tui/app.go` (idle Esc-Esc detection, Ctrl+Z handler, /unrewind command)
- Modify: `internal/tui/renderer.go` (transient rewind hint)
- Create: `internal/tui/rewind_test.go`

**Summary:** Detect double Esc within 500ms window when idle (no turn in flight). Open picker on Esc-Esc. Implement undo-rewind buffer (one slot, cleared on next user message). Wire Ctrl+Z and `/unrewind` handlers. Show transient "Rewound to turn N. Press Ctrl+Z to undo" hint with 3s fade.

---

## Task 7: Wire Session ID Assignment and Git Repo Check in main.go

**Files:**
- Modify: `cmd/sam/main.go` (session ID assignment, git-repo check, cleanup handlers, orphan sweep)

**Summary:** At startup, assign SessionID via `checkpoint.SessionID()`, check `IsGitRepo()`, run `SweepOrphans()` for orphans older than 7 days. Register cleanup via deferred `DeleteSession()` + SIGINT/SIGTERM handler. Show hint "rewind unavailable: not in a git repo" if needed.

---

## Task 8: Integration Test — Multi-Turn Rewind with Code Restore

**Files:**
- Create: `internal/agent/rewind_integration_test.go`

**Summary:** Write end-to-end test in real temp git repo: create session, run 3 agent turns with file modifications, verify checkpoints created, rewind to turn 1, verify restore (both [c] and [b] modes), verify overlap detection, verify Ctrl+Z undo. Document that Feature A cancels do NOT snapshot (contract preserved).

---

## Task 9: Update README and Configuration Defaults

**Files:**
- Modify: `README.md` (document Esc-Esc / Ctrl+Z / `/unrewind` keybindings, refs/sam/checkpoints/ namespace)
- Modify: `internal/config/config.go` (ensure [ux] block defaults: checkpoint_enabled=true, esc_double_window_ms=500)

**Summary:** User-facing docs for rewind UX. Reserved git namespace documentation (prevent user custom refspecs from colliding). Config defaults for feature toggle and timing.

---

## Task 10: Final Integration and End-to-End Validation

**Files:**
- Verify: All tests pass, no goroutine leaks, no -race violations
- Verify: Features A, C, D remain unbroken

**Summary:** Full test suite, race detection, goleak verification. Confirm cancel path does not snapshot (Feature A contract). Confirm steer queue auto-submit increments turnNum once (Feature C contract). Confirm file picker unaffected (Feature D contract). Final commit batch.

---

## Key Design Decisions Embedded in Plan

1. **Go-git ONLY** — no shell-out to git CLI; in-memory index; CGO-free; verify write-tree + checkout-index support
2. **Durable JSON sidecars** — not in-memory only; enables picker preview across same-process session restart and orphan detection
3. **Conservative overlap heuristic** — false-positives (warn when safe) acceptable; false-negatives (clobber user work) are data-loss bugs — never
4. **Graceful-completion-only snapshots** — cancelled/errored turns never snapshot; rewind targets always represent stable agent-completed states
5. **Surgical restore** — only paths agent touched in undone turns, overlap-flagged files not auto-restored, user explicitly confirms
6. **One-slot undo-rewind buffer** — simple, prevents confusion; cleared on next user message; Ctrl+Z / `/unrewind` before next message
7. **Strict Feature A/C/D no-break** — cancel prevents snapshot, steer queue auto-submit doesn't double-snapshot, file picker untouched

---

**Status:** Ready for execution. All tasks concrete, testable, independently committable.
