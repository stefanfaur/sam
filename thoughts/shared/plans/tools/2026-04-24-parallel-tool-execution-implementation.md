---
date: 2026-04-24T05:31:24Z
researcher: sfaur
git_commit: 7b808cc
branch: main
repository: sam
topic: "Implementation plan: parallel tool execution"
tags: [implementation, agent, tools, concurrency]
status: ready
last_updated: 2026-04-24
last_updated_by: sfaur
design_doc: thoughts/shared/plans/tools/2026-04-24-parallel-tool-execution-design.md
---

# Implementation Plan: Parallel Tool Execution

**Design:** `thoughts/shared/plans/tools/2026-04-24-parallel-tool-execution-design.md` (read before starting).

Nine phases, each a small, independently-commitable, verifiable step. Phases 1–5 are pure refactors and dead-code removal with zero behavioral change — they're safe to land before touching dispatch. Phases 6–8 introduce the parallel path. Phase 9 is the final verification gate.

## Prerequisites

- [ ] Read the design doc end-to-end.
- [ ] On a clean working tree off `main`.
- [ ] `go test ./...` green baseline.
- [ ] `go test -race ./...` green baseline.

---

## Phase 1 — Tool interface + `typedBase` refactor

**Why:** Foundation. Introduces `ParallelSafe()` on the `Tool` interface, embeds a `typedBase` struct into the generic `typed[In]` so functional options can mutate capability bits generically, and switches concrete implementer to pointer receivers. Zero behavior change (no call site passes options yet).

### Changes

**`internal/tools/tool.go`** — add method to interface:

```go
type Tool interface {
    Name() string
    Description() string
    Schema() map[string]any
    Run(ctx context.Context, raw json.RawMessage) (Result, error)
    ParallelSafe() bool
}
```

**`internal/tools/registry.go`** — replace the `typed[In]` value-type + `New[In]` pair (current `registry.go:11-39`) with:

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
    var in In
    if len(raw) > 0 {
        if err := json.Unmarshal(raw, &in); err != nil {
            return Result{Output: "invalid input: " + err.Error(), IsError: true}, nil
        }
    }
    defer func() {
        if r := recover(); r != nil {
            // caught panic - convert to error result
        }
    }()
    return t.run(ctx, in)
}
```

The `Run` body is a verbatim carry-over from the current implementation (same no-op recover as today — Phase 2 tightens it). Keep the existing `Registry` code unchanged (it already accepts `Tool`).

### Verify

- [ ] `go build ./...` compiles.
- [ ] `go test ./internal/tools/... ./internal/agent/...` green.
- [ ] `go vet ./...` clean.

### Commit

> `refactor(tools): introduce typedBase and ParallelSafe option`
>
> Add `ParallelSafe() bool` to the `Tool` interface. Embed a non-generic `typedBase` into `typed[In]` so functional options can mutate capability bits without leaking generics. Switch to pointer receivers so option application stays on a single instance. All existing constructors (`NewRead`, `NewWrite`, `NewEdit`, `NewBash`) continue to compile unchanged; `ParallelSafe()` defaults to `false`.

---

## Phase 2 — Tighten `typed.Run` panic recovery

**Why:** Today's `recover` swallows panics and silently returns a zero `Result`, which would lie to the LLM ("empty output, no error"). Parallel dispatch makes this worse because sibling goroutines depend on each slot being filled correctly. Fix it now as a standalone quality improvement.

### Changes

**`internal/tools/registry.go`** — replace the `Run` method body with a named-return + real recovery:

```go
func (t *typed[In]) Run(ctx context.Context, raw json.RawMessage) (res Result, err error) {
    var in In
    if len(raw) > 0 {
        if err := json.Unmarshal(raw, &in); err != nil {
            return Result{Output: "invalid input: " + err.Error(), IsError: true}, nil
        }
    }
    defer func() {
        if r := recover(); r != nil {
            res = Result{
                Output:  fmt.Sprintf("panic in tool %s: %v", t.name, r),
                IsError: true,
            }
            err = nil
        }
    }()
    return t.run(ctx, in)
}
```

Add `"fmt"` import if not already present.

**`internal/tools/registry_test.go`** (new file) — TDD: write the test first, run it, watch it fail, then apply the fix. Tests:

```go
package tools

import (
    "context"
    "testing"
)

func TestTypedRunRecoversPanicToErrorResult(t *testing.T) {
    tool := New[struct{}]("panicker", "", func(ctx context.Context, _ struct{}) (Result, error) {
        panic("boom")
    })
    res, err := tool.Run(context.Background(), []byte(`{}`))
    if err != nil {
        t.Fatalf("expected nil error, got %v", err)
    }
    if !res.IsError {
        t.Error("expected IsError=true on panic")
    }
    if res.Output == "" {
        t.Error("expected non-empty Output on panic")
    }
}
```

### Verify

- [ ] `go test -run TestTypedRunRecoversPanicToErrorResult ./internal/tools/` passes.
- [ ] `go test ./...` green.
- [ ] `grep -rn "IsError: false" internal/tools/ | grep -i panic` returns nothing (no test depends on old zero-result behavior).

### Commit

> `fix(tools): typed.Run panic recover returns error Result`
>
> Previously, a panicking tool body produced a zero `Result` (`Output=""`, `IsError=false`) — indistinguishable from a successful empty-output run. Return an explicit error result with the panic value so callers (and the LLM) can see what happened. Adds regression test.

---

## Phase 3 — `Read` opts into parallel-safe + per-tool assertions

**Why:** Flip the capability bit on `Read`. Lock down the default for `Edit`/`Write`/`Bash` with tests so a future refactor can't silently change it.

### Changes

**`internal/tools/read.go`** — append the option at the `New[ReadInput]` call (currently `read.go:51`):

```go
return New[ReadInput]("Read", description, func(ctx context.Context, in ReadInput) (Result, error) {
    return runRead(ctx, in, tracker, rtkClient)
}, ParallelSafe())
```

**`internal/tools/read_test.go`** — add:

```go
func TestReadToolIsParallelSafe(t *testing.T) {
    tool := NewRead(NewReadTracker(), nil, "")
    if !tool.ParallelSafe() {
        t.Error("Read must be ParallelSafe")
    }
}
```

**`internal/tools/edit_test.go`** — add:

```go
func TestEditToolIsNotParallelSafe(t *testing.T) {
    tool := NewEdit(NewReadTracker(), "")
    if tool.ParallelSafe() {
        t.Error("Edit must not be ParallelSafe")
    }
}
```

**`internal/tools/write_test.go`** — add the equivalent `TestWriteToolIsNotParallelSafe`. Look up `NewWrite` signature in `write.go` and pass whatever zero-value deps it requires.

**`internal/tools/bash_test.go`** — add the equivalent `TestBashToolIsNotParallelSafe`. Same treatment.

### Verify

- [ ] `go test ./internal/tools/` green.
- [ ] All four `ParallelSafe` assertions pass.

### Commit

> `feat(tools): mark Read as parallel-safe`
>
> `Read` is pure IO with no mutable state beyond the already-thread-safe `ReadTracker`, so it can run concurrently with sibling calls within an assistant turn. Opt in via the new `ParallelSafe()` option. Lock the default-`false` behavior for `Edit`/`Write`/`Bash` with assertions.

---

## Phase 4 — Delete `Agent.readFiles` dead code

**Why:** `a.readFiles` is written at `loop.go:242` and has **zero readers** anywhere in the tree (verified: `grep -rn readFiles internal/` shows only the declaration, the constructor init, the write, and a stale comment). The real "was this file read?" gate is `tools.ReadTracker`, wired from `cmd/sam/main.go` and already mutex-guarded. Removing the dead write eliminates the race that parallel `Read` would otherwise trigger — no lock needed.

### Changes

**`internal/agent/agent.go`** — delete:

- Line 36: `readFiles map[string]struct{}` field.
- Line 84: `readFiles:  make(map[string]struct{}),` in the `New` struct literal.
- Line 225: remove the trailing `(readFiles stays).` phrase from the `Reset` comment so it reads cleanly: `// Reset clears history and session allowlist.`

**`internal/agent/loop.go`** — delete the block at lines 237-244:

```go
// Track read files
if call.Name == "Read" && !result.IsError {
    var input struct {
        FilePath string `json:"file_path"`
    }
    if err := json.Unmarshal(call.Input, &input); err == nil && input.FilePath != "" {
        a.readFiles[input.FilePath] = struct{}{}
    }
}
```

Remove the `encoding/json` import from `loop.go` only if no other usage remains (grep the file after delete).

### Verify

- [ ] `grep -rn readFiles internal/` returns nothing.
- [ ] `go build ./...` compiles.
- [ ] `go test ./...` green.
- [ ] `go test -race ./...` green.

### Commit

> `refactor(agent): remove dead readFiles field`
>
> `Agent.readFiles` was written on every successful `Read` call but never read. The real file-read gate is `tools.ReadTracker`, wired into the `Read` tool itself and queried by `Edit`/`Write`. Deleting the write eliminates a concurrent-map-write hazard that would have appeared once parallel `Read` dispatch lands.

---

## Phase 5 — Extract `execOne` (pure refactor)

**Why:** Separate the "run a single tool call and produce its history block + TUI event" responsibility from the dispatch loop. No behavior change; prepares for phase 6/7.

### Changes

**`internal/agent/loop.go`** — below `runToolCall`, add:

```go
// execOne runs a single tool call, emits its ToolResult event, and returns the
// content block to append to history. Used by both serial and parallel dispatch.
func (a *Agent) execOne(ctx context.Context, s submit, call ToolCall) llm.ContentBlock {
    res := a.runToolCall(ctx, s, call)
    emitToChan(s.out, ToolResult{
        ID:        call.ID,
        Name:      call.Name,
        Output:    res.Output,
        IsError:   res.IsError,
        Rewritten: res.Rewritten,
    }, s.ctx)
    return llm.ContentBlock{
        Type:      llm.ContentToolResult,
        ToolUseID: call.ID,
        Output:    res.Output,
        IsError:   res.IsError,
    }
}
```

Replace the current for-loop in `turn` (currently `loop.go:45-55`):

```go
results := make([]llm.ContentBlock, 0, len(pending))
for _, call := range pending {
    results = append(results, a.execOne(ctx, s, call))
}
```

This is exactly what the current code does — same emit, same block shape, same serial order — just factored.

### Verify

- [ ] `go test ./...` green (all existing tests must pass — this is a pure refactor).
- [ ] `go test -race ./...` green.
- [ ] Manual smoke: `go build ./cmd/sam && ./sam` launches, simple "read file X" prompt works end-to-end.

### Commit

> `refactor(agent): extract execOne from turn dispatch`
>
> Factor the per-call "run, emit event, produce history block" sequence into its own method. No behavior change; prepares for the upcoming parallel dispatch path which needs this piece to be reusable.

---

## Phase 6 — Add `parallelToolSem` + `execParallelGroup`

**Why:** Introduce the parallel primitive without wiring it to dispatch yet. Keeps phase 7 small.

### Changes

**`internal/agent/loop.go`** — add imports (`sync`, `fmt`) if missing. Add at package scope (after the imports block):

```go
// parallelToolSem caps concurrent parallel-safe tool executions across the
// process. A turn rarely emits more than a handful of parallel-safe calls, so
// 8 covers typical fan-out without a config knob.
var parallelToolSem = make(chan struct{}, 8)
```

Add the method alongside `execOne`:

```go
// execParallelGroup runs the given calls concurrently, writing each call's
// resulting ContentBlock into out[idx] at the matching index. Every goroutine
// writes exactly one block, including when the tool panics. Blocks until the
// whole group is joined.
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

Nothing calls this yet — it should compile and existing tests should stay green.

### Verify

- [ ] `go build ./...` compiles.
- [ ] `go vet ./...` clean.
- [ ] `go test ./...` green (the method is dead code at this phase).

### Commit

> `feat(agent): add execParallelGroup primitive`
>
> Introduce a package-level 8-wide semaphore and a helper that runs a batch of tool calls concurrently, writing each result into a caller-provided slice at the matching index. Panic-safe: every goroutine writes exactly one `ContentBlock`. Not wired to dispatch yet.

---

## Phase 7 — Wire dispatch to group-and-preserve

**Why:** Replace the serial loop from phase 5 with the group-and-preserve algorithm that actually exploits the new primitive.

### Changes

**`internal/agent/loop.go`** — replace the serial dispatch block (the one written in phase 5) with:

```go
results := make([]llm.ContentBlock, len(pending))
i := 0
for i < len(pending) {
    headTool, _ := a.tools.Get(pending[i].Name)
    if headTool == nil || !headTool.ParallelSafe() {
        results[i] = a.execOne(ctx, s, pending[i])
        i++
        continue
    }
    j := i + 1
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
```

Leave the `a.history = append(...)` line that follows unchanged — `results` is now a pre-sized slice, indexed by call position, filled by either path.

### Verify

- [ ] `go build ./...` compiles.
- [ ] `go test ./...` green — existing tests (`TestOneToolTurn`, `TestMaxIterationsCap`, `TestMultiTurnReasoningPreservation`) exercise single-tool and tool-loop flows and must still pass.
- [ ] `go test -race ./...` green.
- [ ] Manual smoke: run `sam` binary, prompt "read three small files and summarize them" — verify no visible regression.

### Commit

> `feat(agent): group-and-preserve parallel tool dispatch`
>
> Walk `pending` in order. Consecutive parallel-safe calls execute as a concurrent group via `execParallelGroup`; non-safe calls run inline and act as sequential barriers. History order is preserved because every goroutine writes into its matching slot. No protocol change — providers match `tool_result` to `tool_use` by ID, and the TUI already keys its rendering by call ID.

---

## Phase 8 — Parallel-dispatch test suite

**Why:** Lock the behavior in. Deterministic, race-clean, no sleeps.

### New file: `internal/agent/loop_parallel_test.go`

Shared helper — a gate-based fake tool:

```go
package agent

import (
    "context"
    "encoding/json"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/stefanfaur/sam/internal/llm"
    "github.com/stefanfaur/sam/internal/llm/fake"
    "github.com/stefanfaur/sam/internal/policy"
    "github.com/stefanfaur/sam/internal/tools"
)

type gateTool struct {
    name         string
    parallelSafe bool
    run          func(ctx context.Context, raw json.RawMessage) (tools.Result, error)
}

func (g *gateTool) Name() string                                                   { return g.name }
func (g *gateTool) Description() string                                            { return "" }
func (g *gateTool) Schema() map[string]any                                         { return map[string]any{"type": "object", "additionalProperties": false} }
func (g *gateTool) ParallelSafe() bool                                             { return g.parallelSafe }
func (g *gateTool) Run(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
    return g.run(ctx, raw)
}
```

Helper to build a fake-provider script with N parallel tool_use blocks (one assistant turn, one end-of-turn second turn):

```go
func parallelToolScript(calls []struct{ ID, Name, Input string }) []fake.Script {
    first := fake.Script{{Type: llm.EventMessageStart}}
    for _, c := range calls {
        first = append(first,
            llm.StreamEvent{Type: llm.EventToolUseStart, ToolUseID: c.ID, ToolName: c.Name},
            llm.StreamEvent{Type: llm.EventToolUseDelta, ToolUseID: c.ID, PartialJSON: c.Input},
            llm.StreamEvent{Type: llm.EventToolUseStop, ToolUseID: c.ID},
        )
    }
    first = append(first, llm.StreamEvent{Type: llm.EventMessageStop, StopReason: "tool_use"})
    final := fake.Script{
        {Type: llm.EventMessageStart},
        {Type: llm.EventTextDelta, Text: "done"},
        {Type: llm.EventMessageStop, StopReason: "end_turn"},
    }
    return []fake.Script{first, final}
}
```

Tests:

1. **`TestParallelGroupRunsConcurrently`** — deterministic concurrency proof. Fake Read signals `started <- id` on entry, blocks on `<-release`. Submit 4 parallel Reads. Read 4 entries from `started` with a 2s test timeout, then close `release`. Assert all 4 started before any finished.

2. **`TestParallelGroupPreservesResultOrder`** — seed `rng := rand.New(rand.NewSource(1))` at test start for deterministic shuffling, then fake Read does `time.Sleep(time.Duration(rng.Intn(5))*time.Millisecond)` under a mutex (rng is not thread-safe), then returns `id` as output. Submit 6 parallel Reads. After `TurnDone`, inspect `agent.history`: the last user message's `Content[idx].Output` must equal `calls[idx].ID` for every idx (proves slot-indexed writes preserve order).

3. **`TestSemaphoreCapsConcurrency`** — fake Read atomically increments `inflight` on entry, samples into an `atomic.Int32 maxSeen`, sleeps 1ms, decrements, returns. Submit 20 parallel Reads. Assert `maxSeen.Load() <= 8`.

4. **`TestMixedBatchGroupAndPreserve`** — register fake Read (parallel-safe) and fake Bash (not). Script emits `[Read t1, Read t2, Bash t3, Read t4]`. Each tool appends its ID to a shared `order` slice under a mutex, and records its start time. Assert: t1 and t2 overlap (start<end interleave); t3 starts strictly after both t1 and t2 end; t4 starts strictly after t3 ends; `agent.history[last].Content[idx].ToolUseID` matches call order.

5. **`TestPanicInParallelGroupIsolated`** — fake Read panics iff `call.ID == "t2"`. Submit `[Read t1, Read t2, Read t3]`. Assert: `history[last].Content[0].IsError == false`; `Content[1].IsError == true` and `Output` contains "panic"; `Content[2].IsError == false`. Assert no unhandled panic escaped (test simply completing is sufficient).

6. **`TestParallelGroupCancellation`** — fake Read blocks on **either** ctx cancellation or a `release` signal, whichever comes first:

   ```go
   run: func(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
       select {
       case <-ctx.Done():
           return tools.Result{Output: "cancelled", IsError: true}, nil
       case <-release:
           return tools.Result{Output: "ok"}, nil
       }
   }
   ```

   Submit 4 parallel Reads. After a short `time.Sleep(20*time.Millisecond)` to let goroutines launch, call `agent.CancelCurrent()`. Drain events; assert `ErrorEvent` observed. Do **not** close `release` — the whole point is to verify ctx propagates through `runToolCall` into the tool body, unblocking it without any other signal. `goleak.VerifyTestMain` catches any goroutine that didn't exit on ctx cancellation, so no follow-up turn is needed.

7. **`TestToolResultEventsAllPair`** — submit 5 parallel Reads, capture events, assert exactly 5 `ToolResult` events and that `{ID}` set matches the set of submitted call IDs. Do not assert arrival order.

### Verify

- [ ] `go test -race ./internal/agent/...` green.
- [ ] `go test -race ./...` green (whole tree).
- [ ] `go test -count=50 -race ./internal/agent/ -run TestParallel` — 50 reps clean (weed out residual flake).

### Commit

> `test(agent): parallel tool dispatch coverage`
>
> Seven tests exercising the group-and-preserve dispatch path: concurrency proof, result-order preservation, semaphore cap, mixed batches, panic isolation, cancellation cleanup, event pairing. All deterministic via gate channels and atomic counters — no wall-time sleeps for concurrency assertions. `goleak.VerifyTestMain` already installed in this package catches stuck goroutines.

---

## Phase 9 — Final verification + index update

### Verification checklist

- [ ] `go build ./...` clean.
- [ ] `go vet ./...` clean.
- [ ] `go test ./...` green.
- [ ] `go test -race ./...` green.
- [ ] `go test -count=20 -race ./internal/agent/ ./internal/tools/` green (flake check).
- [ ] Manual smoke with a real provider: prompt "read files X, Y, Z and summarize" — wall-time noticeably faster than prior commit for >2 reads; single-read turns unchanged; edit/write turns unchanged.
- [ ] `grep -rn readFiles internal/` empty.
- [ ] `grep -rn "ParallelSafe" internal/tools/ | wc -l` shows at least: interface decl, `typedBase` field, option func, method impl, Read call, plus one assertion per tool (≥ 9).

### Codebase index

- [ ] Update `thoughts/shared/index/agent.md` — add a sentence about `execParallelGroup` / `parallelToolSem` under "How It Works" and mention the dispatch is now group-and-preserve.
- [ ] Update `thoughts/shared/index/tools.md` — note `Tool.ParallelSafe() bool` and that `Read` opts in.

### Final commit

> `docs(index): record parallel tool dispatch`
>
> Reflect the new dispatch algorithm and the `ParallelSafe` capability in the domain index files.

---

## Rollback Plan

Each phase is independently revertable via `git revert`. The highest-risk phase is 7 (dispatch wiring). If a regression lands on `main`, reverting 7 alone restores serial behavior while keeping the `ParallelSafe` interface, dead-code cleanup, and panic-recovery tightening in place.

## Out of Scope

- Bash / Edit / Write parallelism (design doc non-goal).
- Per-tool timeouts.
- Config knob for semaphore size.
- Telemetry / metrics.
- Approval-flow batching (no parallel tool currently requires approval).
