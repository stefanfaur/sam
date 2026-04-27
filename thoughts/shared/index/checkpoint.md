# checkpoint

> Per-session shadow git snapshots powering Esc-Esc rewind. Pure go-git (no shell-out, CGO-free). Refs live in the reserved namespace `refs/sam/checkpoints/<session>/<turn>` and never touch HEAD, current branch, working tree, or index.

## Files
- `internal/checkpoint/checkpoint.go` — Manager, SessionID, IsGitRepo, RefPrefix, SessionsStateDir, SweepOrphans
- `internal/checkpoint/checkpoint_test.go` — 13 tests (snapshot HEAD-preservation, parent chaining, ListPaths diff, Restore + escape refusal, OverlapPaths user-edit + delete, DeleteSession, SweepOrphans)
- `internal/agent/turn_record.go` — durable JSON sidecar at `$XDG_STATE_HOME/sam/sessions/<sid>.json` (TurnRecord schema v1, atomic tmp+rename writes, per-session mutex)
- `internal/agent/snapshot.go` — `Agent.snapshotTurn()` graceful-completion hook
- `internal/agent/rewind_integration_test.go` — end-to-end multi-turn restore + overlap + cancel-skip-snapshot

## Manager API

```go
SessionID() string                  // unix-secs-6byte-hex
IsGitRepo(path) bool                // detect-dot-git
New(repoPath, sessionID) (*Manager, error)
(m).Snapshot(turn) (sha, error)     // wt.Commit with HEAD/index backup+restore
(m).SnapshotSHA(turn) string
(m).Turns() []int
(m).MaxTurn() int
(m).ListPaths(turn) []string        // diff snapshot[turn] vs snapshot[max]
(m).Restore(turn, paths) error      // surgical: writes blobs, deletes paths missing from snap
(m).OverlapPaths(paths) []string    // working-tree blob hash vs snap[max]
(m).DeleteSession() error
SweepOrphans(repo, age, sidecarMTime) error
```

## Snapshot mechanics

`Snapshot(turn)`:
1. Save HEAD branch ref + index state.
2. `wt.AddWithOptions(&AddOptions{All: true})` — stages tracked + untracked, respects `.gitignore`.
3. `wt.Commit(...)` with `Parents` overridden (turn-1's snapshot if present, else HEAD), `AllowEmptyCommits: true`. This advances the user's current branch as a side effect.
4. `Storer.SetReference("refs/sam/checkpoints/<sid>/<turn>", commitHash)`.
5. **Defer** restores branch ref + index — user state ends identical to start.

Cancelled or errored turns never reach the snapshot site (graceful-only — only the `len(pending)==0 || stop=="end_turn"` branch in `loop.go`, after queue-drain continuation handling).

## Restore mechanics

`Restore(turn, paths)`:
- Fetches commit tree at the chosen turn ref.
- For each path: if present in tree → write blob to working tree (mode-preserving); if absent → `os.Remove` (the path was created in the undone turns).
- Refuses absolute paths and `..` escapes.
- Never touches HEAD, branch refs, or the index.

`OverlapPaths(paths)` flags paths whose working-tree content hash (`plumbing.ComputeHash(BlobObject, data)`) differs from the latest snapshot's blob — also flags missing-from-disk and missing-from-snap variants. The TUI's `restoreConfirm` excludes overlapping paths from restore unless the user explicitly toggles `o`.

## Lifecycle

- `cmd/sam/main.go runTUI` mints a `SessionID()` once, gates init on `cfg.UX.Resolved()` + `IsGitRepo`, runs `SweepOrphans(cwd, 7d, agent.SessionMTime)` on startup.
- Defers `Manager.DeleteSession()` + `agent.DeleteSession(sid)` on exit (also fires under SIGINT/SIGTERM via the existing `signal.NotifyContext`).
- Sweep removes refs whose sidecar is missing or whose mtime is older than `olderThan` — orphan check.

## Sidecar (turn_record)

Schema v1 JSON:
```json
{
  "schema": 1,
  "session_id": "...",
  "started_at": "...",
  "updated_at": "...",
  "turns": [
    { "index": N, "user_msg_index": K, "user_msg": "...",
      "tools": [{"name":"Edit","input":{...},"output":"...","is_error":false}],
      "snapshot_sha": "...", "stopped_by": "end_turn", "timestamp": "..." }
  ]
}
```

Loaded by the rewind picker for preview rendering and by orphan-sweep mtime probe.

## Config

```toml
[ux]
checkpoint_enabled    = true   # set false to disable feature entirely
esc_double_window_ms  = 500    # idle Esc-Esc chord window (also abort chord)
```

`config.UXConfig.Resolved()` returns `(true, 500)` when fields are unset.
