# Minimax — Maintainer Reference

**Wire:** anthropic-compat (Messages API at `/anthropic` path)
**Provider(s):** `minimax`
**Representative models:** `MiniMax-M2.7`
**Prompt family addendum:** `internal/system/defaults/prompts/minimax.md`

## Capabilities

- Context window: 1M tokens
- Max output: 192k
- Default thinking budget: 32k
- Reasoning surface: signed thinking blocks (Anthropic-compat)
- Parallel tool calls: yes
- Streaming: yes

## Wire quirks

- Endpoint is `https://api.minimax.io/anthropic` — same Messages-API shape as Claude.

## Known upstream issues

- Streaming may discard thinking blocks across turns on some configurations — verify round-trip behavior when tuning.

## Source links

- Minimax M2 docs: https://www.minimaxi.com/en/news
