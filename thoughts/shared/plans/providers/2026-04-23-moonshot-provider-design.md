# Moonshot AI Provider — Design

> Date: 2026-04-23
> Status: Design approved, ready for implementation plan

## Summary

Add Moonshot AI as a bundled provider preset, exposing `kimi-k2.6` (default) and `kimi-k2.5`. Wire is OpenAI-compatible against `https://api.moonshot.ai/v1`. Reasoning (`reasoning_content`) surfaces natively; default `reasoning_effort` is `high`; prior reasoning is echoed on multi-turn.

## API Probe (evidence)

- `GET /v1/models` with `KIMI_API_KEY` returns 9 entries. Kimi k2.x entries advertise `supports_reasoning=true`, `supports_image_in=true`, `supports_video_in=true`, `context_length=262144`.
- `POST /v1/chat/completions` with `model=kimi-k2.6` returns `choices[0].message.reasoning_content` as a first-class string field alongside `content`.
- `reasoning_effort` query parameter is accepted without error.
- `usage.prompt_tokens_details.cached_tokens` is returned — maps cleanly onto existing `streamUsage.PromptTokensDetails.CachedTokens`.

## Scope

**In:**

- `kimi-k2.6` (default), `kimi-k2.5` — flagship reasoning models, 262k context.

**Out (YAGNI):**

- `moonshot-v1-*` legacy models (8k/32k/128k plain + vision-preview). Users who need them can add entries to their own `config.toml`.
- Moonshot's Anthropic-compatible endpoint. OpenAI wire already covers the need.
- `<think>` tag parsing. Moonshot uses the structured `reasoning_content` field.

## Architecture

### 1. Preset entry — `internal/config/presets.go`

Add one map entry:

```go
"moonshot": {
    Name:         "moonshot",
    Wire:         "openai",
    BaseURL:      "https://api.moonshot.ai/v1",
    APIKeyEnv:    "KIMI_API_KEY",
    DefaultModel: "kimi-k2.6",
},
```

No `ParseThinkTags` (default false). No inline `Caps` override — the caps table (§2) carries all kimi-specific behavior.

### 2. Capabilities — `internal/llm/openaicompat/caps.go`

Insert a new entry into `orderedCapsTable` for the `kimi-k2` prefix. The shape is close to deepseek-r (both emit `reasoning_content` and benefit from multi-turn echo), with one addition: kimi also accepts `reasoning_effort`, so the kimi entry sets `SupportsReasoningEffort: true` while deepseek-r does not.

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

Ordering: insert before the generic `gpt-*` stanza. No prefix collision with existing entries (`gpt-*`, `o*`, `deepseek-*`, `trinity-*`).

Notes on each flag:

- `SupportsReasoningEffort=true` lets `buildRequest` set `reasoning_effort` when the resolver returns a non-empty string. With default resolver wrapping (§4), kimi-k2 models default to `"high"` unless the user overrides.
- `EchoReasoning=true` causes `translateAssistantMessage` (translate.go:78) to write prior `ContentThinking` blocks into the outbound `reasoning` field on assistant turns, preserving chain-of-thought across tool loops (same behavior deepseek-r uses).
- `ReasoningSource="reasoning_content"` is purely informational — verified by grep, the field is read only in tests (caps_test.go). Stream extraction in `translate.go:125-130` emits both `ReasoningContent` and `Reasoning` deltas unconditionally regardless of this field. Kept for accuracy and future /model routing.

### 3. Context window — `internal/config/config.go`

Extend `ModelContextWindow` with a kimi branch:

```go
case strings.HasPrefix(name, "kimi-k2"):
    return 262_144,
```

Placement: alongside the other reasoning-model branches. No `ModelThinkingBudget` entry needed (that field is Anthropic-wire only).

### 4. Default reasoning effort — resolver wrap

Ship `kimi-k2.6` and `kimi-k2.5` with `reasoning_effort = "high"` unless the user's `config.toml` says otherwise. Considered two places:

- Option A: Seed entries into the `Models` map during `Load`. Requires new seeding logic and mutates user-visible state.
- **Option B (chosen):** Wrap the effort resolver in `cmd/sam/main.go:97`.

Current resolver (main.go:97):

```go
resolver := func(m string) string { return cfg.Models[m].ReasoningEffort }
```

Note `cfg.Models[m]` yields a zero-value `ModelConfig` when the key is absent, so `ReasoningEffort` is the empty string `""` in both the "absent" and "present but blank" cases — they're indistinguishable and treated the same.

Proposed wrap:

```go
resolver := func(m string) string {
    if e := cfg.Models[m].ReasoningEffort; e != "" {
        return e
    }
    if strings.HasPrefix(m, "kimi-k2") {
        return "high"
    }
    return ""
}
```

Extract to a helper (e.g. `config.DefaultReasoningEffort(model string) string` or a small `main.go`-local function) so it can be unit-tested without constructing a full `Config`. User config always wins; the family default applies only when the user is silent. No mutation of `cfg.Models`, no change to `presets.go`.

### 5. Data flow (sanity check)

`TUI/CLI` → `registry.Build(entry=moonshot, model=kimi-k2.6, effortResolver, _)` → `openaicompat.NewProvider` → `MergeCaps(DefaultCaps("kimi-k2.6"), entry.Caps)` picks up the new table entry → `Stream` sends `reasoning_effort=high`, receives SSE with `reasoning_content` deltas → `translate` emits `EventThinkingDelta` + `EventTextDelta` — existing rendering path. No agent-loop changes required.

## Error Handling

No new failure modes. Missing `KIMI_API_KEY` is surfaced by the existing `openaicompat.Client.Stream` path (same as `minimax`, `arcee`). Invalid model names fall through to generic OpenAI-style 400s from Moonshot — displayed by existing error plumbing.

## Testing

1. **Caps prefix match** — extend `internal/llm/openaicompat/caps_test.go` `TestDefaultCapsPrefixMatch` with rows for `kimi-k2.6` and `kimi-k2.5` asserting `SupportsReasoningEffort=true`, `ReasoningSource="reasoning_content"`, `EchoReasoning=true`.
2. **Context window** — add `TestModelContextWindow` case for `kimi-k2.6` → 262_144 (or extend existing table test; check `internal/config/context_test.go`).
3. **Preset shape** — quick table test asserting `Presets()["moonshot"]` has `Wire="openai"`, `BaseURL="https://api.moonshot.ai/v1"`, `APIKeyEnv="KIMI_API_KEY"`, `DefaultModel="kimi-k2.6"`.
4. **Effort fallback helper** — unit-test the family-default resolver helper (§4): `kimi-k2.6` with empty user config → `"high"`; `kimi-k2.6` with user `reasoning_effort="low"` → `"low"` (user wins); `gpt-4o` with empty config → `""` (no family default, unchanged).
5. **Live smoke (manual)** — run `sam --provider moonshot` with `KIMI_API_KEY` set, send a prompt, verify thinking panel renders reasoning, verify tool loop works.

## Documentation

- `README.md`: add `moonshot` to the providers table / example config block. Show `KIMI_API_KEY` in the secrets example.
- `thoughts/shared/index/providers.md`: append `moonshot` to the provider list next session's index update.

## Open Risks

- Moonshot may tighten parameter validation over time; `reasoning_effort` is currently accepted silently. If they later reject it on non-reasoning models, the caps table's prefix scoping (only `kimi-k2`) already contains the blast radius.
- `EchoReasoning=true` means prior thinking is sent back to the model on subsequent turns. If Moonshot charges for echoed reasoning tokens (unknown — docs ambiguous), token usage may be higher than expected. Mitigation: users can override via `providers.moonshot.caps.echo_reasoning = false` in their `config.toml`.
