# Scrollback-First TUI with Streaming Markdown — Design

**Date:** 2026-04-22
**Status:** Design approved by user; spec under review
**Scope:** `internal/tui/` rewrite of rendering pipeline
**Deps:** `github.com/charmbracelet/bubbletea v1.3.10`, `github.com/charmbracelet/glamour v1.0.0`

## Problem

Current TUI has two related issues:

1. **Flicker during streaming.** Assistant markdown is streamed with a typewriter effect and re-rendered through glamour every 4 ticks. Each tick calls `viewport.SetContent` on the full history, which repaints the whole viewport. A stable-prefix cache limits glamour cost but the plain-text tail and ANSI prefix boundaries still shift on every reveal, causing visible flashing.
2. **No terminal scrollback.** History lives inside a `bubbles/viewport.Model`. The terminal's native scrollback only sees the viewport area; scrolling up past old turns with the terminal's own scrollbar/wheel does not work.

## Goals

- Eliminate flicker during streaming assistant text.
- Make prior turns appear in the real terminal scrollback, scrollable via the terminal's own UI.
- Preserve styling quality from glamour for finalized output.
- No regression to tool-call UX, approval modal, thinking card, status bar, input box.

## Non-Goals

- Reflowing already-committed output on terminal resize (matches crush, claude-code, opencode behavior).
- Streaming tables mid-row (requires blank-line terminator to commit).
- Typewriter/character-by-character reveal animation.

## Key Research Findings

- `tea.Printf` / `tea.Println` commit output above the bubbletea live frame, into real terminal scrollback. Requires **inline mode** (no `tea.WithAltScreen`) — silent no-op under alt-screen.
- `glamour` has no streaming API. `Write()` buffers; `Close()`/`Render()` processes the full document in one pass.
- No Go library exists for "is this partial markdown safe to render." Heuristic (double-newline + code-fence tracking) is the standard approach.
- Both `charmbracelet/crush` and `sst/opencode` re-render the full streamed content per delta — they flicker too. Our design avoids that.
- `glamour.WithWordWrap(width)` is set at renderer construction. Resize requires a new renderer; caches invalidate.

## Architecture

### Two Regions

1. **Scrollback (above the live frame)** — immutable, permanent terminal history. Written via `tea.Printf` cmds. Contents:
   - User messages (`> prompt text`)
   - Finalized assistant markdown blocks (glamoured)
   - Tool-call lines
   - Tool-result blocks
   - Error lines
   - Info lines (`/help`, `/cwd`, `/model set …`)
2. **Live frame (below)** — redrawn by `View()`. Top-to-bottom:
   - Live partial markdown tail (plain text, un-committed)
   - Active thinking card (ephemeral — never committed to scrollback)
   - Active approval panel *or* modal (mutually exclusive with input)
   - Status bar
   - Input box (with optional suggestion dropdown above)

### Program Configuration

- `tea.NewProgram(model, tea.WithContext(ctx))` — unchanged. Already inline-mode (no `WithAltScreen`). Good.
- No `viewport.Model` in the hot path. Drop `m.viewport`, `m.lastVP`, `m.history`, `rebuildViewport`.
- Debug overlay (Ctrl-L) is the **one exception**: it enters alt-screen for the duration, then exits. Inline-mode scrollback does not coexist with an overlay full-screen log viewer cleanly.

### `Model` Changes

Remove:
- `history []renderedBlock`
- `viewport viewport.Model`
- `lastVP string`

Add:
- `scanner *blockScanner` — persisted across deltas for fence-state continuity.

Keep:
- `glam *glamour.TermRenderer`, `status`, `input`, `approval`, `modal`, `debug`, `ring`, `factory`, `suggest`, `width`, `height`, `agent`, `lastCtrlC`.

### `pendingTurn` Changes

Remove: `shown`, `thinkShown`, `ticking`, `tickCount`, `rendered`, `stablePrefix`, `stableRendered`, `toolRow`, `thinkIdx`.

Keep / add:
- `raw []rune` — full assistant text received so far.
- `committed int` — rune index in `raw` that has been flushed to scrollback.
- `thinkRaw []rune` — live thinking text (rendered in View(), never committed).
- `done bool` — TurnDone received; flush remaining tail.
- `events <-chan Event`.

### Streaming Pipeline

#### Block Scanner

New file: `internal/tui/stream.go`.

```go
type blockScanner struct {
    fenceOpen bool
    fenceMark string // "```" or "~~~"
    fenceIndent int  // leading spaces on the fence-open line (0..3)
    inIndentedCode bool // 4-space code block state
    inHTMLBlock bool    // CommonMark type-1 HTML block (script/pre/style/textarea)
}

// SafeSplit returns the largest rune index into `tail` such that tail[:idx]
// ends on a closed-block boundary. Returns 0 if no safe split exists.
// Does NOT mutate scanner state.
func (s *blockScanner) SafeSplit(tail []rune) int

// Advance updates scanner state to reflect having committed `prefix`.
// Called after a successful commit.
func (s *blockScanner) Advance(prefix []rune)
```

**Input normalization (performed once at delta-append time, before scanning):**

- Strip a leading BOM (U+FEFF) if present on the very first delta.
- Normalize `\r\n` → `\n`, bare `\r` → `\n`. The scanner assumes `\n`-only line endings.

**Line classification.** For each line the scanner computes one of:

| Type | Match rule |
|---|---|
| `blank` | empty or only whitespace |
| `fence-open` | 0–3 leading spaces, then ` ``` ` or ` ~~~ ` (≥3 chars), when `!fenceOpen && !inIndentedCode && !inHTMLBlock` |
| `fence-close` | 0–3 leading spaces, then the same marker as `fenceMark` (≥ open length, no trailing non-space), when `fenceOpen` |
| `setext-underline` | entire line is `=+` or `-+`, preceded by a non-blank paragraph line |
| `html-block-open` | one of the CommonMark type-1 starts (`<script`, `<pre`, `<style`, `<textarea`, case-insensitive) at start-of-line |
| `html-block-close` | contains the matching end tag, when `inHTMLBlock` |
| `indented-code-start` | `inIndentedCode == false` and line starts with ≥4 spaces and previous line was blank |
| `table-row` | starts with `|` (delimiter row or data row) |
| `other` | default paragraph / list / quote / heading text |

**State transitions (per line):**

- `fence-open` → set `fenceOpen=true`, `fenceMark`, `fenceIndent`. Never a split boundary.
- `fence-close` → clear fence state. The blank line *after* this line is the split candidate.
- `blank` + `!fenceOpen && !inHTMLBlock` → potential split candidate (see rejection rules below).
- `html-block-open` → set `inHTMLBlock=true`. Close either via `html-block-close` or via `blank` (CommonMark type-6/7 rule; for v1 we terminate type-1 on its end tag, others on blank — treat all uniformly as "end on blank" for simplicity).
- Any other → paragraph/list continuation.

**Split-candidate rejection rules** (checked before accepting a `blank` line as split):

- `fenceOpen` → reject.
- `inHTMLBlock` → reject.
- Next non-blank line (if present in `tail`) is a `setext-underline` → reject (must stay with the preceding paragraph).
- Indented code block: a blank line *inside* an indented code block is a valid CommonMark continuation. We do not track indented-code-block interior perfectly; known cosmetic limitation (see Risks). For v1, do not treat indented-code specially on splitting — splits inside one will render as two code regions.
- Tables: v1 rule is "split on any valid blank line." If no blank line appears mid-table, nothing commits until the turn ends. This matches typical model output (blank line after table).

**Commit boundary is always `\n\n`** (end of blank line). The largest accepted candidate index is returned. Scanner tracks fence/HTML state across calls by persisting via `Advance`.

#### TextDelta Handler

```go
func (m *Model) handleTextDelta(ev agent.TextDelta) tea.Cmd {
    normalized := normalizeDelta(ev.Text) // CRLF→LF, strip BOM on first delta
    m.pending.raw = append(m.pending.raw, []rune(normalized)...)
    m.status.state = "responding"

    tail := m.pending.raw[m.pending.committed:]
    idx := m.scanner.SafeSplit(tail)
    wait := waitAgent(m.pending.events)
    if idx == 0 {
        return wait
    }
    prefix := tail[:idx]
    rendered := safeGlamourRender(m.glam, string(prefix))
    m.scanner.Advance(prefix)
    m.pending.committed += idx
    // Sequence, not Batch: the Printf Cmd must land in scrollback before the
    // next View() redraw (which shows the shrunken tail). Batch leaves order
    // unspecified and can cause a one-frame duplicate tail.
    return tea.Sequence(tea.Printf("%s", rendered), wait)
}
```

No ticker. No typewriter. The tail is rendered in `View()` as **plain text** (no glamour), so every frame is a cheap string draw. When a commit fires, scrollback grows by exactly the rendered block height and the tail shrinks by the committed range — the transition is seamless because both halves of the live frame are drawn in the same update cycle.

#### TurnDone Handler

```go
func (m *Model) handleTurnDone() tea.Cmd {
    if m.pending == nil {
        // Idempotent: TurnDone can arrive after ErrorEvent already cleared
        // pending, or twice if the provider is buggy.
        m.status.state = "idle"
        m.input.Focus()
        return func() tea.Msg { return turnClosedMsg{} }
    }
    var flush tea.Cmd
    if m.pending.committed < len(m.pending.raw) {
        tail := m.pending.raw[m.pending.committed:]
        rendered := safeGlamourRender(m.glam, string(tail))
        flush = tea.Printf("%s", rendered)
    }
    m.pending = nil
    m.status.state = "idle"
    m.input.Focus()
    // Sequence (not Batch): flush must land in scrollback before the live
    // frame redraws without the tail.
    return tea.Sequence(flush, func() tea.Msg { return turnClosedMsg{} })
}
```

`safeGlamourRender` wraps `m.glam.Render(s)` with `defer recover()` and returns the raw string on panic or error, with trailing newlines trimmed. Older glamour releases have panicked on unterminated fences; belt-and-braces.

### Tool Call / Result

`ToolCall` event:
1. If `m.pending.raw` has un-committed text, flush it (glamour-render the remainder, `tea.Printf`).
2. If a live thinking card exists, drop it without committing (per design decision — thinking is ephemeral).
3. `tea.Printf(renderToolCall(ev.Name, ev.Input))`.

`ToolResult` event:
- `tea.Printf(renderToolResult(ev.Name, ev.Output, ev.IsError))`.
- Bump `m.status.iter`.

No `toolRow` correlation needed — call and result print in order.

### Thinking Card

- `ThinkingDelta` appends to `m.pending.thinkRaw`. `View()` renders `renderThinkingCard(thinkRaw, streaming=true)` above the status bar.
- On any non-thinking agent event, or on `TurnDone`, clear `thinkRaw` — no commit.
- Simpler than current; no `thinkIdx` bookkeeping.

### Approval

Unchanged from current design. `m.approval` sits in the live frame. On respond, approval cleared, events resumed via `waitAgent`.

### User / Info / Error

All routed through a small helper:

```go
func printBlock(content string) tea.Cmd { return tea.Printf("%s", content) }
```

- `startTurn(text)` — before calling `m.agent.Submit`, emit `printBlock(m.renderUserMsg(text))`.
- `addInfo(text)` — `printBlock(infoStyle.Render(text))`.
- `ErrorEvent` — `printBlock(renderError(ev.Err))`.

### Resize

`tea.WindowSizeMsg`:
1. Update `m.width`, `m.height`, `m.status.width`.
2. Resize `m.input` and debug viewport.
3. Rebuild `m.glam` via `glamourForWidth(w)`.
4. No viewport to rebuild.
5. Already-committed scrollback keeps old width — accepted.
6. Live tail in View() re-renders at new width on next frame.

### `/clear`

Per user decision: **soft clear**.
- Emit `tea.ClearScreen` cmd — wipes the visible region only, scrollback remains.
- Reset agent state as today (`m.agent.Reset()` on `/reset`; `/clear` is visual-only).
- **If a turn is in flight**, `/clear` must also:
  - Cancel via `m.agent.CancelCurrent()`.
  - Reset `m.pending = nil`, `m.scanner = &blockScanner{}` — otherwise open fence state + un-committed `raw` leak into the next turn.
  - Status → `idle`, input focus.

### Debug View (Ctrl-L)

Two cmds wrap the toggle:
- On enter: `tea.EnterAltScreen` — gives a clean surface for the log ring.
- On exit: `tea.ExitAltScreen` — returns to inline mode.

Debug's own `viewport.Model` stays; only history pipeline loses its viewport.

### `View()` Shape

Final ordering (top → bottom), matching current convention of status above input:

```
[live tail plain-text]          (present only while m.pending.raw has uncommitted tail)
[thinking card]                 (present only while m.pending.thinkRaw non-empty)
[status bar]
<blank line>
[input box | approval | modal | suggestions+input]
```

Each bracketed row is conditional — absent rows produce no blank line. The status bar is always rendered (so it anchors the bottom region). Suggestions, when active, render directly above the input box.

**Transition smoothness:** when a commit fires, bubbletea flushes the `tea.Printf` output above the live frame in the same render pass that redraws the shrunken tail. There is no frame where both the committed rendered block and the pre-commit tail are visible simultaneously — no tearing.

## Files to Change

| File | Change |
|---|---|
| `internal/tui/app.go` | Remove viewport/history fields; add scanner. |
| `internal/tui/update.go` | Rewrite streaming handlers; remove tick system; route all output through `tea.Printf`. |
| `internal/tui/render.go` | Unchanged; still used for tool-call/result formatting. |
| `internal/tui/stream.go` | **New.** `blockScanner`, `SafeSplit`, `Advance`. |
| `internal/tui/debug.go` | Wrap toggle with alt-screen enter/exit cmds. |
| `internal/tui/scrollback.go` | Delete (empty file currently). |
| `internal/tui/messages.go` | Remove `tickMsg` type and its `Update` switch arm. |
| `cmd/sam/main.go` | No changes; `tea.NewProgram` already inline. |

## Migration Steps (Each Commitable)

Dual-write was rejected — it causes visible duplicate output. Steps are ordered so each leaves the TUI in a working state without running both paths.

1. **Add `blockScanner` + normalization helpers + `safeGlamourRender`** in `internal/tui/stream.go`. Unit tests only; no integration yet.
2. **Cut-over commit** — one large step that must be done together:
   - Remove `m.viewport`, `m.history`, `m.lastVP`, `rebuildViewport`.
   - Remove `tickMsg`, `handleTick`, `ensureTick`, `revealRate`, `redrawAssistantBuffer{Plain,Glam,Final}`, `redrawThinkingBuffer`.
   - Route user message, info, error, tool-call, tool-result events through `tea.Printf` directly at call sites.
   - Rewrite `handleAgentEvent` `TextDelta` / `ThinkingDelta` / `TurnDone` per this spec.
   - Rewrite `View()` to the ordering above.
3. **Debug view** alt-screen enter/exit cmds wired to Ctrl-L toggle.
4. **`/clear`** soft-clear via `tea.ClearScreen`, plus pending/scanner reset on mid-turn.
5. **Polish pass:** resize stress test, suggestions dropdown position relative to input, modal placement, approval focus handling.

## Testing

### `blockScanner` unit matrix

Each row drives `SafeSplit` + `Advance` across synthetic delta chunks. Verify `committed` index and post-call scanner state.

| Case | Expected |
|---|---|
| Two paragraphs separated by `\n\n` | Split at the `\n\n` |
| Paragraph then ```` ```lang ```` then code then ```` ``` ```` then `\n\n` then paragraph | Split only after the closing fence's blank line, not at any interior blank |
| Fence-open in delta N, fence-close in delta N+3 | `fenceOpen` persists across calls; no split until close+blank |
| Nested fence: outer ```` ``` ```` with inner ```` ~~~ ```` | Only the matching marker closes; inner ` ~~~ ` ignored |
| CRLF line endings | Normalized to `\n`; splits still work |
| Leading BOM on first delta | Stripped; first-line classification unaffected |
| Setext heading: `para\n===\n\nnext` | Must not split between `para` and `===`; split goes after `===` blank |
| HTML block `<script>\ncode\n</script>\n\nnext` | No split inside block; split at trailing blank |
| Table rows `\|a\|b\|\n\|-\|-\|\n\|1\|2\|\n\n` | Split at trailing blank only; interior lines stay together |
| Loose list with blank lines between items | Known cosmetic: splits on those blanks. Document, don't fix in v1. |
| Indented code block `\n    line1\n\n    line2\n\n` | Known cosmetic: splits at interior blank. Document, don't fix in v1. |
| Empty `tail` | Returns 0 |
| Tail with no blank line at all | Returns 0 |

### Manual integration checklist

- Long assistant response with code blocks + lists — no flicker; finalized blocks appear in scrollback.
- Terminal resize mid-stream — input/status reposition; tail re-flows at new width; prior scrollback keeps old width (accepted).
- Ctrl-C mid-stream — cancels; no orphan tail in View().
- Approval mid-turn — panel replaces input; scrollback untouched; approve/deny resumes stream.
- `/clear` mid-turn — cancels, clears visible, scanner reset, scrollback remains.
- `/clear` at idle — visible cleared, scrollback remains, agent state untouched.
- `/reset` — agent conv cleared, scrollback remains (visual history preserved for user reference).
- Debug toggle (Ctrl-L) mid-stream — alt-screen overlay; toggle back; live frame redraws intact.
- Terminal wheel / Shift+PageUp — prior turns scrollable in terminal's native scrollback.
- `/help` — prints to scrollback; scrollable later.

## Risks

| Risk | Mitigation |
|---|---|
| Malformed markdown from model (unclosed fence) blocks all commits until TurnDone | Acceptable — full flush at end via `safeGlamourRender` (panic-safe). |
| Resize narrowing makes old output overflow | Industry-standard behavior; no mitigation. |
| `tea.Printf` + `waitAgent` ordering | Use `tea.Sequence`, not `tea.Batch`, so Printf commits before the next delta is processed and the View redraws the new tail. |
| Losing the typewriter UX "feel" | Streaming still visible via tail; finalized blocks land in bursts like claude-code/crush. |
| Thinking card vanishing without trace | Per design decision. |
| Indented code block / loose list splits at interior blank | Known cosmetic limitation; documented in v1. Can add deeper state tracking if users report. |
| Debug alt-screen mid-stream | Alt-screen is a separate terminal buffer; inline scrollback underneath is undisturbed. Printf cmds queued during alt-screen land in the inline buffer and reveal cleanly on exit. |

## Open Future Work (Out of Scope)

- Partial-block progressive styling (render single streaming paragraph with glamour in the tail — glamour handles incomplete docs reasonably, worth experimenting later).
- Smart tables: detect mid-table and defer commit until trailing blank line.
- Session transcript export — easier now since all output is already in scrollback format.
