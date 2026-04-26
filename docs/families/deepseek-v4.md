# DeepSeek V4 — Maintainer Reference

**Wire:** anthropic-compat (Messages API at `/anthropic` path)
**Provider(s):** `deepseek`
**Representative models:** `deepseek-v4-pro`, `deepseek-v4-flash`
**Prompt family addendum:** `internal/system/defaults/prompts/deepseek-v4.md`

## Capabilities

- Context window: 1M tokens
- Max output: 384k (default `MaxTokens` 192k)
- Default thinking budget: 120k
- Reasoning surface: signed `ContentThinking` blocks (Anthropic-compat)
- Parallel tool calls: yes (Messages API)
- Streaming: yes

## Wire quirks

- Endpoint: `https://api.deepseek.com/anthropic` — Messages API shape; thinking blocks are signed and must round-trip on tool-call turns. Do not strip `ContentThinking` between turns.
- Per-model `MaxTokens` resolver lives in `internal/llm/anthropiccompat/` — see provider preset.

## Known upstream issues

- None tracked.

## Source links

- DeepSeek V4 design: `thoughts/shared/plans/providers/2026-04-24-deepseek-v4-design.md`
- DeepSeek API docs: https://api-docs.deepseek.com
