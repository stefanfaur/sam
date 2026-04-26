# TUI Visual Tweaks — Design

## Overview

Three small, independent polish changes to SAM's TUI scrollback and color palette.

## Requirements

1. Add vertical padding before assistant text messages so they don't sit flush against the preceding user message or tool card.
2. Give user messages a faint background so they stand out in scrollback.
3. Replace the bright alarm-red error color with a calmer dusty-rose tone.

## Design

### 1. Assistant-message padding

**Approach:** Introduce a `msgStarted bool` on `pendingTurn`. The `MessageStart` event handler sets it. The first `TextDelta` after each message start emits `tea.Printf("\n")` to insert a blank line, then clears the flag.

**Why this approach:** `MessageStart` fires at the start of every assistant message block, so padding appears reliably between user input and assistant prose, and also between tool results and follow-up text in multi-iteration turns. It does not interfere with continuous text streaming because only the first delta triggers the newline.

**Files touched:**
- `internal/tui/app.go` — add `msgStarted bool` to `pendingTurn`
- `internal/tui/update.go` — set flag in `MessageStart` handler, consume in `TextDelta` handler

### 2. User-message background

**Approach:** Add `Background(t.subtleBg())` to the `UserMsg` style in `theme.go`. The `subtleBg()` helper already exists and returns ANSI 235 on dark terminals and 254 on light terminals, giving a faint but visible background behind the text and the existing horizontal padding.

**Files touched:**
- `internal/tui/theme.go` — one line added to `UserMsg` style definition in `Theme.Apply`

### 3. Calmer error color

**Approach:** Change the default for both `ErrorFg` and `StateError` from ANSI 160 (bright red) to ANSI 167 (pale red / dusty rose). Every downstream consumer—`ToolError`, `ToolCardPeekErr`, `ToolCardError` border, `renderError`, and the status-bar error segment—inherits the new default automatically.

**Files touched:**
- `internal/tui/theme.go` — change two default color values in `NewTheme`

## Testing

- `go test ./internal/tui/...` must pass after each change.
- Visual verification: launch SAM, submit a prompt that triggers a tool error (e.g. `read /nonexistent`), confirm the settled error card uses the dusty-rose tone rather than bright red.
- Visual verification: submit any prompt, confirm user message has faint background and assistant text starts with a blank line above it.

## Risks

- The `msgStarted` flag is state that must be reset correctly on turn boundaries. `TurnDone` already clears `pending`, so the flag is naturally reset.
- No breaking changes to config, API, or external behavior.
