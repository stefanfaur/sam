# RTK Integration for Bash + Read Tools — Design

**Status:** Draft
**Date:** 2026-04-23
**Author:** sfaur (via brainstorm)
**Scope:** Deep integration of rtk (Rust Token Killer, https://github.com/rtk-ai/rtk) as a transparent-by-default compression layer for the agent's `Bash` and `Read` tools, with an LLM-controlled escape hatch.

## 1. Motivation

LLM context consumption from verbose command outputs (git diffs, test failures, file contents) inflates token cost and reduces reasoning capacity. RTK is a standalone Rust binary that ships per-command filters producing ~80% median compression on typical dev workflows, and exposes `rtk rewrite` as a single source of truth for hook-style command rewriting.

We want those savings realized by default in our agent, while:
- Preserving today's approval semantics (allowlists keyed on the LLM-requested command).
- Keeping an explicit escape hatch so the LLM can request exact bytes when it matters (e.g. JSON it is about to patch, stderr it is debugging).
- Surfacing the actual executed command in the TUI so behavior is never hidden from the user.

## 2. Goals / Non-goals

**Goals**
- Compress Bash output via `rtk rewrite` for every supported command by default.
- Compress file reads via `rtk read --level minimal` by default.
- Provide a single per-tool `raw: bool` field as the LLM's escape hatch.
- Fail loudly if rtk itself breaks; fail silently (fall through) when rtk just has no equivalent for a command.
- Keep TUI and approval policy transparent about what actually executes.

**Non-goals**
- No always-on `rtk proxy` tracking for commands rtk cannot compress.
- No `--ultra-compact` mode by default.
- No TUI `/rtk` overlay or in-app savings dashboard (`rtk gain` remains a user-shell command).
- No new first-class tools (e.g. `Git`, `Test`) wrapping rtk subcommands directly.
- No per-command allow/denylist of rtk rewrites (toggle at config level only).

## 3. Decisions (from brainstorm)

| # | Topic | Decision |
|---|-------|----------|
| 1 | Transparency model | Hybrid: compression by default, `raw: true` escape hatch. |
| 2 | Bash rewrite mechanism | `rtk rewrite` only; no `rtk proxy` passthrough. |
| 3 | Read integration depth | Hook into Read via `raw: true` escape hatch (tool retains its schema shape). |
| 4 | Enablement | `[rtk] mode = "auto" \| "on" \| "off"`, default `auto`. |
| 5 | Approval + display | Approval matches original command; TUI shows both original and rewritten. |
| 6 | Escape hatch shape | Single `raw bool` field per tool. |
| 7 | Compression strength | Per-tool tuned: plain `rtk rewrite` for Bash; `--level minimal` for Read. |
| 8 | Read default | On by default (`raw: true` opts out). |
| 9 | Failure policy | Hard fail to the LLM on rtk internal errors; `rtk rewrite` exit 1 is not an error. |

## 4. Architecture

### 4.1 New package `internal/rtk`

Thin wrapper around the `rtk` binary. The agent depends on this package; tools receive an `*rtk.Client` via constructor injection, mirroring how `launchDir` and `ReadTracker` are passed today.

```go
// internal/rtk/client.go

type Mode string

const (
    ModeAuto Mode = "auto"
    ModeOn   Mode = "on"
    ModeOff  Mode = "off"
)

type Client struct {
    mode    Mode
    enabled bool
    version string
}

// New returns a Client in the requested mode; does not probe yet.
func New(mode Mode) *Client

// Detect probes `rtk --version`; returns version and whether rtk is usable.
// Sets Enabled according to mode: auto -> ok, on -> must be ok, off -> false.
// When mode=on and !ok, returns a non-nil error the caller must treat as fatal.
func (c *Client) Detect(ctx context.Context) error

func (c *Client) Enabled() bool
func (c *Client) Version() string

// Rewrite runs `rtk rewrite <cmd>` with an internal 5s timeout.
// supported=true  -> rewritten is the replacement command.
// supported=false -> rtk has no equivalent; caller should use original cmd.
// err != nil      -> rtk itself failed (crash, unexpected exit code, bad output, timeout).
func (c *Client) Rewrite(ctx context.Context, cmd string) (rewritten string, supported bool, err error)

// Read runs `rtk read --level minimal -n <path>` with an internal 30s timeout.
// Returns the raw bytes of rtk's stdout (capped at rtkReadMaxBytes = 1 MiB) or an error on non-zero exit.
// The caller is responsible for gating which files get sent here (see 4.3).
func (c *Client) Read(ctx context.Context, path string) ([]byte, error)
```

### 4.2 Bash tool changes (`internal/tools/bash.go`)

Schema gains `Raw`:

```go
type BashInput struct {
    Command   string  `json:"command" jsonschema:"required,description=Shell command executed via bash -c."`
    TimeoutMS flexInt `json:"timeout_ms,omitempty" jsonschema:"description=Max 600000 (10 min). Default 120000 (2 min)."`
    Raw       bool    `json:"raw,omitempty" jsonschema:"description=Skip rtk compression; run the command as-is. Use when exact output bytes matter (diff application, stderr inspection)."`
}
```

Constructor injects rtk client. This is a **signature change** from today's `NewBash(launchDir string) Tool`; callers in `cmd/sam` and anywhere else that constructs tools must be updated.

```go
func NewBash(launchDir string, rtk *rtk.Client) Tool {
    return New[BashInput]("Bash", bashDescription, func(ctx context.Context, in BashInput) (Result, error) {
        return runBash(ctx, in, launchDir, rtk)
    })
}
```

Execution path:

```
effective := in.Command
rewritten := ""
if rtk.Enabled() && !in.Raw {
    r, supported, err := rtk.Rewrite(ctx, in.Command)
    if err != nil {
        return Result{Output: "rtk rewrite failed: " + err.Error(), IsError: true}, nil
    }
    if supported {
        rewritten = r
        effective = r
    }
}
// The rewritten string is still executed via the existing code path:
//     exec.Command("bash", "-c", effective) with the 30 KB limitedWriter
// attached to both stdout and stderr. rtk is NOT spawned as the outer
// process; it runs as the child of bash, meaning the existing framing
// (<stderr>, <exit>, <truncated>) applies unchanged.
```

**Approval policy** (`internal/policy`) continues to match on `in.Command` (the original). No policy-layer changes.

**TUI display** (`internal/tui`) shows the original as the primary line and, when `rewritten != ""`, a secondary dimmed line prefixed with `↳` showing the rewritten command. The existing Bash card renderer gains one optional subline; approval prompts still reference the original text.

Output framing is unchanged: the existing 30KB `limitedWriter`, `<stderr>`, `<exit>`, `<truncated>` tags apply to rtk's output without modification.

### 4.3 Read tool changes (`internal/tools/read.go`)

Schema gains `Raw`:

```go
type ReadInput struct {
    FilePath string  `json:"file_path" jsonschema:"required,description=Absolute path to the file to read."`
    Offset   flexInt `json:"offset,omitempty" jsonschema:"description=1-indexed line number to start from."`
    Limit    flexInt `json:"limit,omitempty" jsonschema:"description=Max number of lines to return. Default 2000."`
    Raw      bool    `json:"raw,omitempty" jsonschema:"description=Skip rtk compression; return exact file contents with line numbers. Use for editing or when bytes matter."`
}
```

Constructor injects rtk client:

```go
func NewRead(tracker *ReadTracker, rtk *rtk.Client) Tool
```

**rtk `read` observed behavior (verified against rtk 0.36.0).**

- Output format is `N │ <content>` (space, pipe, space), not `%6d\t<content>`. This differs from our native output.
- `-m N` does not produce "up to N lines"; rtk chooses what to elide and may emit summary markers such as `// ... 9 more lines (total: 10)` that look like content lines but are not real source lines with a valid line number.
- There is no `--offset` / `--from-line` flag. Any offset math done on rtk's output is unreliable because of the elision markers above.

Consequence: we cannot safely honor explicit `offset` / `limit` windows through rtk. Those are a precision request from the LLM; precision is exactly what rtk trades away.

**Execution path.**

1. Validate absolute path, open file, run `isBinary` check. These stay in Go (we do not want rtk's binary-file behavior).
2. `tracker.Mark(in.FilePath)` — unconditional, preserves the Write/Edit "must Read first" invariant even for compressed reads.
3. Resolve `offset` (default 1) and `limit` (default `defaultReadLimit` = 2000).
4. Decide path:
   - **Native path** (existing `bufio` scan, `%6d\t<line>\n` output) is taken when ANY of:
     - `!rtk.Enabled()`
     - `in.Raw == true`
     - caller supplied an explicit window: `offset > 1` or `limit != 0 && limit != defaultReadLimit`
   - **rtk path** is taken only for default full-file reads.

   Note on the `limit` check: an LLM that explicitly passes `limit = 2000` (the default value) will be routed to the rtk path. This is intentional — we treat "default-equivalent" as "no explicit window requested". If an LLM wants byte-exact output regardless of defaults, it should pass `raw: true`. The Read tool description advertises this behavior.
5. rtk path:
   - call `rtk.Read(ctx, absPath)` → runs `rtk read --level minimal -n <path>`.
   - on non-zero exit or timeout, return `Result{IsError: true, Output: "rtk read failed: " + stderr}`.
   - rtk's own output is returned verbatim as `Result.Output`. We do NOT reformat rtk's `N │` prefixes to our `%6d\t` prefix — reformatting is fragile in the presence of elision markers, and the LLM does not rely on the exact prefix shape (Write/Edit matching is against live file bytes, not Read's returned string).
   - `rtkReadMaxBytes` (1 MiB) caps the stdout buffer inside `rtk.Client.Read`; exceeding the cap is surfaced as an error to the LLM, which can retry with `raw: true`.
6. Return.

**Invariants preserved**
- `ReadTracker` gate still enforced (Write/Edit require prior Read).
- Binary short-circuit still runs before rtk is consulted.
- Native path output shape stays byte-identical to today.
- Explicit offset/limit requests are always honored by the native path.

### 4.4 Config (`internal/config/config.go`)

```go
type RTKConfig struct {
    Mode string `toml:"mode"` // "auto" (default) | "on" | "off"
}

type Config struct {
    // ... existing fields
    RTK RTKConfig `toml:"rtk"`
}
```

Applied in the existing `applyDefaults()` path:
- Empty → `"auto"`.
- Unknown value → log a warning and clamp to `"auto"`.

### 4.5 Startup (`cmd/sam`)

```
rtkClient := rtk.New(rtk.Mode(cfg.RTK.Mode))

// mode=off short-circuits: no probe, Enabled()=false.
// mode=auto: probe; Enabled() = binary found.
// mode=on:   probe; err returned if binary missing.
if err := rtkClient.Detect(ctx); err != nil {
    log.Fatalf("rtk: %v", err)
}
if rtkClient.Enabled() {
    log.Infof("rtk enabled (%s)", rtkClient.Version())
} else {
    log.Infof("rtk disabled (mode=%s)", cfg.RTK.Mode)
}

// Detect MUST complete before tool registration so the first tool call
// sees a consistent Enabled() value without a race.
registry.Register(tools.NewBash(launchDir, rtkClient))
registry.Register(tools.NewRead(readTracker, rtkClient))
```

The client is immutable after `Detect` returns; tools hold a shared pointer and only read `Enabled()` / `Version()`. No locks required.

### 4.6 Logging (`internal/logging`)

Two new structured events. No dashboards or counters — observability is log-driven only.

- `rtk.rewrite` — fields: `cmd` (string), `supported` (bool), `duration_ms` (int), `err` (string, empty on success).
- `rtk.read` — fields: `path` (string), `bytes_in` (int, file size), `bytes_out` (int, rtk stdout length), `duration_ms` (int), `err` (string).

These land in the existing JSON file logger and the in-memory ring buffer used by the debug overlay. No new UI.

The `cmd` field inherits the agent's existing logging posture: raw command strings may contain secrets (auth headers, tokens) because we already log Bash invocations today. rtk integration does not change that posture; any redaction work belongs in the logging layer, not here.

## 5. Failure semantics

| Situation | Handling |
|-----------|----------|
| `rtk.mode=on` and binary not installed at startup | Fatal startup error. |
| `rtk.mode=auto` and binary not installed | `Enabled()=false`; all paths fall through to native Go. Logged once. |
| `rtk.mode=off` | `Detect` is skipped; `Enabled()=false` unconditionally. |
| `rtk rewrite` exits 1 | Not an error. `supported=false`, original command is executed. |
| `rtk rewrite` exits anything else / crashes / emits malformed output / exceeds internal 5s timeout | Tool returns `IsError: true` with rtk stderr. |
| `rtk read` non-zero exit or exceeds 30s internal timeout | Tool returns `IsError: true` with rtk stderr. |
| `rtk read` stdout exceeds `rtkReadMaxBytes` (1 MiB) | Tool returns `IsError: true`; LLM can retry with `raw: true`. |
| Executed rewritten command itself fails | Normal Bash tool behavior — exit code surfaced as today. |
| rtk binary upgraded mid-session (flag semantics change) | Out of scope; any resulting rtk error surfaces per the rows above. Users can restart the agent. |

**Environment passthrough.** `exec.Command` inherits the agent's full `os.Environ()` today (including provider API keys). With rtk inserted, the chain becomes `bash -c "rtk <subcmd> ..."`; rtk inherits the same environment as bash would have. rtk is a local binary we trust (same trust boundary as any rewritten command), so this is accepted. No env filtering layer is introduced.

**Process visibility.** When rtk rewrites a command, `ps aux` and similar OS-level tools show the rewritten command (e.g. `rtk git status`), not the original `git status` the LLM requested. This is acceptable — the TUI surfaces both lines (§4.2), so users have an accurate view inside the agent even if an external monitor only sees the rewritten form.

## 6. Test plan

**`internal/rtk` (new).**
- `Detect` with mock exec: mode=auto, binary missing → disabled, no err.
- `Detect` with mock exec: mode=on, binary missing → err returned.
- `Rewrite` returns `(out, true, nil)` on exit 0.
- `Rewrite` returns `("", false, nil)` on exit 1.
- `Rewrite` returns `(_, _, err)` on exit 2 / process error.
- `Read` happy path returns stdout bytes.
- `Read` non-zero exit returns err with stderr attached.

**`internal/tools/bash_test.go` (extend).**
- rtk disabled → identical behavior to today.
- rtk supported command → rewritten command is what `exec.Command` sees.
- `Raw: true` with rtk enabled → original command executed; no rtk call.
- rtk rewrite error → `IsError: true`, rtk stderr surfaced.

**`internal/tools/read_test.go` (extend).**
- rtk disabled → byte-identical output to today.
- rtk enabled, no window args → rtk is invoked; output is rtk's stdout verbatim.
- rtk enabled with `offset > 1` → native `bufio` path taken; no rtk call.
- rtk enabled with `limit` set to a non-default value → native path taken; no rtk call.
- `Raw: true` with rtk enabled → native `bufio` path; no rtk call.
- rtk read non-zero exit → `IsError: true`.
- rtk stdout > `rtkReadMaxBytes` → `IsError: true` with a cap-exceeded message.
- Concurrent reads through the rtk path still call `tracker.Mark` for each file (regression guard for the Write/Edit invariant).

**Config.**
- Empty `[rtk]` block → mode resolves to `"auto"`.
- Unknown mode string → warning logged, clamped to `"auto"`.
- `mode=off` → `Detect` is not invoked even if rtk is on PATH.
- `mode=auto`, rtk missing → one-time info log, subsequent tool calls do not re-log.

**Bash edge cases.**
- Rewritten command that itself times out → existing timeout / SIGTERM path still fires.
- Approved `git status` with rtk enabled executes the rewritten form without triggering a second approval prompt.

## 7. Rollout

Single PR:

1. Land `internal/rtk` package with tests (mockable via an `execer` interface so CI doesn't need rtk installed).
2. Wire Bash + Read with the new constructor signatures; update `cmd/sam`.
3. Default config resolves to `rtk.mode = "auto"` out of the box.
4. Update `README.md` with the `[rtk]` block, the `raw: true` escape hatch, and the approval/display behavior.

Users without rtk installed see no behavior change (auto → disabled, native paths everywhere). Users with rtk installed get compression on the first run; no opt-in required.

## 8. Open questions

None at design time. Items deferred to follow-up work:
- Whether to surface `rtk gain` output in the TUI debug overlay (rejected for v1 — logs suffice).
- Whether to expose `--ultra-compact` via a second config knob (rejected for v1 — rtk's per-command filters already tuned).
- Whether to allow per-command rtk suppression via policy (rejected — use `raw: true` at the call site or `rtk.mode=off` globally).
