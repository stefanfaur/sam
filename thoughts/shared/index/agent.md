# Domain: Agent
> Last updated: 2026-04-26

## Key Files
- `internal/agent/agent.go` — Core agent loop, message history, turn submission; setters (SetProvider, SetModel, SetMaxIters, SetSystem) for mid-session reconfiguration; steer-queue API (`QueueSteer`, `DiscardQueue`, `GetQueue`, `drainQueue`)
- `internal/agent/loop.go` — Turn body, stream consumption, tool dispatch (serial + parallel group); `mergeQueuedText` / `prependQueuedText` helpers for the mid-stream steer drain
- `cmd/sam/main.go` — Entry point, CLI flags, agent initialization

## How It Works
The Agent orchestrates turns: accept user input, invoke tools, stream LLM responses, accumulate message history, and loop until exit. Each turn runs tool approvals via policy, collects tool results, and rounds back. Reasoning models carry chain-of-thought via ContentThinking blocks.

`Agent.SetSystem` swaps the `baseSystem` and triggers `RebuildSkillCatalog`, so the effective prompt (base + skill catalog when the auto-invoke gate is on) reflects the new base on the next turn. The TUI calls it on every model-switch path so per-family prompt addenda stay in sync with the active model.

Tool dispatch within a turn is **group-and-preserve**: `loop.go` walks `pending` in order, collapses consecutive `ParallelSafe` calls into a concurrent group via `execParallelGroup`, and runs non-safe calls inline as sequential barriers. A package-level `parallelToolSem` (cap 8) bounds process-wide concurrency. Each goroutine writes its `ContentBlock` into a pre-sized slot, so `history` order matches call order regardless of completion order. Two layers of panic recovery (inside `typed.Run` and inside the dispatch goroutine) guarantee every slot is filled even on panic.

**Mid-stream steer queue.** Users can hit Enter mid-turn to queue follow-up messages without breaking the live turn. The queue is a `[]string` guarded by `steerMu`. The loop drains it at the **clean iteration boundary**: after `tool_results` are appended to history, `mergeQueuedText` appends a trailing `ContentText` block onto the same user message, so strict-alternation providers (DeepSeek) don't see two consecutive user messages. If the iteration ends with `stop="end_turn"` and the queue is non-empty, the loop continues with the queue text as a fresh user message instead of emitting `TurnDone`. On provider error the queue is preserved untouched, then `prependQueuedText` prepends it to the next submitted user message. `Esc-Esc` abort discards the queue; single-Esc granular cancel preserves it.

## Where to Look
`internal/agent/agent.go` contains the main loop, public API, and the steer-queue methods. Follow the `Submit` flow to understand turn mechanics. `SetSystem` sits near the other setters and relies on `RebuildSkillCatalog` to rebuild the effective prompt. `internal/agent/loop.go` holds `turn`, `execOne`, `execParallelGroup`, `parallelToolSem`, and the queue merge/prepend helpers — the merge is anchored right after the `tool_results` history append, the auto-submit branch sits in the `len(pending)==0 || stop=="end_turn"` block. `internal/agent/queue_integration_test.go` covers boundary drain, end-turn auto-submit, error preservation, and role alternation. `cmd/sam/main.go` shows initialization and graceful shutdown.
