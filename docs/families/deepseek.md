# DeepSeek (catch-all / chat / V3) — Maintainer Reference

**Wire:** openai-compat
**Provider(s):** custom user-defined (`https://api.deepseek.com/v1`)
**Representative models:** `deepseek-chat`, `deepseek-coder`, `deepseek-v3`, future `deepseek-*` variants not covered by the `deepseek-reasoner` or `deepseek-v4` siblings
**Prompt family addendum:** `internal/system/defaults/prompts/deepseek.md`

## Capabilities

- Context window: 64k (V3) / varies by variant
- Max output: 8k typical
- Reasoning surface: none on chat models
- Parallel tool calls: yes
- Streaming: yes

## Wire quirks

- Standard OpenAI shape via `https://api.deepseek.com/v1`.
- The bundled `deepseek` provider preset uses the **anthropic-compat** endpoint for V4 — for V3 / chat you must define a custom provider entry pointing at `/v1`.

## Known upstream issues

- None tracked.

## Source links

- DeepSeek API docs: https://api-docs.deepseek.com
