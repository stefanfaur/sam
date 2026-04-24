# Domain: Tools
> Last updated: 2026-04-23

## Key Files
- `internal/tools/` — Tool implementations (Read, Write, Edit, Bash)
- `internal/policy/` — Per-tool approval rules and allowlist/denylist enforcement
- `internal/tools/types.go` — Tool request/response definitions

## How It Works
Tools are registered by name and invoked by the agent when LLM requests them. Each tool has a Request struct for input, handles approval via policy (defaults vary: Read allows, Write/Edit/Bash ask), and returns a Result. File tools require prior Read before Write/Edit.

## Where to Look
Start with `internal/tools/` to see the four core implementations. Check `internal/policy/` for approval logic. README.md lists the default approval policies for each tool.
