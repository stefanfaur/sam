# Domain: Tools
> Last updated: 2026-04-24

## Key Files
- `internal/tools/` — Tool implementations (Read, Write, Edit, Bash)
- `internal/tools/tool.go` — `Tool` interface, incl. `ParallelSafe() bool` capability bit
- `internal/tools/registry.go` — `typed[In]` generic wrapper, `ParallelSafe()` option, panic-to-error recovery
- `internal/policy/` — Per-tool approval rules and allowlist/denylist enforcement

## How It Works
Tools are registered by name and invoked by the agent when LLM requests them. Each tool has a Request struct for input, handles approval via policy (defaults vary: Read allows, Write/Edit/Bash ask), and returns a Result. File tools require prior Read before Write/Edit.

Every `Tool` declares `ParallelSafe() bool`. The agent's turn dispatcher groups consecutive parallel-safe calls into a concurrent batch and runs unsafe calls inline. `Read` opts in (pure IO, tracker is already mutex-guarded); `Write`/`Edit`/`Bash` stay serial. Adding `ParallelSafe()` as a functional option to `New[In](...)` is the only change needed to flip a tool's capability. A panic inside a tool body is caught in `typed.Run` and surfaces as a `Result{IsError: true}` with the panic value — never a silent empty output.

## Where to Look
Start with `internal/tools/tool.go` for the interface contract, then `registry.go` for the generic wrapper and options. Each tool file holds the concrete `NewX` constructor; only `NewRead` currently passes `ParallelSafe()`. Check `internal/policy/` for approval logic. README.md lists the default approval policies for each tool.
