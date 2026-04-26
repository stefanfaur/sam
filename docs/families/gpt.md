# GPT (non-reasoning) — Maintainer Reference

**Wire:** openai-compat
**Provider(s):** `openai`
**Representative models:** `gpt-4o`, `gpt-4o-mini`, `gpt-4.1`, `gpt-4.1-mini`, `gpt-4-turbo`
**Prompt family addendum:** `internal/system/defaults/prompts/gpt.md`

## Capabilities

- Context window: gpt-4o = 128k; gpt-4.1 = 1M; gpt-4 / gpt-4-turbo = 8k–128k by variant
- Max output: 16k typical
- Reasoning surface: none (no chain-of-thought field)
- Parallel tool calls: yes
- Streaming: yes
- Token field: `max_tokens`

## Wire quirks

- Standard OpenAI chat-completions shape. `system` role accepted directly.

## Known upstream issues

- Sandwich-prompt sensitivity: long context with a short trailing instruction — addendum re-anchors trailing-instruction primacy.

## Source links

- GPT-4.1 prompting guide: https://platform.openai.com/docs/guides/text-generation
