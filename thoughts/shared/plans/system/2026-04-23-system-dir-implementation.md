# Implementation Plan: Externalize system prompt and tool descriptions to `~/.sam/system/`

> Date: 2026-04-23
> Design: [2026-04-23-system-dir-design.md](./2026-04-23-system-dir-design.md)
> Status: Ready to execute

## Overview

Move hardcoded system prompt (`cmd/sam/main.go:25-31`) and tool descriptions (`internal/tools/{read,write,edit,bash}.go`) into a new `internal/system` package that embeds defaults at build time and seeds them to `~/.sam/system/` on first run. Overwrites only files users have not edited (sha256 manifest). Existing `--system-prompt` flag and `system_prompt_file` config key remain top of precedence chain.

Each phase below = one tiny commit. Every phase must leave `go build ./...` and `go test ./...` green.

---

## Phase 1 — Create `internal/system` package skeleton

**Goal:** package exists, embeds defaults, exposes `DefaultDir` + `EmbeddedPrompt` + `EmbeddedToolDescription`. No disk I/O yet.

**New files:**

- `internal/system/system.go` — public API, `DefaultDir`, `Embedded*` functions, `embed.FS` handle.
- `internal/system/defaults/system-prompt.md` — new caveman-priority prompt (body in design §Default content).
- `internal/system/defaults/tools/read.md` — verbatim copy of `internal/tools/read.go:26` `readDescription`.
- `internal/system/defaults/tools/write.md` — verbatim copy of `internal/tools/write.go:15` `writeDescription`.
- `internal/system/defaults/tools/edit.md` — verbatim copy of `internal/tools/edit.go:18` `editDescription`.
- `internal/system/defaults/tools/bash.md` — verbatim copy of `internal/tools/bash.go:27` `bashDescription`.

**API contract:**

```go
package system

//go:embed defaults/*
var defaultsFS embed.FS

func DefaultDir() string {
    if v := os.Getenv("SAM_HOME"); v != "" {
        return filepath.Join(v, "system")
    }
    home, err := os.UserHomeDir()
    if err != nil {
        return filepath.Join(".sam", "system")
    }
    return filepath.Join(home, ".sam", "system")
}

func EmbeddedPrompt() string { ... read defaults/system-prompt.md ... }
func EmbeddedToolDescription(name string) string { ... read defaults/tools/<name>.md ... }
```

Tool name to filename: lowercase (`Read` → `read.md`). Normalize in one helper.

**New test file:** `internal/system/system_test.go`

- `TestDefaultDir_SAMHome` — `t.Setenv("SAM_HOME", "/tmp/foo")` → returns `/tmp/foo/system`.
- `TestDefaultDir_DefaultHome` — unset → ends with `.sam/system`.
- `TestEmbeddedPrompt_NonEmpty` — returned string non-empty, contains `CAVEMAN SPEECH`.
- `TestEmbeddedToolDescription_AllFour` — each of `Read`/`Write`/`Edit`/`Bash` returns non-empty, matches exact current `*Description` string from Go source (regression guard).

**Verify:** `go build ./... && go test ./internal/system/...`

---

## Phase 2 — Add seed + load functions to `internal/system`

**Goal:** disk I/O with atomic writes and manifest-based edit detection.

**Modified file:** `internal/system/system.go` — add `Seed`, `LoadSystemPrompt`, `LoadToolDescription`.
**New file:** `internal/system/seed.go` — manifest struct, sha256 helper, atomic write helper.

**`seed.go` sketch:**

```go
type manifest struct {
    Version string            `json:"version"`
    Hashes  map[string]string `json:"hashes"`
}

const manifestName = ".seed-manifest.json"

// atomicWrite writes data to path via <path>.tmp + rename.
func atomicWrite(path string, data []byte, perm os.FileMode) error

// loadManifest returns empty manifest if missing or corrupt (deliberate).
func loadManifest(dir string) manifest

func saveManifest(dir string, m manifest) error
```

**`Seed` flow (per design §Architecture):**

1. `os.MkdirAll(dir, 0755)` + `os.MkdirAll(dir/tools, 0755)`.
2. Load manifest (empty-on-error).
3. Walk embedded `defaults/`. For each embedded file with relative path `rel`:
   - Compute `embeddedHash = sha256(embeddedBytes)`.
   - Target path `tgt = filepath.Join(dir, rel)`.
   - If `tgt` missing → `atomicWrite(tgt, embedded, 0644)`, set `m.Hashes[rel] = embeddedHash`.
   - Else read `tgt`, compute `diskHash`. If `diskHash == m.Hashes[rel]` → `atomicWrite(tgt, embedded, 0644)`, update manifest hash. If different → skip, and if `embeddedHash != m.Hashes[rel]` log `logger.Info("system: new default available", "file", rel)` once.
4. `saveManifest(dir, m)` via atomic write.
5. Return first error encountered (if any). Partial writes remain on disk.

**`LoadSystemPrompt` / `LoadToolDescription`:**

Straightforward `os.ReadFile` on `dir/system-prompt.md` or `dir/tools/<lower-name>.md`. Return `""` (no error) on `os.IsNotExist`. Real errors still returned.

**New tests in `internal/system/seed_test.go`:**

- `TestSeed_FreshDir`
- `TestSeed_Idempotent`
- `TestSeed_UserEditedSkipped`
- `TestSeed_UnchangedOverwritten` — inject fake manifest hash that does not match embedded to simulate embedded-changed.
- `TestSeed_MissingManifest` — files exist, manifest deleted → new manifest written, existing files preserved.
- `TestSeed_CorruptManifest` — write invalid JSON → same behavior as missing.
- `TestSeed_Concurrent` — `sync.WaitGroup` with 2 goroutines on same fresh tempdir → no corruption, manifest matches embedded.
- `TestLoadSystemPrompt_MissingAndPresent`
- `TestLoadToolDescription_MissingAndPresent`

Each test uses `t.TempDir()` directly (not `SAM_HOME`) to keep scope narrow.

**Verify:** `go test ./internal/system/... -race`

---

## Phase 3 — Extend tool constructors with `description` argument

**Goal:** tool constructors accept description string. Existing call sites broken intentionally to catch all of them.

**Modified files:**

- `internal/tools/read.go` — remove `var readDescription = "..."`. Change `func NewRead(tracker *ReadTracker) Tool` to `func NewRead(tracker *ReadTracker, description string) Tool`. Body uses `description` instead of `readDescription`.
- `internal/tools/write.go` — same treatment for `writeDescription` / `NewWrite`.
- `internal/tools/edit.go` — same for `editDescription` / `NewEdit`.
- `internal/tools/bash.go` — same for `bashDescription` / `NewBash`. Signature becomes `func NewBash(launchDir, description string) Tool`.

**Call sites that must be updated in the same commit (else build breaks):**

- `cmd/sam/main.go:93` — `tools.NewRead(tracker, "")` temporary empty string; real wiring in Phase 4.
- `cmd/sam/main.go:94` — `tools.NewWrite(tracker, "")`.
- `cmd/sam/main.go:95` — `tools.NewEdit(tracker, "")`.
- `cmd/sam/main.go:96` — `tools.NewBash(cwd, "")`.
- `internal/agent/agent_test.go:86` — `tools.NewRead(tracker, "test")`.
- `internal/agent/agent_test.go:210` — same.
- `internal/agent/agent_test.go:307` — same.
- `internal/tools/read_test.go` — any `NewRead(...)` sites → pass `"test"`.
- `internal/tools/write_test.go` — any `NewWrite(...)` → pass `"test"`.
- `internal/tools/edit_test.go` — any `NewEdit(...)` → pass `"test"`.
- `internal/tools/bash_test.go` — any `NewBash(...)` → pass `"test"`.

Empty string in `main.go` is an intentional temporary placeholder; Phase 4 replaces with real lookup.

**Verify:** `go build ./... && go test ./...` green. Tool descriptions in Phase 3 are empty when run; acceptable because Phase 4 is the very next commit. If paranoid, ship Phases 3 + 4 together.

---

## Phase 4 — Wire `cmd/sam/main.go` to seed and load from `~/.sam/system/`

**Goal:** delete hardcoded `defaultSystemPrompt`, seed on startup, inject real tool descriptions.

**Modified file:** `cmd/sam/main.go`

Delete `defaultSystemPrompt` const (`cmd/sam/main.go:25-31`).

In `main()` after `config.LoadSecrets().ApplyEnv(cfg)`:

```go
sysDir := system.DefaultDir()
if err := system.Seed(sysDir, logger); err != nil {
    logger.Warn("system seed failed", "err", err)
}
```

Pass `sysDir` to `runAgentOneShot` and `runTUI`. Update signatures:

```go
runAgentOneShot(ctx, cfg, sysDir, *prompt, logger)
runTUI(ctx, cfg, sysDir, logger, ring)
```

Change `buildRegistry(cwd)` → `buildRegistry(cwd, sysDir)`:

```go
func buildRegistry(cwd, sysDir string) (*tools.Registry, *tools.ReadTracker) {
    desc := func(name string) string {
        s, _ := system.LoadToolDescription(sysDir, name)
        if s == "" {
            s = system.EmbeddedToolDescription(name)
        }
        return s
    }
    tracker := tools.NewReadTracker()
    reg := tools.NewRegistry()
    reg.Register(tools.NewRead(tracker, desc("Read")))
    reg.Register(tools.NewWrite(tracker, desc("Write")))
    reg.Register(tools.NewEdit(tracker, desc("Edit")))
    reg.Register(tools.NewBash(cwd, desc("Bash")))
    return reg, tracker
}
```

Replace both system-prompt lookup sites:

```go
// was: sys := cfg.LoadSystemPrompt(defaultSystemPrompt)
diskPrompt, _ := system.LoadSystemPrompt(sysDir)
if diskPrompt == "" {
    diskPrompt = system.EmbeddedPrompt()
}
sys := cfg.LoadSystemPrompt(diskPrompt)
```

Apply at both `cmd/sam/main.go:107` (in `runTUI`) and `cmd/sam/main.go:172` (in `runAgentOneShot`).

**Verify:**

- `go build ./... && go test ./...` green.
- Manual: `rm -rf /tmp/sam-home && SAM_HOME=/tmp/sam-home ./sam -p "say hi"` — directory seeded with 5 files + manifest. `ls /tmp/sam-home/system/`.
- Manual: edit `/tmp/sam-home/system/system-prompt.md`, re-run, confirm file preserved and manifest unchanged for that hash.
- Manual: `./sam --system-prompt /tmp/custom-prompt.md -p "…"` confirms flag still wins.

---

## Phase 5 — Integration / precedence E2E test

**Goal:** regression guard for the full precedence chain.

**New file:** `internal/system/integration_test.go` (or `cmd/sam/main_test.go` if main is easier to drive).

Testable via `cfg.LoadSystemPrompt` directly plus `system.LoadSystemPrompt`. Do not shell out. Cases:

- `TestPrecedence_EmbeddedOnly` — `SAM_HOME=<tempdir>` but never call `Seed`, no config, no flag → caller-level fallback resolves to `EmbeddedPrompt`.
- `TestPrecedence_DiskBeatsEmbedded` — `Seed` into tempdir, modify `system-prompt.md` on disk, `LoadSystemPrompt` returns disk content.
- `TestPrecedence_ConfigBeatsDisk` — set `cfg.SystemPromptFile` to point at arbitrary file, `cfg.LoadSystemPrompt(diskContent)` returns file content.
- `TestPrecedence_FlagBeatsConfig` — covered indirectly via `config.Load` with `Overrides.SystemPromptFile`; verify override populates `cfg.SystemPromptFile`.

**Verify:** `go test ./... -race` green.

---

## Phase 6 — Documentation

**Goal:** README reflects new behavior.

**Modified file:** `README.md`

Add short section near existing config docs:

- `~/.sam/system/` auto-created on first run.
- Files in it: `system-prompt.md`, `tools/read.md`, `tools/write.md`, `tools/edit.md`, `tools/bash.md`, `.seed-manifest.json`.
- User edits preserved across SAM upgrades (manifest tracks hashes).
- Override priority: `--system-prompt` > `system_prompt_file` config > `~/.sam/system/system-prompt.md` > built-in defaults.
- `$SAM_HOME` env var moves root (default `~/.sam`).
- Delete a file + manifest entry to force re-seed.

**Verify:** `grep -n '~/.sam/system' README.md` — non-empty.

---

## Rollback

Each phase is a single commit. Revert the commit to roll back. Phases 3 + 4 may be combined into one commit if the reviewer prefers no-intermediate-broken-behavior (though tests stay green throughout).

## Risks

- `//go:embed defaults/*` picks up `.md` files only; confirm no extra files accidentally embedded. Mitigation: single directory, explicit pattern.
- `os.UserHomeDir` failure on exotic environments. Mitigation: `DefaultDir` returns relative `.sam/system` fallback; `Seed` still works if current working directory is writable.
- Disk permission errors on `~/.sam/system/`. Mitigation: `Seed` returns error, main logs warn, loaders return empty, embedded fallback used.
- Tool name normalization mismatch. Mitigation: centralize lowercase in a helper, covered by `TestEmbeddedToolDescription_AllFour`.

## Out of scope

- Project-local `./.sam/system/` override.
- Externalizing JSON schemas.
- Hot-reload of files mid-session.
- UI surfacing "new default available" notifications beyond a log line.
