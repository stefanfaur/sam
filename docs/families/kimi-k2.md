# Kimi-K2 — Maintainer Reference

**Wire:** openai-compat
**Provider(s):** `moonshot`
**Representative models:** `kimi-k2.6`, `kimi-k2.5`
**Prompt family addendum:** `internal/system/defaults/prompts/kimi-k2.md`

## Capabilities

- Context window: 262k tokens
- Max output: 32k
- Reasoning surface: `reasoning_content` field on chat completion
- Parallel tool calls: yes
- Streaming: yes
- Reasoning effort: supported (`reasoning_effort=high` recommended for agent work)

## Wire quirks

- No inline `<think>` tags — reasoning streams via `reasoning_content`. Matching `Capabilities.ReasoningSource = "reasoning_content"`.
- Endpoint: `https://api.moonshot.ai/v1`.

## Known upstream issues

- None tracked.

## Source links

- Moonshot agent prompting: https://platform.moonshot.ai/docs
