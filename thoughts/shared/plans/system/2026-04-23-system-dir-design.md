# Design: Externalize system prompt and tool definitions to `~/.sam/system/`

> Date: 2026-04-23
> Status: Draft, pending review

## Problem

The base system prompt and tool descriptions are currently hardcoded in Go:

- `cmd/sam/main.go:25-31` — `defaultSystemPrompt` const
- `internal/tools/bash.go:27` — `bashDescription`
- `internal/tools/edit.go:18` — `editDescription`
- `internal/tools/read.go:26` — `readDescription`
- `internal/tools/write.go:15` — `writeDescription`

Users cannot tweak these without rebuilding. Existing overrides (`--system-prompt` flag, `system_prompt_file` TOML key) only cover the system prompt, not tool descriptions.

## Goals

1. Move the canonical defaults for system prompt and tool descriptions out of Go source and into `~/.sam/system/`.
2. Keep binary self-sufficient — no external assets required at install time.
3. Preserve all existing override mechanisms (CLI flag, config file).
4. Allow SAM upgrades to ship improved defaults without clobbering user edits.

## Non-Goals

- Project-local `./.sam/system/` override (YAGNI).
- Externalizing tool JSON schemas (remain derived from Go `In` types).
- Removing `--system-prompt` flag or `system_prompt_file` config key.

## Decisions

### D1 — Bootstrap: auto-seed on first run

On startup, if `~/.sam/system/` is missing or incomplete, write embedded defaults to disk. Subsequent runs read from disk. User can edit freely.

### D2 — Layout: per-file markdown

```
~/.sam/system/
  system-prompt.md
  tools/
    read.md
    write.md
    edit.md
    bash.md
  .seed-manifest.json
```

Each tool `.md` is the description body only. Name comes from the Go registry.
Schemas remain Go-derived via `invopop/jsonschema` on `*Input` structs.

### D3 — Precedence: disk slots in above embedded

```
--system-prompt flag
  > system_prompt_file config
    > ~/.sam/system/system-prompt.md
      > embedded fallback
```

For tool descriptions (no flag/config today):

```
~/.sam/system/tools/<name>.md
  > embedded fallback
```

### D4 — Upgrade: overwrite only unmodified

Track content hashes in `~/.sam/system/.seed-manifest.json`:

```json
{
  "version": "1",
  "hashes": {
    "system-prompt.md": "sha256:...",
    "tools/read.md":    "sha256:...",
    "tools/write.md":   "sha256:...",
    "tools/edit.md":    "sha256:...",
    "tools/bash.md":    "sha256:..."
  }
}
```

Seed algorithm (runs every startup, idempotent):

1. For each embedded file:
   - If disk file missing → atomic write (temp + rename), record hash.
   - If disk sha256 == manifest hash → atomic overwrite with new embedded, update manifest.
   - If disk sha256 != manifest hash (user edited) → skip. If embedded hash changed vs manifest, log `info` once noting new default available.
2. Atomic write of manifest (temp + rename).

Edge cases:

- Missing manifest, files exist → rebuild manifest from current disk hashes (treat all as user-edited).
- Corrupt manifest JSON → same as missing. Deliberate: safe default, freezes current disk state as canonical user content until user deletes the dir or manifest.
- Directory perms: `0755`. File perms: `0644`.
- Concurrent first-run of two SAM processes: accepted race. Both may write; last-writer-wins on manifest. Content is identical so outcome is consistent. No flock introduced.
- Permission error (read-only `~/.sam/system/`): `Seed` returns error, `main.go` logs `Warn` and proceeds. `LoadSystemPrompt` / `LoadToolDescription` return empty; callers fall through to embedded content. Precedence chain unaffected.

## Architecture

### New package: `internal/system`

```
internal/system/
  system.go       // public API
  seed.go         // Seed() logic, manifest I/O
  defaults/       // embed.FS
    system-prompt.md
    tools/
      read.md
      write.md
      edit.md
      bash.md
```

**Public API:**

```go
package system

// DefaultDir returns $SAM_HOME/system if $SAM_HOME is set, else ~/.sam/system.
// $SAM_HOME is the SAM root (parallel to existing skills convention at
// ~/.sam/skills); the system directory always lives under it.
func DefaultDir() string

// Seed writes embedded defaults into dir, respecting manifest-tracked edits.
// Idempotent. Writes each file atomically (temp file + os.Rename) so a crash
// mid-write never leaves a truncated file on disk. Returns first I/O error
// encountered; files already written are kept.
func Seed(dir string, logger *slog.Logger) error

// LoadSystemPrompt reads system-prompt.md; returns "" if missing.
func LoadSystemPrompt(dir string) (string, error)

// LoadToolDescription reads tools/<name>.md; returns "" if missing.
func LoadToolDescription(dir, toolName string) (string, error)

// EmbeddedPrompt returns the embedded system-prompt.md content.
func EmbeddedPrompt() string

// EmbeddedToolDescription returns embedded tools/<name>.md content.
func EmbeddedToolDescription(name string) string
```

**Import rules:** `internal/system` depends only on stdlib + `log/slog`. `cmd/sam`, `internal/config`, `internal/tools` may import it. No cycles.

### Changes to existing files

#### `cmd/sam/main.go`

- Delete `defaultSystemPrompt` const.
- Early in `main()` (after config load, before agent init):
  ```go
  sysDir := system.DefaultDir()
  if err := system.Seed(sysDir, logger); err != nil {
      logger.Warn("system seed failed", "err", err)
  }
  ```
- Replace `defaultSystemPrompt` fallback in both `runTUI` and `runAgentOneShot`:
  ```go
  diskPrompt, _ := system.LoadSystemPrompt(sysDir)
  if diskPrompt == "" {
      diskPrompt = system.EmbeddedPrompt()
  }
  sys := cfg.LoadSystemPrompt(diskPrompt)
  ```
- `buildRegistry` gains a `sysDir` argument and looks up each tool's description via `system.LoadToolDescription`, falling back to `system.EmbeddedToolDescription`.

#### `internal/tools/read.go`, `write.go`, `edit.go`, `bash.go`

- Delete package-level `*Description` vars.
- Constructors take `description string`:
  ```go
  func NewBash(cwd, description string) Tool
  func NewRead(tracker *ReadTracker, description string) Tool
  func NewWrite(tracker *ReadTracker, description string) Tool
  func NewEdit(tracker *ReadTracker, description string) Tool
  ```
- Each constructor passes its `description` into `New[In](name, description, fn)`.

#### `internal/tools/*_test.go`

- Pass a fixed string (e.g. `"test"`) for `description` where constructors are invoked.
- Assertions on `.Description()` updated if any exist.

#### `internal/agent/agent_test.go`

Also calls `tools.NewRead(tracker)`. Must pass `"test"` as second argument at:

- `internal/agent/agent_test.go:86`
- `internal/agent/agent_test.go:210`
- `internal/agent/agent_test.go:307`

#### `cmd/sam/main.go` — both call sites

`buildRegistry` is invoked from both `runTUI` (`cmd/sam/main.go:102`) and
`runAgentOneShot` (`cmd/sam/main.go:169`). Both call sites must pass `sysDir`.

## Default content

### `defaults/system-prompt.md`

```md
You are SAM, a terse coding agent.

CAVEMAN SPEECH — MANDATORY.
Drop articles (a, an, the). Drop filler (just, really, basically, actually, simply).
Drop pleasantries, hedging, throat-clearing.
Short synonyms. Fragments fine. Minimum words needed.
Technical terms exact. Code blocks, file paths, commits unchanged.
Pattern: `[thing] [action] [reason]. [next step].`

THINKING — MANDATORY.
Think deep before act. Analyze hard. Trace dependencies, read callers, check types.
Unknown = read file, run command, verify. Never guess.
No assumptions. Only verified facts.
If fact not confirmed, say "unverified" or go verify.
Prefer one slow correct step over three fast wrong ones.

EVIDENCE — MANDATORY.
Claim = proof. Cite file:line. Run command, show output.
"I think" / "probably" / "should work" = banned. Verify or state uncertainty explicitly.

Tools: Read, Write, Edit, Bash. Use when needed. Read before Write/Edit on existing files.
```

### `defaults/tools/*.md`

Verbatim copies of the current strings:

- `read.md` — content of `internal/tools/read.go:26` `readDescription`
- `write.md` — content of `internal/tools/write.go:15` `writeDescription`
- `edit.md` — content of `internal/tools/edit.go:18` `editDescription`
- `bash.md` — content of `internal/tools/bash.go:27` `bashDescription`

## Testing

### `internal/system/seed_test.go`

Each test calls `t.Setenv("SAM_HOME", t.TempDir())` or passes tempdir directly.

- `TestSeed_FreshDir` — all files written, manifest hashes match embedded.
- `TestSeed_Idempotent` — re-seed unchanged is no-op.
- `TestSeed_UserEditedSkipped` — modify `system-prompt.md` on disk, re-seed, file preserved, warning logged.
- `TestSeed_UnchangedOverwritten` — pretend embedded changed (test double), disk file unmodified → overwritten, manifest updated.
- `TestSeed_MissingManifest` — delete manifest, files exist → rebuild manifest from disk hashes.
- `TestSeed_CorruptManifest` — write invalid JSON → same as missing.

### `internal/system/load_test.go`

- `TestLoadSystemPrompt_Missing` — empty dir returns `""`, no error.
- `TestLoadSystemPrompt_Present` — write file, content returned verbatim.
- `TestLoadToolDescription_Missing` / `_Present` — same for tool files.

### Updated tests

- `internal/tools/bash_test.go`, `edit_test.go`, `read_test.go`, `write_test.go` — pass `"test"` as description.
- `internal/agent/agent_test.go` lines 86, 210, 307 — add `"test"` argument to `tools.NewRead(tracker)` calls.

### New integration tests

- `TestSeed_Concurrent` — spawn two goroutines calling `Seed` on same fresh tempdir; assert no file corruption, final manifest matches embedded hashes.
- `TestDefaultDir_SAMHome` — `t.Setenv("SAM_HOME", "/tmp/foo")` → returns `/tmp/foo/system`. Unset → returns `<user-home>/.sam/system`.
- `TestPrecedence_E2E` (in `cmd/sam/` or new `internal/system/integration_test.go`) — set up tempdir with disk file, config file, and CLI flag; assert the resolved system prompt matches the highest-precedence source in each combination.

## Rollout

1. Create `internal/system/` package, embedded defaults, `Seed`, `Load*` APIs, tests.
2. Update tool constructors to accept description.
3. Update `cmd/sam/main.go` to seed and wire descriptions.
4. Remove `defaultSystemPrompt` and `*Description` vars.
5. Run full test suite.
6. Manually verify first-run seed, idempotent re-seed, user-edit preservation.

## Open questions

None at design time. Flag during implementation if discovered.
