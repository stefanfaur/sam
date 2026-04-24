# Domain: Agent
> Last updated: 2026-04-24

## Key Files
- `internal/agent/agent.go` — Core agent loop, message history, turn submission; setters (SetProvider, SetModel, SetMaxIters, SetSystem) for mid-session reconfiguration
- `internal/agent/tools.go` — Tool invocation and result handling
- `cmd/sam/main.go` — Entry point, CLI flags, agent initialization

## How It Works
The Agent orchestrates turns: accept user input, invoke tools, stream LLM responses, accumulate message history, and loop until exit. Each turn runs tool approvals via policy, collects tool results, and rounds back. Reasoning models carry chain-of-thought via ContentThinking blocks.

`Agent.SetSystem` swaps the `baseSystem` and triggers `RebuildSkillCatalog`, so the effective prompt (base + skill catalog when the auto-invoke gate is on) reflects the new base on the next turn. The TUI calls it on every model-switch path so per-family prompt addenda stay in sync with the active model.

## Where to Look
`internal/agent/agent.go` contains the main loop and public API. Follow the `Submit` flow to understand turn mechanics. `SetSystem` sits near the other setters and relies on `RebuildSkillCatalog` to rebuild the effective prompt. `cmd/sam/main.go` shows initialization and graceful shutdown.
