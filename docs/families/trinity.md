# Trinity — Maintainer Reference

**Wire:** openai-compat (via Arcee Conductor)
**Provider(s):** `arcee`
**Representative models:** `trinity-large-thinking`, `trinity-large`
**Prompt family addendum:** `internal/system/defaults/prompts/trinity.md`

## Capabilities

- Context window: 512k tokens
- Max output: 32k
- Reasoning surface: `reasoning_content` (vLLM `deepseek_r1` reasoning-parser output) — flip caps entry to `inline_think` if probe finds raw `<think>` tags in content.
- Parallel tool calls: yes
- Streaming: yes

## Wire quirks

- Endpoint: `https://api.arcee.ai/api/v1`.
- Conductor may route across backends; assume standard OpenAI tool-call semantics.
- Strict null-content rejection: assistant messages with `tool_calls` and `null` content are 400'd. SAM forces `content=""` universally to fence this (`internal/llm/openaicompat/translate.go`).

## Known upstream issues

- `<think>` tag leakage into visible output on the Thinking variant — addendum suppresses; prompt-side guard is the primary fix.

## Source links

- Arcee Conductor docs: https://docs.arcee.ai
