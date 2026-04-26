# GPT Reasoning (gpt-5 / o-series) — Maintainer Reference

**Wire:** openai-compat
**Provider(s):** `openai`
**Representative models:** `gpt-5`, `gpt-5-mini`, `o1-preview`, `o1-mini`, `o3-mini`, `o4-mini`
**Prompt family addendum:** `internal/system/defaults/prompts/gpt-reasoning.md`

## Capabilities

- Context window: 400k (gpt-5 / o-series)
- Max output: 100k (gpt-5)
- Reasoning surface: internal — never echoed; `reasoning_effort` controls budget
- Parallel tool calls: yes
- Streaming: yes
- Token field: `max_completion_tokens`
- System role: `developer` (not `system`)

## Wire quirks

- **Formatting prepend required:** literal `Formatting re-enabled.\n` must be the first line of the system/developer message — otherwise the model emits plain text with no Markdown structure. Wired via `Capabilities.PrependFormatting = true` (`internal/llm/openaicompat/caps.go`).
- `max_tokens` is rejected — must use `max_completion_tokens`.
- Sampling params (`temperature`, `top_p`) are not honored — leave nil.

## Known upstream issues

- Codex tool-use sequencing — see https://github.com/openai/openai-cookbook (search "reasoning best practices").

## Source links

- OpenAI reasoning best practices: https://platform.openai.com/docs/guides/reasoning
