# Domain: Providers
> Last updated: 2026-04-26

## Key Files
- `internal/llm/provider.go` — Provider interface definition
- `internal/llm/types.go` — Message and response type definitions
- `internal/llm/registry/` — Provider registry and factory logic
- `internal/llm/anthropiccompat/` — Anthropic-compatible wire implementation
- `internal/llm/openaicompat/` — OpenAI-compatible wire implementation
- `internal/llm/openaicompat/caps.go` — `Capabilities` (incl. `PrependFormatting` for o-series), `DefaultCaps`, `MergeCaps` against `config.CapsOverride`
- `internal/llm/openaicompat/translate.go` — `toWireMessages` (system-as-user splice when `SystemRole=="user"`, `Formatting re-enabled.\n` prepend when `PrependFormatting`); `translateAssistantMessage` forces `content=""` on tool-call-only turns

## How It Works
SAM abstracts LLM providers behind a common interface with two wire implementations. The config declares provider presets (minimax, anthropic, openai, arcee, moonshot, deepseek) keyed by name, each with wire type and API key env var. The registry dispatches by wire and instantiates the appropriate provider at runtime. `deepseek` rides the anthropic wire via DeepSeek's `/anthropic` endpoint and uses per-model `ModelMaxTokens` (192k default, 1M context, 120k thinking budget) resolved per turn by the agent.

OpenAI-compat capabilities branch on model prefix: gpt-5/o-series get `PrependFormatting=true` (literal `Formatting re-enabled.\n` as first line of system message — required to emit Markdown) and `developer` system role with `max_completion_tokens`. Trinity / strict OpenAI-compat servers reject assistant messages with `tool_calls` and null content; SAM forces `content=""` universally. Self-hosted original DeepSeek R1 weights configure `[providers.<name>.caps] system_role = "user"` to engage the system-as-user splice (`<system>...</system>` wrapped into first user message; no top-level system emitted).

## Where to Look
Start at `internal/llm/provider.go` for the interface contract, then explore the wire packages (anthropiccompat, openaicompat) for streaming, message translation, and capability handling. Config examples are in README.md.
