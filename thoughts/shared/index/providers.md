# Domain: Providers
> Last updated: 2026-04-24

## Key Files
- `internal/llm/provider.go` — Provider interface definition
- `internal/llm/types.go` — Message and response type definitions
- `internal/llm/registry/` — Provider registry and factory logic
- `internal/llm/anthropiccompat/` — Anthropic-compatible wire implementation
- `internal/llm/openaicompat/` — OpenAI-compatible wire implementation

## How It Works
SAM abstracts LLM providers behind a common interface with two wire implementations. The config declares provider presets (minimax, anthropic, openai, arcee, moonshot, deepseek) keyed by name, each with wire type and API key env var. The registry dispatches by wire and instantiates the appropriate provider at runtime. `deepseek` rides the anthropic wire via DeepSeek's `/anthropic` endpoint and uses per-model `ModelMaxTokens` (192k default, 1M context, 120k thinking budget) resolved per turn by the agent.

## Where to Look
Start at `internal/llm/provider.go` for the interface contract, then explore the wire packages (anthropiccompat, openaicompat) for streaming, message translation, and capability handling. Config examples are in README.md.
