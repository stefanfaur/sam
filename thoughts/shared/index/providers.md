# Domain: Providers
> Last updated: 2026-04-27

## Key Files
- `internal/llm/provider.go` — Provider interface definition
- `internal/llm/types.go` — Message and response type definitions, including `ContentImage` block + `ImageAttachment` shape for multimodal user turns
- `internal/llm/anthropic_caps.go` — `AnthropicVisionSupported(model)` longest-prefix vision lookup for the native Anthropic SDK wire
- `internal/llm/registry/` — Provider registry and factory logic
- `internal/llm/anthropiccompat/` — Anthropic-compatible wire implementation; `BuildMessages` emits `anthropic.NewImageBlockBase64` for `ContentImage` blocks
- `internal/llm/openaicompat/` — OpenAI-compatible wire implementation
- `internal/llm/openaicompat/caps.go` — `Capabilities` (incl. `PrependFormatting` for o-series, `Vision` for gpt-4o/gpt-4-turbo/gpt-4.1), `DefaultCaps`, `MergeCaps` against `config.CapsOverride`
- `internal/llm/openaicompat/translate.go` — `toWireMessages` (system-as-user splice when `SystemRole=="user"`, `Formatting re-enabled.\n` prepend when `PrependFormatting`); `translateAssistantMessage` forces `content=""` on tool-call-only turns; `translateUserMessage` switches to multimodal `MultiContent` array when ContentImage blocks are present
- `internal/llm/openaicompat/wire.go` — `chatMessage.MarshalJSON` renders `content` as either string or array based on Content vs MultiContent fields; `contentPart`/`contentPartImage` carry text + data-URL image parts

## How It Works
SAM abstracts LLM providers behind a common interface with two wire implementations. The config declares provider presets (minimax, anthropic, openai, arcee, moonshot, deepseek) keyed by name, each with wire type and API key env var. The registry dispatches by wire and instantiates the appropriate provider at runtime. `deepseek` rides the anthropic wire via DeepSeek's `/anthropic` endpoint and uses per-model `ModelMaxTokens` (192k default, 1M context, 120k thinking budget) resolved per turn by the agent.

OpenAI-compat capabilities branch on model prefix: gpt-5/o-series get `PrependFormatting=true` (literal `Formatting re-enabled.\n` as first line of system message — required to emit Markdown) and `developer` system role with `max_completion_tokens`. Trinity / strict OpenAI-compat servers reject assistant messages with `tool_calls` and null content; SAM forces `content=""` universally. Self-hosted original DeepSeek R1 weights configure `[providers.<name>.caps] system_role = "user"` to engage the system-as-user splice (`<system>...</system>` wrapped into first user message; no top-level system emitted).

## Where to Look
Start at `internal/llm/provider.go` for the interface contract, then explore the wire packages (anthropiccompat, openaicompat) for streaming, message translation, and capability handling. Config examples are in README.md.
