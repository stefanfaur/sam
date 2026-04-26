# DeepSeek Reasoner (R1-series) — Maintainer Reference

**Wire:** openai-compat
**Provider(s):** custom user-defined (`https://api.deepseek.com/v1`); self-hosted vLLM common
**Representative models:** `deepseek-reasoner`, `deepseek-r1`, `deepseek-r1-0528`, `deepseek-r1-distill-*`
**Prompt family addendum:** `internal/system/defaults/prompts/deepseek-reasoner.md`

## Capabilities

- Context window: 128k
- Max output: 32k typical
- Reasoning surface: always-on; surfaces via `reasoning_content` (R1-0528+)
- Parallel tool calls: yes (R1-0528+); earlier weights may not support
- Streaming: yes
- Token field: `max_tokens`

## Wire quirks

- `Capabilities.ReasoningSource = "reasoning_content"`; `EchoReasoning = true` so reasoning round-trips on tool-call turns.
- Original R1 weights (pre-0528) used a different reasoning surface and rejected the `system` role — for self-hosted original weights set `[providers.<name>.caps] system_role = "user"` to engage SAM's system-as-user splice (`<system>...</system>` wrapped into first user message).

## Known upstream issues

- See R1-0528 release notes for tool-call regressions on stale builds.

## Source links

- R1 model card: https://huggingface.co/deepseek-ai/DeepSeek-R1
- R1-0528 release notes: https://api-docs.deepseek.com/news
