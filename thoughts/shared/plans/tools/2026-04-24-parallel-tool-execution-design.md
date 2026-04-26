---
date: 2026-04-24T05:31:24Z
researcher: sfaur
git_commit: 7b808cc
branch: main
repository: sam
topic: "Parallel execution of tool calls within an assistant turn"
tags: [design, agent, tools, concurrency]
status: draft
last_updated: 2026-04-24
last_updated_by: sfaur
---

# Design: Parallel Tool Execution

## Problem

When the LLM emits multiple `tool_use` blocks in a single assistant message, the agent currently executes them sequentially (`internal/agent/loop.go:46-55`). On exploration-heavy turns (models routinely emit 3–10 parallel `Read` calls for multi-file context gathering), this serializes IO that the model already signaled as independent, inflating wall-time by roughly `N * read_latency` instead of `max(read_latency)`.

Providers already emit parallel tool calls by design; sequential execution leaves that latency on the floor.

## Goal

Execute independent `Read` calls concurrently within a single turn, without touching approval UX, result-pairing semantics, or any non-Read tool. Preserve call-order of results in history, preserve user-visible streaming guarantees, and make the change race-safe.

## Non-Goals

- Parallelizing `Bash`, `Edit`, or `Write` (out of scope; deferred until per-path serialization / read-only Bash detection is designed).
- Per-tool timeouts.
- Config-driven concurrency limits.
- Telemetry / metrics on parallel execution.
- Cross-turn concurrency (turns remain serialized by `Agent.run`).
- Reordering of unsafe calls relative to safe ones.

## Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Scope v1 | `Read` only | Pure IO, default-allow, no FS mutation, no approval path. |
| Concurrency cap | Fixed semaphore, size **8** | Caps fan-out from rogue outputs; 8 covers typical parallel-read turns; no config knob (YAGNI). |
| TUI event ordering | No change — key by call `ID` | `ToolCall` already emitted up-front during streaming (`loop.go:145`); `ToolResult` already matched by `ID` (`tui/update.go:621`). Parallel exec produces out-of-completion-order `ToolResult` events that pair correctly. |
| Mixed batches | Group-and-preserve | Walk `pending` in order; consecutive parallel-safe calls run as a concurrent group; non-safe calls act as sequential barriers. Preserves any side-effect ordering encoded by the model. |
| Capability declaration | `Tool.ParallelSafe() bool` on the `Tool` interface | Tool owns the answer; future-proof for enabling other tools one at a time. |
| Rollout | Always on, no flag | Parallel `Read` is boring and safe; no maintenance surface for a knob that would never flip. |
| `a.readFiles` | **Delete** the field | It is write-only — no reader exists in the codebase (verified via `grep`). `tools.ReadTracker` (`internal/tools/read.go:28-48`) is the real, already-mutex-protected tracker and is invoked from `runRead` itself. The `a.readFiles` write in `runToolCall` is dead code; removing it also removes the race. |
| Panic isolation | Per-goroutine `recover` in `execOne` | Today `typed.Run`'s recover (`registry.go:33-37`) swallows panics but returns a zero `Result`, so under sequential execution a panicking tool silently produces an empty output. Under parallel execution the same panic would leave `out[idx]` unset. `execOne` wraps the body in a `defer recover()` that materializes an error `Result`. |

## Architecture

### Tool interface change — `internal/tools/tool.go`

Add one method:

```go
type Tool interface {
    Name() string
    Description() string
    Schema() map[string]any
    Run(ctx context.Context, raw json.RawMessage) (Result, error)
    ParallelSafe() bool
}
```

### Generic constructor — `internal/tools/registry.go`

Embed a small non-generic base into `typed[In]` so functional options can mutate it generically. Move the existing `name`, `desc`, `sch` fields into the base. Switch the Tool implementation to a pointer receiver so `New` can apply options after allocation (current code returns `typed[In]` by value; since it's only exposed through the `Tool` interface, the switch to `*typed[In]` is transparent to callers):

```go
type typedBase struct {
    name         string
    desc         string
    sch          map[string]any
    parallelSafe bool
}

type typed[In any] struct {
    typedBase
    run func(context.Context, In) (Result, error)
}

type Option func(*typedBase)

func ParallelSafe() Option {
    return func(b *typedBase) { b.parallelSafe = true }
}

func New[In any](
    name, desc string,
    fn func(context.Context, In) (Result, error),
    opts ...Option,
) Tool {
    t := &typed[In]{
        typedBase: typedBase{name: name, desc: desc, sch: SchemaOf[In]()},
        run:       fn,
    }
    for _, o := range opts {
        o(&t.typedBase)
    }
    return t
}

func (t *typed[In]) Name() string           { return t.name }
func (t *typed[In]) Description() string    { return t.desc }
func (t *typed[In]) Schema() map[string]any { return t.sch }
func (t *typed[In]) ParallelSafe() bool     { return t.parallelSafe }

func (t *typed[In]) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
    // body unchanged from current value-receiver version
}
```

Existing call sites (`NewEdit`, `NewWrite`, `NewBash`, `NewRead`) continue to compile because `opts ...Option` is variadic and none of them pass options today. `NewRead` appends `ParallelSafe()`.

### Read opts in — `internal/tools/read.go`

```go
return New[ReadInput]("Read", description, runRead, ParallelSafe())
```

`Edit`, `Write`, `Bash` constructors unchanged; `ParallelSafe()` returns `false` by default.

### Dispatch change — `internal/agent/loop.go:45-55`

Replace the single `for _, call := range pending` loop with group-and-preserve execution:

```go
results := make([]llm.ContentBlock, len(pending))
i := 0
for i < len(pending) {
    tool, _ := a.tools.Get(pending[i].Name)
    if tool == nil || !tool.ParallelSafe() {
        results[i] = a.execOne(ctx, s, pending[i])
        i++
        continue
    }
    j := i
    for j < len(pending) {
        t, _ := a.tools.Get(pending[j].Name)
        if t == nil || !t.ParallelSafe() {
            break
        }
        j++
    }
    a.execParallelGroup(ctx, s, pending[i:j], results[i:j])
    i = j
}
a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: results})
```

`execOne` wraps the current `runToolCall` body (policy check, approval, run, emit `ToolResult`) and returns the resulting `ContentBlock`. Panic recovery lives in the goroutine defer in `execParallelGroup` plus the tightened `typed.Run` recover — `execOne` itself stays straight-line:

```go
func (a *Agent) execOne(ctx context.Context, s submit, call ToolCall) llm.ContentBlock {
    res := a.runToolCall(ctx, s, call)
    emitToChan(s.out, ToolResult{ID: call.ID, Name: call.Name, Output: res.Output, IsError: res.IsError, Rewritten: res.Rewritten}, s.ctx)
    return llm.ContentBlock{
        Type:      llm.ContentToolResult,
        ToolUseID: call.ID,
        Output:    res.Output,
        IsError:   res.IsError,
    }
}
```

Because `typed.Run`'s existing `recover` (`registry.go:33-37`) currently swallows the panic and returns a **zero** `Result` (empty output, `IsError=false`), it must be tightened to return an explicit error result. Otherwise a panicking tool produces a silently-empty reply to the LLM:

```go
func (t *typed[In]) Run(ctx context.Context, raw json.RawMessage) (res Result, err error) {
    defer func() {
        if r := recover(); r != nil {
            res = Result{Output: fmt.Sprintf("panic in tool %s: %v", t.name, r), IsError: true}
            err = nil
        }
    }()
    // ... existing body
}
```

This tightening is valuable on its own (sequential callers also benefit) and is in-scope because the parallel path depends on it.

`execParallelGroup` spawns one goroutine per call, each gated by a shared semaphore:

```go
var parallelToolSem = make(chan struct{}, 8)

func (a *Agent) execParallelGroup(
    ctx context.Context, s submit,
    calls []ToolCall, out []llm.ContentBlock,
) {
    var wg sync.WaitGroup
    for idx, call := range calls {
        wg.Add(1)
        go func(idx int, call ToolCall) {
            defer wg.Done()
            defer func() {
                if r := recover(); r != nil {
                    out[idx] = llm.ContentBlock{
                        Type:      llm.ContentToolResult,
                        ToolUseID: call.ID,
                        Output:    fmt.Sprintf("panic during tool dispatch: %v", r),
                        IsError:   true,
                    }
                }
            }()
            parallelToolSem <- struct{}{}
            defer func() { <-parallelToolSem }()
            out[idx] = a.execOne(ctx, s, call)
        }(idx, call)
    }
    wg.Wait()
}
```

Every goroutine writes exactly one `ContentBlock` to its `out[idx]` slot (success, tool error, or panic-recovered), so the final history append sees no zero-value blocks.

### Dead-code removal — `a.readFiles`

`a.readFiles` (`agent.go:36`, written at `loop.go:242`) has **no readers** in the codebase (verified: `grep -rn readFiles internal/` shows only the declaration, constructor init, a stale comment on `Reset`, and the single write). The actual "was this file read?" gate is `tools.ReadTracker` (`tools/read.go:28-48`), which has its own `sync.RWMutex`, is wired into the `Read` tool from `cmd/sam/main.go`, and is what `Edit`/`Write` query via `tracker.Seen(...)`.

Remove:
- The `readFiles map[string]struct{}` field and its constructor init in `agent.go`.
- The `if call.Name == "Read" && !result.IsError { … a.readFiles[...] = struct{}{} }` block in `runToolCall`.
- The `// (readFiles stays)` aside on the `Reset` comment (`agent.go:225`).

This removes the race entirely — no lock needed, no sync map needed. `ReadTracker` already handles concurrent Reads safely.

### No change required

- `consumeStream` — unchanged; still emits one `ToolCall` event per `tool_use` block during streaming.
- Approval flow — parallel-safe tools are all default-`Allow`; the approval channel path in `runToolCall` never fires for them.
- Provider, TUI, policy, skills.

## Correctness Argument

1. **Result ordering in history:** results are written to pre-sized slots indexed by call position. Final `history` append sees calls in the same order the model emitted them.
2. **Tool-result pairing for provider:** Anthropic and OpenAI match `tool_result` → `tool_use` by `id`, not position. Completion-order emission to TUI is a pure UX concern, and TUI already keys by `ID` (`tui/update.go:617-625`).
3. **Shared state in parallel group:**
   - `a.tools` — `Registry.Get` is `RLock`-guarded (`registry.go:56-61`); concurrent reads are safe.
   - `a.policy` — the `*Policy` pointer is set once in `New` and never swapped; its internals (`defaults`, `session`) are mutex-guarded.
   - `a.readFiles` — deleted (see above); no remaining agent-level map is written during tool dispatch.
   - `ReadTracker` — already `sync.RWMutex`-guarded; safe under concurrent `Mark`.
   - `history` and `msg.Content` — touched only outside the parallel region.
4. **Cancellation:** `ctx` passed through to each goroutine; `ctx.Done()` short-circuits each `runToolCall`. Existing `s.ctx.Err()` check after the group catches cancellations cleanly.
5. **Semaphore correctness:** buffered `chan struct{}` with `cap = 8`; acquire at goroutine start, release in `defer`. No deadlock because there is no cross-goroutine dependency inside the group.
6. **Event-channel backpressure.** `s.out` has capacity 64 (`agent.go:166,206`). Up to 8 concurrent goroutines may each emit one `ToolResult` before the TUI drains. 8 ≪ 64, so parallel emission cannot fill the channel on its own. A slow TUI consumer can still backpressure — `emitToChan` uses `select { case out <- e: case <-ctx.Done(): }`, so a stuck consumer parks each goroutine on its emit until `ctx` cancels. That blocks `wg.Wait()` and holds slots in the semaphore, but does **not** deadlock: Bubbletea's `waitAgent` drains the channel one event at a time from the TUI loop, and ctx cancellation propagates through `CancelCurrent`. This is the same exposure sequential execution has today; parallel execution does not worsen it.
7. **Panic isolation.** Two layers of `recover`: inside `typed.Run` (tightened to return an error `Result` instead of a zero `Result`), and inside the goroutine body (produces a `ContentBlock` with `IsError=true` if anything panics outside `Run`). No goroutine can exit without writing its `out[idx]` slot.

## Testing

Tests use a fake tool that implements `Tool` directly (no need for a new registry abstraction — `Registry.Register` takes any `Tool`). The fake accepts a per-call "gate" channel so concurrency is verified deterministically, not by wall-time sleeps.

Add a new file `internal/agent/loop_parallel_test.go`:

1. **Group boundaries.** Batch `[Read, Read, Bash, Read]`. Fake `Read` is `ParallelSafe=true`, fake `Bash` is `ParallelSafe=false`. Use call-ordering channels to record execution interleave. Assert: the first two `Read`s executed with overlapping lifetimes; `Bash` started only after both finished; the trailing `Read` started only after `Bash` finished; `results[idx]` matches call order.
2. **True concurrency (deterministic).** Install a fake `Read` that signals `started <- call.ID` on entry, then blocks on `release <- ` until the test closes it. Submit 4 Reads. Test reads 4 start signals off `started` **before** closing `release` — proves all 4 goroutines are in flight simultaneously. No sleeps, no timing margins.
3. **Result order preserved under random completion order.** Fake `Read` returns `call.ID` after a short randomized per-call delay (1–5 ms). Assert `history[last].Content[idx].Output == calls[idx].ID` for each `idx`.
4. **Semaphore cap respected.** Fake `Read` increments an atomic `inflight` counter on entry, decrements on exit; tracks max value seen. Submit 20 Reads (model can emit more than cap). Assert `maxInflight <= 8`.
5. **Panic isolation.** Fake `Read` panics on `call.ID == "t2"`. Submit 3 parallel Reads. Assert the other two produce normal `Result`s; the panicking one yields a `ContentBlock` with `IsError=true` and a non-empty `Output`.
6. **Cancellation.** Start a parallel group of 4 blocked Reads, call `Agent.CancelCurrent()`. Assert the turn emits `ErrorEvent` with `ctx.Canceled`; all 4 goroutines exit; semaphore is drained (assertable by submitting a follow-up turn and confirming it acquires immediately).
7. **`ToolResult` events.** Capture events; assert one `ToolResult` per call, all pairable by `ID`, no assertion on arrival order.
8. **Tool-level:** `tools/read_test.go` asserts `ParallelSafe() == true`; `edit_test.go`, `write_test.go`, `bash_test.go` assert `== false`. `registry.go` panic-recovery test asserts `typed.Run` returns an error `Result` when the tool body panics.

All tests run under `go test -race` in CI. Existing `agent_test.go` and TUI integration tests (`tui/integration_cards_test.go`, `tui/update_events_test.go`) must pass unchanged — the protocol surface is unchanged.

## Files Changed

| File | Change |
|---|---|
| `internal/tools/tool.go` | Add `ParallelSafe() bool` to `Tool` interface. |
| `internal/tools/registry.go` | Introduce `typedBase` embedded struct; `Option` type; `ParallelSafe()` helper; switch `typed[In]` to pointer receivers; tighten the existing `recover` in `Run` to return an error `Result`. |
| `internal/tools/read.go` | Pass `ParallelSafe()` option to `New[ReadInput]`. |
| `internal/agent/agent.go` | Remove dead `readFiles` field and constructor init; update `Reset` comment. |
| `internal/agent/loop.go` | Replace serial tool dispatch with group-and-preserve; factor `execOne`; add `execParallelGroup`; add `parallelToolSem`; remove the `readFiles` write block in `runToolCall`. |
| `internal/agent/loop_parallel_test.go` | New test file — 7 tests covering group boundaries, concurrency, ordering, semaphore cap, panic isolation, cancellation, event pairing. |
| `internal/tools/read_test.go`, `edit_test.go`, `write_test.go`, `bash_test.go` | Assert `ParallelSafe()` returns expected value. |
| `internal/tools/registry_test.go` | New test: panicking tool body produces error `Result`, not zero `Result`. |

## Risks & Mitigations

- **Risk:** A future tool added with `ParallelSafe()` returning `true` but with hidden shared state. **Mitigation:** Interface docstring states the contract; race-mode CI catches real violations.
- **Risk:** Semaphore size 8 wrong for some workloads. **Mitigation:** Package-level `var` — one-line change. No config surface until profiling shows need.
- **Risk:** Tightening `typed.Run`'s recover changes behavior for sequential callers too (previously a tool panic produced a zero `Result`; now it produces an error `Result`). **Mitigation:** This is strictly better — the old behavior silently lied to the LLM. No test depends on the zero-result path (verified by grep for `IsError: false` in tool tests).
- **Risk:** Pointer-receiver switch on `typed[In]` is a subtle ABI change. **Mitigation:** `Tool` is an interface, not a concrete type, and no call site takes `typed[In]` by value; all go through `Tool`. Switching to `*typed[In]` is source-compatible for every caller in the tree.

## Open Questions

None — all decisions locked.
