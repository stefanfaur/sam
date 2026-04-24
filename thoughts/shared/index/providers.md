# Domain: Providers
> Last updated: 2026-04-23

## Key Files
- `internal/llm/provider.go` — Provider interface definition
- `internal/llm/types.go` — Message and response type definitions
- `internal/llm/registry/` — Provider registry and factory logic
- `internal/llm/anthropiccompat/` — Anthropic-compatible wire implementation
- `internal/llm/openaicompat/` — OpenAI-compatible wire implementation

## How It Works
SAM abstracts LLM providers behind a common interface with two wire implementations. The config declares provider presets (minimax, anthropic, openai, arcee, moonshot) keyed by name, each with wire type and API key env var. The registry dispatches by wire and instantiates the appropriate provider at runtime.

## Where to Look
Start at `internal/llm/provider.go` for the interface contract, then explore the wire packages (anthropiccompat, openaicompat) for streaming, message translation, and capability handling. Config examples are in README.md.
