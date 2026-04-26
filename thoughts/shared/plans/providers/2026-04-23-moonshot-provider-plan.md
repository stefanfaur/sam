# Moonshot AI Provider — Implementation Plan

> Date: 2026-04-23
> Design doc: `thoughts/shared/plans/providers/2026-04-23-moonshot-provider-design.md`
> Domain: providers

## Overview

Add Moonshot AI as a bundled provider exposing `kimi-k2.6` (default) and `kimi-k2.5`. OpenAI-compatible wire at `https://api.moonshot.ai/v1`, native `reasoning_content`, default `reasoning_effort="high"`, multi-turn reasoning echo enabled.

## Success Criteria

- `sam --provider moonshot` boots with `KIMI_API_KEY` set.
- Sending a message to `kimi-k2.6` streams text + thinking deltas and completes a tool loop.
- `reasoning_effort=high` sent by default when user config is silent.
- User `config.toml` `[models."kimi-k2.6"] reasoning_effort = "low"` overrides the default.
- All new and existing tests pass.

## Steps

### Step 1 — Capabilities table entry

**File:** `internal/llm/openaicompat/caps.go`

Insert in `orderedCapsTable` **before** the `gpt-4o, gpt-4.1, …` stanza (so prefix match finds it first — not strictly necessary since prefixes don't overlap, but keeps reasoning-capable families grouped at the top):

```go
{
    prefixes: []string{"kimi-k2"},
    caps: Capabilities{
        SystemRole:              "system",
        MaxTokensField:          "max_tokens",
        SupportsSamplingParams:  true,
        SupportsReasoningEffort: true,
        ReasoningSource:         "reasoning_content",
        EchoReasoning:           true,
    },
},
```

**Verify:** `go build ./...` compiles.

### Step 2 — Caps test

**File:** `internal/llm/openaicompat/caps_test.go`

Extend the prefix-match table in `TestDefaultCapsPrefixMatch` with rows for `kimi-k2.6` and `kimi-k2.5`. Expected: `effort=true`, `src="reasoning_content"`, `echo=true`.

**Verify:** `go test ./internal/llm/openaicompat/...` passes.

### Step 3 — Context window branch

**File:** `internal/config/config.go`

In `ModelContextWindow`, add a case in the switch near the other reasoning-family entries:

```go
case strings.HasPrefix(name, "kimi-k2"):
    return 262_144
```

**Verify:** extend whichever test file covers `ModelContextWindow` (check `internal/config/context_test.go`); add a `kimi-k2.6 → 262144` row. Run `go test ./internal/config/...`.

### Step 4 — Effort resolver helper + main.go wrap

**File:** `internal/config/config.go`

Add a package-level helper:

```go
// DefaultReasoningEffort returns a built-in reasoning_effort default for
// model families that benefit from a preset (e.g., kimi-k2 at "high").
// User-config values in Config.Models always override this.
func DefaultReasoningEffort(model string) string {
    if strings.HasPrefix(model, "kimi-k2") {
        return "high"
    }
    return ""
}
```

**File:** `cmd/sam/main.go:97`

Wrap the existing resolver:

```go
resolver := func(m string) string {
    if e := cfg.Models[m].ReasoningEffort; e != "" {
        return e
    }
    return config.DefaultReasoningEffort(m)
}
```

**Verify:** add unit test in `internal/config/config_test.go` (or `context_test.go`) for `DefaultReasoningEffort`:
- `kimi-k2.6` → `"high"`
- `kimi-k2.5` → `"high"`
- `gpt-4o` → `""`
- `MiniMax-M2.7` → `""`

Run `go test ./internal/config/...`.

### Step 5 — Preset entry

**File:** `internal/config/presets.go`

Add to the returned map:

```go
"moonshot": {
    Name:         "moonshot",
    Wire:         "openai",
    BaseURL:      "https://api.moonshot.ai/v1",
    APIKeyEnv:    "KIMI_API_KEY",
    DefaultModel: "kimi-k2.6",
},
```

**Verify:** If a preset test exists, add a row for `moonshot`. Otherwise add a minimal assertion to `config_test.go` confirming `Presets()["moonshot"].Wire == "openai"` and `DefaultModel == "kimi-k2.6"`.

Run `go test ./internal/config/...`.

### Step 6 — Live smoke test

Manually run:

```bash
KIMI_API_KEY=<from .env> go run ./cmd/sam --provider moonshot
```

Checks:
- TUI boots without error.
- Send "hi" — verify thinking panel shows reasoning and a text reply renders.
- Trigger a tool (`List the files in the current directory`) — verify tool loop closes.
- Toggle `/model kimi-k2.5` — verify switch works (context window and caps re-resolved).

### Step 7 — README + index update

**File:** `README.md`

Locate the providers example and add a `moonshot` block alongside `minimax`/`anthropic`/`openai`/`arcee`. Add `KIMI_API_KEY` to the secrets example.

**File:** `thoughts/shared/index/providers.md`

Append `moonshot` to the list of bundled providers. (This file is the persistent codebase index; update happens at the end of the session anyway — include the edit here so the commit carries fresh docs.)

## Out of Scope

- Moonshot `moonshot-v1-*` legacy models.
- Anthropic-wire endpoint variant.
- `<think>` tag parsing.
- `ModelThinkingBudget` branch (Anthropic-wire only).
- Schema/UI changes — none required.

## Verification Checklist

- [ ] `go build ./...`
- [ ] `go test ./...`
- [ ] Manual smoke test (Step 6) passes
- [ ] README + index updated
- [ ] No unrelated files touched

## Risk / Rollback

Rollback is a single-commit revert — every change is additive (new preset, new caps row, new switch case, new helper). No existing behavior altered.

The `EchoReasoning=true` choice may increase token usage on long tool loops. If users report this, the `config.toml` override path already exists:

```toml
[providers.moonshot.caps]
echo_reasoning = false
```

No code change needed for the escape hatch.
