# Claude — Maintainer Reference

**Wire:** anthropic-compat (Messages API)
**Provider(s):** `anthropic`
**Representative models:** `claude-opus-4-7`, `claude-sonnet-4-5`, `claude-haiku-4-5`
**Prompt family addendum:** `internal/system/defaults/prompts/claude.md`

## Capabilities

- Context window: 200k tokens
- Max output: 64k (Sonnet) / 32k (Opus, Haiku)
- Default thinking budget: 16k
- Reasoning surface: signed thinking blocks (`ContentThinking` with signature)
- Parallel tool calls: yes
- Streaming: yes

## Wire quirks

- Extended thinking blocks are signed — signature must round-trip on tool-call follow-ups; do not strip `ContentThinking`.
- `tool_use` and `tool_result` blocks are first-class on Messages API (not flattened into a chat-string sequence like OpenAI).

## Known upstream issues

- None tracked at present.

## Source links

- Anthropic prompting guide: https://docs.anthropic.com/en/docs/build-with-claude/prompt-engineering
- Claude 4 best practices: https://docs.anthropic.com/en/docs/build-with-claude/claude-4-best-practices
