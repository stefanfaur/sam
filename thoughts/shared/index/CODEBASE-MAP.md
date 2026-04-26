# Codebase Map
> Last updated: 2026-04-26 · "SAM CLI agent with Go stack, Bubbletea TUI, multi-provider LLM support (Anthropic, OpenAI compatible), per-tool approval policies, and per-model system-prompt composition. Family map split 6→9 (gpt/gpt-reasoning, deepseek/deepseek-reasoner/deepseek-v4); base prompt rewritten with SAFETY/SCOPE/OUTPUT; openaicompat gained PrependFormatting cap, system-as-user splice, and tool-call empty-content fence; docs/families/*.md hosts maintainer reference per family. Hybrid Esc/Esc-Esc cancel scope (granular vs abort) plus mid-stream steer queue: Enter during pending turn queues input, drained at clean iteration boundary as a trailing text block on the tool_results user message, with end_turn auto-submit and provider-error preservation."

## Physical Modules
- **cmd/sam**             → CLI entry point, config loading, provider initialization, resolveSystemPrompt
- **internal/agent**      → Core agent loop, message history, tool submission and streaming, SetSystem for mid-session prompt swap
- **internal/config**     → TOML-based config parsing, provider presets, model defaults, PromptFamily type with longest-prefix FamilyForModel resolver
- **internal/llm**        → Provider interface, types, registry, Anthropic + OpenAI-compatible wires
- **internal/logging**    → Structured JSON logging to file and in-memory ring buffer
- **internal/policy**     → Per-tool approval rules, allowlist/denylist enforcement
- **internal/rtk**        → Optional transparent compression layer for Bash and Read tools
- **internal/skills**     → Skill discovery, YAML frontmatter parsing, skill execution
- **internal/system**     → Embedded + disk system prompt, tool descriptions, per-family prompt addenda (prompts/<family>.md), seed manifest
- **internal/tools**      → Read, Write, Edit, Bash tool implementations
- **internal/tui**        → Bubbletea-based terminal UI, settings modal, debug overlay, SystemResolverFn plumbing

## Tech Stack
- **Language:** Go 1.26.2
- **TUI Framework:** Charmbracelet (Bubbletea, Bubbles, Glamour, Huh, Lipgloss)
- **Config Format:** TOML (BurntSushi)
- **LLM SDKs:** Anthropic SDK (official), OpenAI-compatible wire
- **JSON Schema:** invopop/jsonschema
- **Utilities:** YAML parsing, clipboard, colorization, formatting

## Logical Domains → Detail Files
- **providers** → thoughts/shared/index/providers.md
- **agent** → thoughts/shared/index/agent.md
- **tools** → thoughts/shared/index/tools.md
- **config** → thoughts/shared/index/config.md
- **system** → thoughts/shared/index/system.md
- **ui** → thoughts/shared/index/ui.md
