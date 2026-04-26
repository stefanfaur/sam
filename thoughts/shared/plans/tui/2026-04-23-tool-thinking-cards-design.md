# TUI Tool + Thinking Cards Design

**Date:** 2026-04-23
**Domain:** tui
**Status:** design — awaiting review
**Related:** skills system (`thoughts/shared/plans/skills/2026-04-22-skills-system-implementation.md`) established the `skillCardState` pattern this design extends.

---

## Problem

Current TUI renders tool calls and thinking blocks as flat lines:

- Tool call: `● Bash  <preview>` followed by up to 10 lines of output then `… N more lines`.
- Thinking: left-border italic block with the full thought visible.

Issues:

1. **Output noise.** Tool-heavy turns fill scrollback with raw command output. The conversation gets buried.
2. **No animation.** Running tools show no indication they're still executing until the result arrives.
3. **Thinking blocks are loud then invisible.** While streaming they dominate the view; after streaming ends nothing marks that the model thought — the boundary between "thought then answered" and "answered directly" is lost in scrollback.
4. **Inconsistent visual vocabulary.** The new skill card (rounded border, spinner header, meta row, expand hint) sets a nicer bar. Tool and thinking rendering feels dated next to it.

Goal: apply the skill-card visual pattern to tool calls and thinking blocks, collapse verbose output behind expansion commands, and leave a compact settled marker in scrollback so the user always knows what the model did.

---

## User-Facing Behavior

### Tool cards

Every tool call renders as a single rounded-border card that transitions through three visual states in place during the turn, then settles to a final form in scrollback.

**Running state** (spinner rotates every 80ms, header uses accent color). The summary slug is literally `running…` — no line count, no elapsed — until the result arrives:

```
╭─────────────────────────────────────────────╮
│ ⠋ Bash · running…                           │
│ go test ./internal/tui/...                  │
╰─────────────────────────────────────────────╯
```

**Done success state:**

```
╭─────────────────────────────────────────────╮
│ ● Bash · 47 lines · 1.2s        [/show-tool 3] │
│ go test ./internal/tui/...                  │
│ PASS                                        │  ← first line peek, dim italic
╰─────────────────────────────────────────────╯
```

**Done error state** (border flips to `ErrorFg`, glyph `✗`, peek in error color):

```
╭─────────────────────────────────────────────╮
│ ✗ Bash · 12 lines · 0.4s        [/show-tool 3] │
│ go test ./internal/tui/...                  │
│ FAIL: TestFoo                               │  ← first line of error
╰─────────────────────────────────────────────╯
```

**Per-tool summary slug** (the middle meta fragment):

| Tool                        | Summary                                      |
| --------------------------- | -------------------------------------------- |
| `Bash`, `Read`, `Grep`, `Glob` | `N lines · <elapsed>`                     |
| `Edit`                      | `N lines edited · <basename>`                |
| `Write`                     | `N lines written · <basename>`               |
| `Task` / subagent           | `~N tok · <elapsed>`                         |
| Fallback (unknown tool)     | `N lines · <elapsed>`                        |

`N` for `Edit` counts lines in `new_string` (`strings.Count(new, "\n") + 1`). `N` for `Write` counts lines in `content`. `N` for read/bash-like tools counts lines in stdout. `~N tok` uses the existing `estimateTUITokens` helper (chars/4).

**First-line peek.** `strings.SplitN(output, "\n", 2)[0]`, truncated to `cardWidth - 6` chars. For errors, first line of error output in `ToolError` style. Not shown for `Edit` / `Write` (they have no meaningful first-line output).

**Card width.** Full terminal width, same as the skill card. Wraps via lipgloss.

**Narrow-terminal hint layout.** The expand hint `[/show-tool N]` sits inline with the meta row only when everything fits: `len(glyph+name+meta) + len(hint) + 2 (separator) <= innerWidth`. Otherwise the hint drops to its own right-aligned line below the meta row. Measured using `lipgloss.Width()` on styled strings. Same rule applies to the thinking settled card.

**Truncation happens at render time, not capture time.** `toolCardState.Output` holds the full stdout forever (for ring expansion); the first-line peek and the tool-input preview line are computed inside `renderToolCard` from the current card inner width. They are not fields on the state. A terminal resize mid-turn produces correct truncation on the next spinner tick without cache invalidation.

### Thinking card

Two streaming modes, selectable via settings (`tui.thinking.stream_mode`, default `"full"`):

**Mode A — full (default):**

```
╭─────────────────────────────────────────────╮
│ ⠋ thinking…                                 │   ← accent, bold, italic
│                                             │
│ The user wants me to refactor the …         │   ← italic, muted, on subtle bg
│ I should start by reading the update loop … │
╰─────────────────────────────────────────────╯
```

Body area has a subtle background color — `lipgloss.Color("235")` on dark backgrounds, `"254"` on light — autodetected once per `Apply(width)` via `lipgloss.HasDarkBackground()`. Terminals composite solid colors, so this reads as "washed" rather than truly transparent, but gives the desired visual tint.

**Mode B — header only:**

```
╭─────────────────────────────────────────────╮
│ ⠋ thinking… 312 chars                       │
╰─────────────────────────────────────────────╯
```

Char counter updates as thought streams. No body, no bg tint needed.

**Settled state (both modes collapse identically):**

```
╭─────────────────────────────────────────────╮
│ 💭 thought for 4.2s · 312 chars   [/show-thinking 1] │
╰─────────────────────────────────────────────╯
```

Single-row card with `💭` marker. Stays in scrollback as proof the model thought. User can expand via `/show-thinking N`.

### Expansion commands

Two new slash commands, mirroring the existing `/show-skill`:

- **`/show-tool [N]`** — prints the full tool dump to scrollback with no border:

  ```
  ● Bash · 47 lines · 1.2s
  $ go test ./internal/tui/...
  ---
  PASS
  ok  example.com/sam/internal/tui  0.342s
  … (full output) …
  ```

  If `N` is omitted, uses the most recent tool in the ring. Unknown index prints a short error line. Index numbers are in-memory only — they don't survive restarts (see Non-goals).

- **`/show-thinking [N]`** — prints the full thought body. If `N` is omitted, uses the most recent thinking entry in the ring; unknown index prints a short error line:

  ```
  💭 thought for 4.2s
  ---
  The user wants me to refactor the tool rendering …
  (full body)
  ```

Both commands appear in suggestions (`suggestionsFor`) and help text (`helpTextFor`).

---

## Architecture

### State model

Extend `pendingTurn` with explicit per-kind card slots. All live-rendered cards mutate through this struct; on `TurnDone` they flush to scrollback in chronological order and then get recorded in ring buffers for expansion.

```go
type pendingTurn struct {
    events    <-chan Event
    raw       []rune
    committed int
    thinkRaw  []rune
    skill     *skillCardState
    thinking  *thinkingCardState   // new
    tools     []*toolCardState     // new, ordered by start time
    done      bool
}

type toolCardState struct {
    ID        string           // provider tool_use_id, used to match call→result
    Name      string
    Input     json.RawMessage   // raw; preview/first-line computed at render time
    Output    string            // full stdout/stderr merged
    Lines     int               // computed on result arrival; 0 while running
    EditLines int               // for Edit, computed on result arrival
    Bytes     int               // for Write / Task
    IsError   bool
    Cancelled bool              // set during flush if EndedAt still zero
    StartedAt time.Time
    EndedAt   time.Time         // zero while running
    Index     int               // for /show-tool N
}

type thinkingCardState struct {
    Text      string
    StartedAt time.Time
    EndedAt   time.Time          // zero while streaming
    Index     int
}
```

### Lifecycle

Mirrors the proven `skillCardState` flow. Settlement is inference-based because the agent event stream has no explicit `ToolCallStarted` or `ThinkingDone` events — just `agent.ToolCall`, `agent.ToolResult`, `agent.ThinkingDelta`, `agent.TextDelta`, `agent.TurnDone`, `agent.ErrorEvent`.

1. **Event arrival** mutates pending state rather than calling `tea.Printf` directly.
   - `agent.ToolCall{ID, Name, Input}` → append a new `toolCardState` to `pending.tools` with `StartedAt = now`. Also counts as the implicit "thinking done" signal: if `pending.thinking != nil && thinking.EndedAt.IsZero()`, set `thinking.EndedAt = now`.
   - `agent.ToolResult{ID, Name, Output, IsError}` → find card by `ID`. If found, fill `Output` / `Lines` / `EditLines` / `Bytes` / `IsError`, set `EndedAt = now`. (`FirstLine` and any preview lines are not stored — they're computed at render time.) If not found (shouldn't happen, but defensively), create a new card with zero `StartedAt` and treat it as already settled.
   - `agent.ThinkingDelta{Text}` → allocate `pending.thinking` lazily on first delta (`StartedAt = now`), append text.
   - `agent.TextDelta{Text}` → existing path (appends to `pending.raw`). Also counts as implicit thinking-done: if `pending.thinking != nil && thinking.EndedAt.IsZero()`, set `thinking.EndedAt = now`.
   - `agent.TurnDone` / `agent.ErrorEvent` → flush (see step 4); also settles thinking if still open.
2. **Live view** (`View()`) composes in chronological order: `[thinking card if any] + [tool cards in start order] + [assistant tail] + [skill card]`. Tool cards whose `EndedAt.IsZero()` render in running state; the rest render in settled state.
3. **Spinner tick** (`spinnerTickMsg`, 80ms) advances the frame counter; `View()` reads it. Ticks schedule themselves while `pending != nil`.
4. **Flush on turn end.** Both `agent.TurnDone` and `agent.ErrorEvent` trigger the same flush path (on error, the error message also prints via the existing error handler). Flush order:
   - thinking card (if present) — settled single-line form →
   - tool cards in start-time order — any still-running ones settle as "cancelled" (see below) →
   - assistant committed text (unchanged path) →
   - skill card (existing flow).

   Each flush calls `tea.Printf` with the settled card string, then records to the appropriate ring buffer.

**Cancelled / still-running at flush.** A tool card whose `EndedAt.IsZero()` at flush time (turn cancelled, provider disconnected, user hit Ctrl-C) renders with:
  - Glyph `◌` instead of `●` / `✗`
  - Border: `Muted` color (not accent, not error)
  - Summary: `cancelled · <elapsed>`
  - No peek line (no output captured)
It still gets a `[/show-tool N]` hint and a ring entry with empty `Output`.

### Ring buffers

```go
const (
    maxRecentTools    = 32
    maxRecentThinking = 32
)

type toolInvocation struct {
    Header  string    // "● Bash · 47 lines · 1.2s"
    Input   string    // preview line
    Output  string    // full stdout
    IsError bool
}

type thinkingInvocation struct {
    Header string     // "thought for 4.2s · 312 chars"
    Body   string
}

// Model gets:
recentTools    []toolInvocation
recentThinking []thinkingInvocation
```

Helpers `recordTool`, `recordThinking` pop the front when capacity is reached — same shape as the existing `recordInvoke` for skills.

**Index allocation — monotonic counter.** `Model` holds two `uint64` counters, `nextToolIndex` and `nextThinkingIndex`, both initialized to `1`. Each `toolCardState.Index` is assigned from `nextToolIndex++`; each `thinkingCardState.Index` from `nextThinkingIndex++`. Indices are never reused.

Ring entries now also carry their `Index`:

```go
type toolInvocation struct {
    Index   int       // monotonic, matches the card's printed hint
    Header  string
    Input   string
    Output  string
    IsError bool
}

type thinkingInvocation struct {
    Index  int
    Header string
    Body   string
}
```

Expansion resolves `/show-tool N` by linear scan over `m.recentTools` for an entry with matching `Index`. Misses print a one-line error. Ring still caps at 32 — after wraparound, the 33rd tool evicts the first; any stale `[/show-tool 1]` rendered earlier in scrollback will resolve to "unknown index" rather than silently returning the wrong entry.

Same scheme for thinking.

### Performance note

Live cards re-render on every 80ms spinner tick. The skill card has validated this cost for a single card. Tool-heavy turns with many in-flight cards will produce larger `View()` strings each tick. For v1 we accept unmitigated per-tick re-render — same pattern as the skill card. If parallel-tool sessions show flicker or CPU issues in practice, add `toolCardState.rendered` caching with invalidation on width change. Not built day one.

### Event source

Events come from `internal/agent/events.go` via the event channel consumed in `internal/tui/update.go` (the switch at lines ~490–537 today). The specific types used by this design: `agent.ToolCall`, `agent.ToolResult`, `agent.ThinkingDelta`, `agent.TextDelta`, `agent.TurnDone`, `agent.ErrorEvent`. `agent.ToolCall.ID` and `agent.ToolResult.ID` are verified to carry matching identifiers — that's the key for the call→result match. No new event types are introduced.

---

## Theme changes

New styles in `internal/tui/theme.go`, built inside `Apply(width)`:

```go
// Tool cards
t.ToolCard        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
                      BorderForeground(t.Accent).Padding(0, 1).MarginTop(1)
t.ToolCardError   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
                      BorderForeground(t.ErrorFg).Padding(0, 1).MarginTop(1)
t.ToolCardHeader  = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
t.ToolCardMeta    = lipgloss.NewStyle().Foreground(t.Muted)
t.ToolCardPeek    = lipgloss.NewStyle().Foreground(t.Muted).Italic(true)
t.ToolCardPeekErr = lipgloss.NewStyle().Foreground(t.ErrorFg).Italic(true)

// Thinking card
t.ThinkingCard     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
                       BorderForeground(t.Accent).Padding(0, 1).MarginTop(1)
t.ThinkingCardBody = lipgloss.NewStyle().Foreground(t.Muted).Italic(true).
                       Background(t.subtleBg())
t.ThinkingCardMeta = lipgloss.NewStyle().Foreground(t.Muted)
```

Helper:

```go
// Evaluated once in NewTheme (not in Apply) since HasDarkBackground may hit
// the terminal. Cached on the Theme struct as `dark bool`.
func (t *Theme) subtleBg() lipgloss.Color {
    if t.dark {
        return lipgloss.Color("235")
    }
    return lipgloss.Color("254")
}
```

`NewTheme` calls `lipgloss.HasDarkBackground()` once and stores the result. `Apply(width)` reads the cached flag — no per-resize TTY probe.

**`ThinkingCardBody` must set `Width(innerWidth)`** to force even background fill across wrapped lines. Without this, wrapped italic body across varying line widths shows ragged tint edges. `innerWidth` = terminal width minus card chrome (border + padding = 4).

**Style retention map** after the refactor:

| Style             | Retained? | Used for                                     |
| ----------------- | --------- | -------------------------------------------- |
| `ToolHeader`      | delete    | superseded by `ToolCardHeader`               |
| `ToolInput`       | delete    | superseded by inline render in `renderToolCard` |
| `ToolResult`      | keep      | `/show-tool N` plain expansion dump          |
| `ToolError`       | keep      | `/show-tool N` error expansion; `ToolCardPeekErr` peek |
| `Thinking`        | delete    | superseded by `ThinkingCardBody`             |
| `ThinkingHeader`  | keep      | `/show-thinking N` expansion dump header     |

Delete the two marked `delete` only after confirming no other callers via a codebase search as part of implementation.

---

## Settings

Add to `internal/tui/settings.go`:

```go
type ThinkingSettings struct {
    StreamMode string `toml:"stream_mode"` // "full" | "header"
}

// on TUISettings (or equivalent root):
Thinking ThinkingSettings `toml:"thinking"`
```

Default `"full"`. On load, unknown values fall back to `"full"`.

Settings modal: add a single select row to the existing first/display tab — two options, `full` and `header`. No new tab. If the codebase already has a clean slot for display prefs, reuse it; otherwise piggyback on the general tab.

---

## Rendering API

New / changed functions in `internal/tui/render.go`:

```go
// Replaces renderToolCall / renderToolResult. State is derived from tc:
// - tc.EndedAt.IsZero() && !tc.Cancelled → running (spinner)
// - tc.Cancelled                         → cancelled (◌, muted border)
// - tc.IsError                           → error (✗, error border)
// - else                                 → success (●, accent border)
func renderToolCard(t *Theme, tc *toolCardState, now time.Time,
                    spinnerFrame, innerWidth int) string

// Unified tool summary slug builder.
// Returns "running…" when tc.EndedAt.IsZero() && !tc.Cancelled.
// Returns "cancelled · <elapsed>" when tc.Cancelled.
// Otherwise returns per-tool summary per the table above.
func toolSummary(tc *toolCardState, now time.Time) string

// Thinking card. Mode is "full" or "header". State derived from tc:
// - tc.EndedAt.IsZero() → live (spinner + body per mode)
// - else                → settled (single-line 💭 marker, mode ignored)
func renderThinkingCard(t *Theme, tc *thinkingCardState, now time.Time,
                        spinnerFrame int, mode string, innerWidth int) string
```

`renderSkillCard` stays as-is.

Expansion printers in `update.go`:

```go
func printToolExpansion(m *Model, n int) tea.Cmd      // /show-tool N
func printThinkingExpansion(m *Model, n int) tea.Cmd  // /show-thinking N
```

Both `tea.Printf` a plain (borderless) dump. Out-of-range `n` prints a one-line error; the handler never panics.

---

## Commands

In `internal/tui/commands.go`:

```go
const (
    CmdShowTool     Command = "/show-tool"
    CmdShowThinking Command = "/show-thinking"
)
```

Extend `parseCommand` to recognize these (case-insensitive, numeric arg optional — same rules as `/show-skill`). Extend `suggestionsFor` and `helpTextFor` to advertise them alongside `/show-skill`.

---

## Tests

### render.go

- `TestRenderToolCard_States` — four visual states render correctly: running (spinner + `running…`), success (`●` + per-tool summary), error (`✗` + error border), cancelled (`◌` + muted border + `cancelled · <elapsed>`); `[/show-tool N]` hint present in all settled states and absent during running.
- `TestRenderToolCard_NarrowTerminalHintWraps` — when inner width is small, hint drops to its own line.
- `TestToolSummary_PerTool` — cases for `Bash`, `Read`, `Edit`, `Write`, `Task`, unknown-tool fallback, running, cancelled. Verifies exact summary strings.
- `TestRenderThinkingCard_StreamModes` — `"full"` live shows body with bg, `"header"` live shows char count only, both collapse to identical settled single-line form.
- `TestRenderThinkingCard_Elapsed` — settled string contains `formatElapsed(now - StartedAt)` when EndedAt is set.
- `TestRenderToolCard_TruncatesAtRenderTime` — changing inner width between two renders of the same state produces different-length peek without mutating `tc.Output`.

### app.go

- `TestModel_RecordToolBoundedRing` — mirror of existing `TestModel_RecordInvokeBoundedRing`, cap = `maxRecentTools`.
- `TestModel_RecordThinkingBoundedRing` — same shape, cap = `maxRecentThinking`.

### commands.go

- `TestParseCommand_ShowTool` — `/show-tool`, `/show-tool 3`, `/SHOW-TOOL 7`.
- `TestParseCommand_ShowThinking` — analogous.
- `TestCommandSuggestions_IncludesShowToolAndThinking` — both appear in `suggestionsFor(nil)`.

### update.go

- `TestUpdate_ToolFlushOrder` — synthesize a pending turn with a thinking card + two tool cards, fire `agent.TurnDone`, assert flush order is thinking → tool[0] → tool[1] → assistant tail → skill.
- `TestUpdate_ToolCardMatchesByID` — `agent.ToolCall` then `agent.ToolResult` with matching ID updates the same card; result with unknown ID creates a defensive settled card rather than panicking.
- `TestUpdate_ToolCallSettlesThinking` — pending thinking still open, then a `agent.ToolCall` arrives; thinking `EndedAt` gets set. Same test for `agent.TextDelta` arrival.
- `TestUpdate_CancelledToolOnError` — pending tool running, then `agent.ErrorEvent` fires; tool flushes with `Cancelled = true`, glyph `◌`, empty ring `Output`.
- `TestUpdate_CancelledToolOnTurnDoneWithoutResult` — pending tool running, `agent.TurnDone` arrives before its result; tool flushes as cancelled.

---

## Rollout

Single PR. No migration — `thinking.stream_mode` default `"full"` produces behavior closest to current (full thought visible during stream). Ring buffers start empty. Existing `/show-skill` and skill card unchanged.

### Risks

- **Redraw cost with parallel tools.** 80ms ticks rebuild `View()` including every in-flight tool card. Accepted for v1 — matches the skill card pattern. If flicker surfaces with >3 parallel tools, add `rendered` caching with width-change invalidation.
- **Width changes.** Terminal resize calls `Apply(w)` which rebuilds lipgloss styles. Because truncation happens at render time (not capture time), state is safe across resizes. `ThinkingCardBody` with `Width(innerWidth)` re-fills the bg on next render.
- **Event ordering races.** If `agent.ToolResult` arrives before its matching `agent.ToolCall` (shouldn't happen, but provider buffering could theoretically reorder), the handler creates the card on the result with zero `StartedAt` and settles it immediately.
- **Ring index stability across restarts.** Indices are in-memory; scrollback persists. A user who scrolls up after a restart and types `/show-tool 7` will hit the "unknown index" error path. Acceptable — noted in Non-goals.

### Non-goals

- No collapse/expand animations (terminal redraw is too coarse for smooth motion).
- No in-memory scrollback reparsing. Settled cards live in the terminal's scrollback as plain strings after `tea.Printf`; re-rendering on theme change is out of scope.
- No per-tool color coding beyond success / error / cancelled.
- No interactive cursor on cards (expansion is via `/show-tool N` typed command, not focus+Enter).
- No persistence of ring buffers across process restarts. Index numbers printed on cards only resolve within the current session.
- No caching of rendered card strings in v1. Accepted re-render cost per tick. Optimization deferred until measured.

---

## File touch list

1. `internal/tui/app.go` — add `toolCardState`, `thinkingCardState`, `toolInvocation`, `thinkingInvocation` types; add `recentTools`, `recentThinking` fields + helpers; extend `pendingTurn`.
2. `internal/tui/render.go` — add `renderToolCard`, `toolSummary`, extend `renderThinkingCard` for three modes × two states; keep `renderSkillCard`.
3. `internal/tui/theme.go` — add ToolCard* and ThinkingCard* styles; add `subtleBg()` helper.
4. `internal/tui/update.go` — route `agent.ToolCall` / `agent.ToolResult` / `agent.ThinkingDelta` / `agent.TextDelta` into pending state; flush on `agent.TurnDone` and `agent.ErrorEvent`; add `CmdShowTool` / `CmdShowThinking` handlers.
5. `internal/tui/commands.go` — add command constants, extend parsing + suggestions + help.
6. `internal/tui/settings.go` — add `ThinkingSettings.StreamMode`.
7. `internal/tui/settings_modal.go` (or a new `settings_display.go`) — stream-mode select row.
8. Tests as listed above.

---

## Open questions for implementation planning

None for design — all user-facing decisions and architectural choices are locked. Implementation-planning phase should confirm:

- Whether there's an existing display/general tab in `settings_modal.go` or a new one is warranted for the thinking-mode select.
- Whether the `agent.Task` tool is currently reachable in this TUI or whether the `~N tok · elapsed` summary for it is defensive / unused today. If unused, the per-tool summary entry is harmless but can be dropped without loss.
