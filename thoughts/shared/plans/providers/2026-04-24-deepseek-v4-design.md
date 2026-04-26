# DeepSeek v4 Provider — Design

> Date: 2026-04-24
> Status: Awaiting review
> Wire: Anthropic-compatible (`https://api.deepseek.com/anthropic`)
> Models: `deepseek-v4-pro` (default), `deepseek-v4-flash`

## Goal

Add first-class DeepSeek provider support to SAM, exposing `deepseek-v4-pro` and `deepseek-v4-flash` through the existing anthropic-compatible wire with per-model defaults tuned for DeepSeek's 1M context window and thinking-by-default reasoning.

## Source of Truth

- Pricing + capabilities: https://api-docs.deepseek.com/quick_start/pricing
- OpenAI endpoint: `https://api.deepseek.com`
- Anthropic endpoint: `https://api.deepseek.com/anthropic`
- Context window: 1,000,000 tokens (both models)
- Max output: 384,000 tokens
- Thinking mode: default on, both models
- Tool calls: supported

## Why Anthropic Wire

SAM already runs a battle-tested `internal/llm/anthropiccompat` stack that natively preserves signed `ContentThinking` blocks across tool-call round-trips and treats structured tool use as first-class. DeepSeek's `/anthropic` endpoint consumes the same Messages API shape, so we avoid:

- Deciding `parse_think_tags` heuristics for inline `<think>` output.
- Translating `reasoning_content` into SAM's block model.
- A second OpenAI-wire variant for the same provider.

Minimax already uses the same pattern (anthropic wire against a non-Anthropic endpoint), proving the approach.

## Change Inventory

### 1. Provider preset

File: `internal/config/presets.go`

Add entry to the map returned by `Presets()`:

```go
"deepseek": {
    Name:         "deepseek",
    Wire:         "anthropic",
    BaseURL:      "https://api.deepseek.com/anthropic",
    APIKeyEnv:    "DEEPSEEK_API_KEY",
    DefaultModel: "deepseek-v4-pro",
    Models:       []string{"deepseek-v4-pro", "deepseek-v4-flash"},
},
```

`Models` populates the TUI model picker (`internal/tui/model_resolve.go:41` uses it for `containsStr` lookup).

### 2. Context window

File: `internal/config/config.go` — `ModelContextWindow`

Replace the existing legacy branch (no existing users):

```go
case strings.HasPrefix(name, "deepseek-r"),
    strings.HasPrefix(name, "deepseek-v3"):
    return 131_072
```

with:

```go
case strings.HasPrefix(name, "deepseek-v4"):
    return 1_000_000
```

### 3. Thinking budget

File: `internal/config/config.go` — `ModelThinkingBudget`

Add case:

```go
case strings.HasPrefix(name, "deepseek-v4"):
    return 120_000
```

### 4. Per-model max_tokens resolver (new)

DeepSeek's 384k output ceiling plus a 120k thinking budget must fit inside `params.MaxTokens`, AND Anthropic's API requires `thinking.budget_tokens < max_tokens`, with the remainder covering every assistant text block, every tool_use block's JSON input, and signatures for the full turn. 8k headroom is not enough for a reasoning-heavy model that emits multi-tool turns — it truncates with `stop_reason: "max_tokens"`.

**Chosen numbers: thinking = 120_000, max_tokens = 192_000** (72k output headroom, still well under DeepSeek's 384k output ceiling).

The global `MaxTokens` default (`32_768`) cannot be bumped because Anthropic Sonnet 4.5 (64k output cap) and other presets would break. The bump must be per-model.

**Add** to `internal/config/config.go`:

```go
type ModelConfig struct {
    ContextWindow        int    `toml:"context_window"`
    ReasoningEffort      string `toml:"reasoning_effort"`
    ThinkingBudgetTokens *int   `toml:"thinking_budget_tokens"`
    MaxTokens            int    `toml:"max_tokens"` // NEW
}

// ModelMaxTokens returns the per-model output budget. Explicit user
// config wins; otherwise family defaults. 0 means "use global default".
func (c *Config) ModelMaxTokens(name string) int {
    if m, ok := c.Models[name]; ok && m.MaxTokens > 0 {
        return m.MaxTokens
    }
    if strings.HasPrefix(name, "deepseek-v4") {
        return 192_000
    }
    return 0
}
```

**TOML round-trip note:** `rawConfig.Models` is already `map[string]ModelConfig` (`config.go:135`), so `[models.<name>] max_tokens = N` parses into the new field with zero additional wiring. No `rawConfig` change required.

### 5. Agent resolver plumbing

Mirrors the existing `SystemResolverFn` pattern so model switches (TUI live switch, CLI override) pick up the right cap.

**`internal/agent/agent.go`**:

```go
type Options struct {
    ...
    MaxTokensResolverFn func(model string) int // NEW
}
```

Store on Agent:

```go
type Agent struct {
    ...
    maxTokensResolver func(string) int
}
```

Set in `New`:

```go
a := &Agent{
    ...
    maxTokensResolver: opts.MaxTokensResolverFn,
}
```

**`internal/agent/loop.go:33`** — resolve per turn:

```go
maxTokens := a.maxTokens
if a.maxTokensResolver != nil {
    if v := a.maxTokensResolver(a.model); v > 0 {
        maxTokens = v
    }
}
req := &llm.Request{
    ...
    MaxTokens: maxTokens,
}
```

**`cmd/sam/main.go`** — wire resolver into both Agent.Options and TUI Options:

```go
MaxTokensResolverFn: func(m string) int { return cfg.ModelMaxTokens(m) },
```

TUI plumbing: add matching option on `tui.Options` (already carries `SystemResolverFn`), forward to agent construction inside TUI model-switch path so mid-session switches honor the resolver.

**Mid-session switch semantics:** `Agent.SetModel` (agent.go:234) is and stays a pure model-name mutation — no new `SetMaxTokens` method. Because `loop.go:33` calls `a.maxTokensResolver(a.model)` at the start of every turn, a `SetModel` call during a session immediately causes the next turn to use the new model's resolved cap. No agent re-construction required.

### 6. Family prompt refresh

File: `internal/system/defaults/prompts/deepseek.md`

Current file describes R1/V3 with 131k context and inline `<think>` — all stale. Rewrite:

```
DEEPSEEK FAMILY.
Anthropic wire. Thinking blocks signed — preserve signature on tool-call round-trip; do not strip ContentThinking.
Context 1M tokens. Max output 384k; default MaxTokens 192k, thinking budget 120k. Long files OK whole.
Thinking mode default, v4 reasons hard — keep hidden reasoning focused.
Tool-call parallelism supported — batch independent reads.
```

Family prefix (`deepseek-` in `internal/config/prompt_families.go:25`) already covers v4 models; no change needed there.

### 7. Tests

**`internal/config/config_test.go`** — add cases:

- `ModelContextWindow("deepseek-v4-pro") == 1_000_000`
- `ModelContextWindow("deepseek-v4-flash") == 1_000_000`
- `ModelThinkingBudget("deepseek-v4-pro") == 120_000`
- `ModelMaxTokens("deepseek-v4-pro") == 192_000`
- `ModelMaxTokens("deepseek-v4-flash") == 192_000`
- User config override wins: inject `Models["deepseek-v4-pro"] = ModelConfig{MaxTokens: 64_000}` → returns 64_000.
- Unknown model returns 0.

**`internal/config/presets_test.go`** (create if missing, else extend equivalent):

- Deepseek preset present in `Presets()`.
- `Wire == "anthropic"`, `BaseURL == "https://api.deepseek.com/anthropic"`, `APIKeyEnv == "DEEPSEEK_API_KEY"`.
- `DefaultModel == "deepseek-v4-pro"`.
- `Models` contains both `deepseek-v4-pro` and `deepseek-v4-flash`.

**`internal/config/family_test.go`**:

- `FamilyForModel("deepseek-v4-pro") == "deepseek"`.
- `FamilyForModel("deepseek-v4-flash") == "deepseek"`.

**`internal/agent/loop_test.go`** (extend existing):

- When `MaxTokensResolverFn` returns a positive value, request carries that value.
- When resolver returns 0, request falls back to `opts.MaxTokens`.
- When resolver is nil, request uses `opts.MaxTokens`.
- After `agent.SetModel("deepseek-v4-pro")`, next turn's request carries 192_000 (via an injected test resolver) — proves the resolver fires per turn, not at construction.

**`internal/llm/anthropiccompat/client_test.go`** (extend — end-to-end wire assertion):

- Build `llm.Request{Model: "deepseek-v4-pro", MaxTokens: 192_000, ThinkingBudgetTokens: 120_000, ...}`, intercept the outbound HTTP request via a test round-tripper, and assert the JSON body contains `"max_tokens": 192000` and the `thinking` block with `"budget_tokens": 120000`. Catches future renames of `Request.MaxTokens` / `Request.ThinkingBudgetTokens` at the wire boundary.

**`internal/tui/model_resolve_test.go`** (add if missing, else extend):

- `resolveModelSpec("deepseek-v4-pro", presets)` → `("deepseek", "deepseek-v4-pro")`.
- `resolveModelSpec("deepseek-v4-flash", presets)` → `("deepseek", "deepseek-v4-flash")`.
- First preset to exercise the `containsStr(entry.Models, spec)` path (`model_resolve.go:41`) — existing presets leave `Models` empty, so this is effectively net-new coverage.

### 8. Documentation

- `README.md`:
  - Add row to providers table: `deepseek | anthropic | deepseek-v4-pro | DEEPSEEK_API_KEY`.
  - Append secrets.toml example line: `DEEPSEEK_API_KEY = "..."`.
  - Note: "DeepSeek v4 defaults — 1M context, 192k max_tokens, 120k thinking budget; override per-model in `[models.<name>]`."
- `thoughts/shared/index/providers.md`: append `deepseek` to the list of bundled presets (the "minimax, anthropic, openai, arcee, moonshot" sentence).
- `thoughts/shared/index/config.md`: mention `ModelMaxTokens` alongside `ModelContextWindow` and `ModelThinkingBudget` in the helpers enumeration.

## Non-Goals

- **No new wire.** Anthropic-compatible endpoint handles Messages API shape as-is.
- **No legacy deepseek-r / deepseek-v3 coverage.** Confirmed zero users.
- **No thinking-mode toggle.** DeepSeek v4 defaults to thinking on; opting out is out-of-scope for this change.
- **No OpenAI-wire deepseek preset.** Single canonical path.

## Risk & Mitigation

| Risk | Mitigation |
|---|---|
| `/anthropic` endpoint deviates from Messages API shape on edge features (prompt caching, citations). | Start with tool use + thinking + streaming only. Add others if/when users request. |
| Per-model MaxTokens resolver introduces new agent option — breaks any existing caller constructing `agent.Options` directly. | Field is optional; nil resolver preserves old behavior (`a.maxTokens` only). |
| Thinking budget 120k + visible output + tool-call JSON exceeds MaxTokens, turn truncates with `stop_reason: "max_tokens"`. | 72k headroom (192k − 120k) covers multi-tool turns. Users who still hit it can raise via `[models."deepseek-v4-pro"] max_tokens = ...`. |
| User sets `thinking_budget_tokens = 0` in TOML expecting thinking disabled. | `anthropiccompat/client.go:142` guards with `if req.ThinkingBudgetTokens > 0` — 0 omits the `thinking` param entirely, so DeepSeek sees a plain Messages request (non-thinking mode). Add a `client_test.go` assertion that `ThinkingBudgetTokens=0` produces no `thinking` field in the outbound body. |

## Verification Plan

1. `go build ./...` — compile clean.
2. `go test ./internal/config/... ./internal/agent/...` — unit suite green.
3. Manual smoke: `SAM_PROVIDER=deepseek SAM_MODEL=deepseek-v4-pro sam` with real `DEEPSEEK_API_KEY` (already in `.env`), issue a short prompt that triggers a tool call (e.g., "read README.md and summarize"), verify:
   - Thinking blocks stream in TUI.
   - Tool-call round-trip preserves signature (no "missing signature" error on continuation).
   - Response completes cleanly (no truncation / `stop_reason: max_tokens`).
4. Model switch via TUI settings modal from claude → deepseek-v4-pro mid-session. No public log line exists for MaxTokens in `anthropiccompat/client.go` today — the wire assertion test from step 7 covers the value at the boundary. For manual verification here, rely on (a) no truncation on long reasoning replies and (b) rapid thinking-block stream indicating thinking mode is active.
5. Smoke the flash variant: `SAM_MODEL=deepseek-v4-flash sam` → same prompt as step 3. Cheaper tier should respond faster but behave identically.

## Open Questions

None — all design decisions locked with user on 2026-04-24.
