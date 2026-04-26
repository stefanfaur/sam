# Status Bar Redesign & Settings Modal — Design Document

**Date:** 2026-04-22
**Domain:** tui
**Status:** Draft — awaiting review

## Goal

Replace the thin single-line status bar with a rich, segment-based two-line status area rendered *below* the input box. Introduce a tabbed Settings modal (`/settings`) that centralizes configuration of the status bar, providers/auth, and theme, replacing the standalone `/provider` and `/auth` slash commands. Persist user preferences to `~/.config/sam/settings.toml`.

## Motivation

- Current status bar shows only `provider · model · state · iter/maxIter · tokens` and is placed above the input, where it jitters on resize.
- Token counters are wired but never populated — provider clients discard usage data.
- `/provider` and `/auth` slash commands create a fragmented configuration UX.
- No runtime theming: glamour style and TUI colors are compiled in.
- Inspiration: Claude Code's `cc-statusline` extension — segmented, colored, glyph-prefixed.

## Non-Goals

- Token cost estimation in dollars (can be added later).
- Multi-terminal width adaptation beyond segment-drop priority.
- Accessibility (high-contrast mode, screen-reader hints) — out of scope for this pass.

## Architecture

### Visual layout (inline mode, top → bottom)

```
[ scrollback: committed blocks from prior turns ]
[ live tail: uncommitted assistant text, plain ]
[ thinking card: when streaming reasoning ]
[ approval panel / modal / suggestion dropdown ]
┌──────────────────────────────────────┐
│ ❯ your prompt here                   │   ← input
└──────────────────────────────────────┘
 ⠋ responding · 1.2s    ↻ 3/25         ← status row 1 (live, animated)
 anthropic/sonnet-4-6 · ~/sam   main● · ctx 42% · 1.2k↓ 340↑ · ^C cancel ^L logs   ← row 2
```

Placing the status bar *below* the input stabilizes the input position during state churn — the textarea no longer reflows on every delta.

### Module split

| File | Role |
|---|---|
| `internal/tui/statusbar.go` (rewrite) | Segment-based renderer. Takes `StatusModel` + `Settings`, returns 0–2 styled lines |
| `internal/tui/segments.go` (new) | Pure functions per segment: `segState`, `segElapsed`, `segModel`, `segCwd`, `segGit`, `segCtx`, `segTokens`, `segKeybinds`, `segSpinner`, `segIter`. Each returns a styled string + priority int (for narrow-width drop order) |
| `internal/tui/settings.go` (new) | `Settings` struct, load/save, defaults, segment toggles |
| `internal/tui/settings_modal.go` (new) | Tabbed modal composing 3 sub-forms (Statusline, Providers, Theme); custom tab switching on top of huh |
| `internal/tui/spinner.go` (new) | Braille frame state, advanced by a `spinnerTickMsg` (reused tick only while pending != nil) |
| `internal/tui/gitinfo.go` (new) | One-shot `git branch --show-current` + `git status --porcelain` probe at startup; cached |
| `internal/tui/theme.go` (new) | Dynamic style vars (mutable), replacing current `var` block in `styles.go`; setter `ApplyTheme(Settings)` updates glamour renderer + lipgloss palette |
| `internal/config/config.go` | Add `[models.<name>] context_window = N` table; helper `ModelContextWindow(name string) int` |
| `internal/agent/agent.go` + `internal/agent/loop.go` | Forward new `UsageEvent` from provider to event channel |
| `internal/llm/*` | Each provider client parses API usage response and returns it alongside text content |
| `cmd/sam/main.go` | No change |

## Settings schema

`~/.config/sam/settings.toml`:

```toml
[statusbar]
enabled  = true
position = "below"        # reserved for future "above"
layout   = "two-line"     # "one-line" | "two-line"
spinner  = true
colors   = true
elapsed  = true

[statusbar.segments]
state      = true
model      = true
provider   = true
cwd        = true
git        = true
iterations = true
context    = true
tokens     = true
keybinds   = true

[theme]
glamour_style = "dark"     # dark | light | dracula | notty | auto
accent        = "#6366f1"  # purple; borders, prompt, active segment text
muted         = "#737373"  # inactive segments, hints
user_border   = "#8b5cf6"  # "> your prompt" bar
assistant_fg  = ""         # empty = inherit terminal
error_fg      = "#ef4444"
state_thinking = "#60a5fa"
state_responding = "#34d399"
state_tool     = "#fbbf24"
state_error    = "#f87171"
state_approval = "#a78bfa"
```

Defaults match §2 "recommended" preset (two-line, spinner on, all segments on).

## Segment catalogue

| Segment | Display | Color | Priority (1=keep longest) | Source |
|---|---|---|---|---|
| `spinner` | `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` frame | state-colored | merged into state | spinner tick 80ms |
| `state` | `✦ thinking` / `⟳ responding` / `⚙ tool:Bash` / `✗ error` / `⏸ approval` / hidden when idle | state-based | 1 | `Model.status.state` |
| `elapsed` | `1.2s` during turn; hides at idle | muted | 4 | `turnStart` time |
| `iterations` | `↻ 3/25` | muted | 3 | `Model.status.{iter,maxIter}` |
| `model` | `sonnet-4-6` | accent | 2 | `Model.status.model` |
| `provider` | `anthropic` (prefix to model with `/`) | muted | 2 | `Model.status.provider` |
| `cwd` | `~/sam` (home-collapsed; truncated mid-path if >30 chars) | muted | 6 | `agent.LaunchDir()` |
| `git` | ` main` (clean) / ` main●` (dirty); hides outside git repo | accent | 7 | one-shot probe |
| `context` | `ctx 42%` (short form) or `ctx 42% (84k/200k)` (wide) | muted, gradient to error at >90% | 5 | `session_input / ModelContextWindow` |
| `tokens` | `1.2k↓ 340↑` (turn) / `12k↓ 3.4k↑` (session) based on variant | muted | 5 | `UsageEvent` accumulator |
| `keybinds` | `^C cancel ^L logs ^D quit /help` | muted | 8 (first to drop) | static |

### Narrow-width fallback
Segments drop in reverse priority order when rendered line exceeds terminal width. Drop-then-re-measure loop, up to 3 iterations.

## Tokens plumbing

1. New event:
   ```go
   type UsageEvent struct {
       InputTokens         int
       OutputTokens        int
       CacheReadInput      int  // Anthropic only; 0 for providers without cache
       CacheCreationInput  int  // Anthropic only
   }
   ```
2. Provider clients parse usage from response JSON:
   - **Anthropic:** `usage.{input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens}`
   - **Minimax:** `usage.{prompt_tokens, completion_tokens}` → map to Input/Output; cache fields stay 0
3. Client emits `UsageEvent` on the agent's event channel after each complete API response (once per iteration). Skip emission if parse fails.
4. `handleAgentEvent` case `agent.UsageEvent`:
   ```go
   m.status.lastIterIn = ev.InputTokens           // for ctx% calc
   m.status.turnIn    += ev.InputTokens
   m.status.turnOut   += ev.OutputTokens
   m.status.turnCacheRead += ev.CacheReadInput
   m.status.sessionIn  += ev.InputTokens
   m.status.sessionOut += ev.OutputTokens
   ```
5. On `startTurn`: reset `turnIn/turnOut/turnCacheRead` to 0; keep session totals and `lastIterIn`
6. On `/reset`: reset all including session + `lastIterIn`

v1 `tokens` segment displays `input↓ output↑` (turn) and `session total` on hover or in wide-mode; cache tokens logged to debug ring but not displayed (deferred to v1.1 when we build a tokens detail view).

## Context % calc

```go
// Use LAST iteration's input_tokens — each LLM call re-sends the full
// conversation, so the most recent input_tokens count is the current
// context footprint. Summing across iterations double-counts history.
ctxUsed := m.status.lastIterIn
ctxMax  := config.ModelContextWindow(m.status.model)  // fallback 128_000
ctxPct  := int(100 * ctxUsed / ctxMax)
```

Displayed via `segCtx` with color gradient: `<70%` muted, `70-90%` yellow, `>90%` error red.

Until the first `UsageEvent` of a session `lastIterIn == 0`, segment shows `ctx —` rather than `ctx 0%`.

## Settings modal — tabbed UX

Modal frame:
```
┌─ Settings ─────────────────────────────────────────────────┐
│  [1·Statusline]  [2·Providers]  [3·Theme]                  │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  <active tab form via huh>                                  │
│                                                            │
│  ──────────────────────────────────────────────            │
│  [live preview panel for Statusline / Theme tabs]          │
│                                                            │
├────────────────────────────────────────────────────────────┤
│  [↹ switch tab  ⏎ next field  Ctrl+S save  Esc cancel]     │
└────────────────────────────────────────────────────────────┘
```

### Tab switching
- Keys: `Ctrl+Tab` forward, `Ctrl+Shift+Tab` back. **No numeric shortcuts** — too fragile with huh text inputs where `1`/`2`/`3` are valid characters
- Key interception: settings_modal's `Update(msg)` checks tab-switch keys *before* forwarding to active form
- Each tab holds its own `huh.Form`; switching just flips which form receives messages

### Tab 1 — Statusline
huh groups:
1. Note: "Status bar appearance"
2. Confirm fields for enabled, spinner, colors, elapsed
3. Select for layout (one-line / two-line)
4. Multi-select for segments (9 options; Space toggles)
5. Note: live-preview label → lipgloss-rendered preview rebuilt from pending form state on each update

### Tab 2 — Providers
1. Select: provider (minimax / anthropic)
2. Password inputs: `minimax_key`, `anthropic_key` (pre-filled with masked existing values; empty = unchanged)
3. On Save: writes secrets.toml, updates `config.toml` `provider` field, rebuilds LLM via factory, updates `status.provider`

### Tab 3 — Theme
1. Select: glamour style (preset list + "custom")
2. Text inputs for hex colors (accent, muted, user_border, error_fg, state_*) — validated via regex `^#[0-9a-fA-F]{6}$`
3. Live preview: sample rendered markdown + the status bar, using pending theme

### Save behaviour
- Global save button (or `Ctrl+S`) commits all three tabs at once
- Writes: `settings.toml` (statusline + theme), `secrets.toml` (keys), `config.toml` (provider)
- Calls `theme.ApplyTheme(settings)` + `agent.SetProvider(newProvider)` + rebuilds glamour renderer
- Closes modal

### Cancel behaviour
- `Esc` discards all changes, closes modal; no file writes

## Spinner & elapsed

`spinnerState{ frame int, lastTick time.Time }` on `Model`.

When `pending` transitions from nil → set:
- Record `turnStart = time.Now()`
- Schedule `spinnerTickMsg` via `tea.Tick(80ms)`

On `spinnerTickMsg`:
- Advance frame (`(frame+1) % len(frames)`)
- Return next `tea.Tick` — tea redraws on every msg, no explicit no-op needed
- If `pending == nil`: return nil (don't schedule next tick)

On turn end (TurnDone/cancel/error):
- Clear spinner by setting `m.status.state = "idle"`
- Next tick sees `pending == nil` and stops

**Interaction with scrollback refactor:** ticks force a `View()` rebuild ~12×/sec during streaming. `View()` is idempotent w.r.t. uncommitted buffer — it reads `m.pending.raw[m.pending.committed:]` as plain text each call, no mutation. `tea.Printf` commits happen only on `TextDelta`/`ToolCall`/`TurnDone`, unaffected by spinner ticks. Tick is gated strictly to `pending != nil` so no idle-session churn.

## Git probe

`func probeGit(dir string) *gitInfo` — runs at `tui.New`:
1. `git -C dir rev-parse --abbrev-ref HEAD` → branch (empty if not a repo)
2. `git -C dir status --porcelain` → dirty if output non-empty
3. Store on `Model.git`; segment hides if `branch == ""`

No refresh during session — cheap, predictable, good enough.

## Theme engine

Current `styles.go` exports package-level `var` lipgloss styles. Move to a `Theme` struct, mutable:

```go
type Theme struct {
    Accent, Muted, UserBorder, ErrorFg lipgloss.Color
    StateThinking, StateResponding, StateTool, StateError, StateApproval lipgloss.Color
    GlamourStyle string

    // derived styles rebuilt on Apply()
    StatusBar, InputBox, InputBoxFocus, UserMsg, ToolHeader, /* ... */ lipgloss.Style
}

func (t *Theme) Apply() { /* recompute derived styles + rebuild glamour renderer */ }
```

**Committed vs pending theme:**
- `Model.theme *Theme` — the applied theme used by all render sites
- The settings modal holds its own `pendingTheme *Theme` built from form state on every keystroke; live preview in the Theme tab renders against `pendingTheme`
- `Model.theme` is only replaced on Save. Render sites always read `m.theme`, never modal state, so live preview never bleeds into the main view.

Render sites (`render.go`, `statusbar.go`, `segments.go`, `update.go` input box, `forms.go` remaining modal chrome) take either `*Model` or `*Theme` explicitly. Global `var` styles removed from `styles.go` (file shrinks to constants only — glyphs, frames).

Glamour renderer rebuilt via `glamour.NewTermRenderer(glamour.WithStylePath(theme.GlamourStyle))` on `Apply()`.

## Data flow

```
User → /settings → modal opens → user edits across tabs → Save
   ↓
Settings struct (pending) → validate → write settings.toml
                                    → write secrets.toml
                                    → config.SaveProvider(name)
                                    → theme.Apply()
                                    → agent.SetProvider(factory(new))
                                    → modal.Close
   ↓
View re-renders with new Settings + Theme
```

## Testing

- `segments_test.go`: each `seg*` function pure — test output for each state/mode, narrow-width edge cases
- `settings_test.go`: load default, load missing file, save-then-load round-trip, sparse TOML fills defaults, malformed hex colors fall back with warning
- `statusbar_test.go`: segment composition, narrow-width drop order, row count per layout
- `theme_test.go`: hex color validation, Apply() round-trip, glamour rebuild on style change, committed-vs-pending isolation
- `gitinfo_test.go`: mock via temp repo; branch, dirty, not-a-repo
- `spinner_test.go`: frame advance, stop on pending=nil, tick-gating invariants
- `settings_modal_test.go` (integration): save path writes all three files (settings, secrets, config) atomically; partial failure (e.g., unwritable secrets.toml) rolls back and surfaces error via `addInfo` without mutating applied state

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| huh doesn't support tabs natively | Custom composite modal with key interception; documented in `settings_modal.go` |
| Spinner tick during streaming competes with `tea.Printf` for redraw | Spinner touches only live frame (not Printf); order-insensitive |
| Config file migration when users upgrade | On load: if file missing → defaults; if present → fill missing fields with defaults; TOML allows sparse |
| Theme hex-color parse errors | Validate at form level; fall back to defaults if file-loaded color malformed, log warning |
| Token usage parse mismatch between providers | Per-provider parser in each client; `UsageEvent` only emitted when parse succeeds |
| Live preview re-renders on every keystroke | Each render ~1ms (lipgloss cached styles); acceptable |

## Migration & backwards compatibility

- Users on old binary have no `settings.toml` → first launch creates defaults in-memory; file written on first `/settings` save
- `/provider` and `/auth` slash commands removed — `/help` text updated to point to `/settings`
- Precedence for glamour style: `settings.toml [theme].glamour_style` > `config.toml [tui].theme` > compile-time default (`"dark"`)
- `config.toml [tui].theme` becomes legacy/deprecated — still honored as fallback, documented in help text. On first `/settings` save with `settings.toml`, `config.toml [tui].theme` is left untouched (not auto-migrated) to avoid surprising edits

### Mid-session git refresh (deferred)

Git probe is startup-only in v1. Mid-session commits leave the dirty indicator stale. v1.1 will add a debounced refresh on user idle (no deltas for 5s) or on TurnDone. Tracked as TODO in `gitinfo.go`.

## Slash commands after this change

Kept: `/model`, `/clear`, `/reset`, `/cwd`, `/help`, `/quit`, `/exit`
Removed: `/provider`, `/auth`
Added: `/settings`

## File change summary

| File | Type |
|---|---|
| `internal/tui/statusbar.go` | Rewrite |
| `internal/tui/segments.go` | Create |
| `internal/tui/settings.go` | Create |
| `internal/tui/settings_modal.go` | Create |
| `internal/tui/spinner.go` | Create |
| `internal/tui/gitinfo.go` | Create |
| `internal/tui/theme.go` | Create |
| `internal/tui/styles.go` | Reduce — move mutable styles into theme.go |
| `internal/tui/update.go` | Reorder View(), wire spinner tick, handle UsageEvent, add `/settings` |
| `internal/tui/app.go` | Add `Settings`, `Theme`, `git`, `turnStart`, token fields |
| `internal/tui/forms.go` | Remove auth/provider forms (superseded) |
| `internal/tui/commands.go` | Add `/settings`, remove `/provider`, `/auth` |
| `internal/tui/render.go` | Parameterize styles (take theme) |
| `internal/config/config.go` | Add `[models]` table, `ModelContextWindow` helper |
| `internal/agent/agent.go` | New `UsageEvent` type |
| `internal/agent/loop.go` | Forward usage from provider |
| `internal/llm/anthropiccompat/client.go` | Parse + return usage |
| `internal/llm/minimax*` | Parse + return usage |

~15 files, ~1350 LOC net.

**Risk note on LOC:** the theme refactor alone touches every render site (statusbar, input, user msg, tool rows, thinking card, error blocks). LOC count understates the blast radius. Suggest landing it as its own step ahead of the modal UI so breakage surfaces early.

## Expected outcomes

- Rich, informative status bar below the input
- Single configuration entry point (`/settings`)
- Live theme customization without restart
- Real token/context feedback during long sessions
- Backwards-compatible defaults — new users get recommended layout automatically
