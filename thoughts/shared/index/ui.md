# Domain: UI
> Last updated: 2026-04-24

## Key Files
- `internal/tui/app.go` — Bubbletea `Model` struct, `Options` (including `SystemResolverFn`), `New` constructor
- `internal/tui/provider_cmd.go` — `switchProvider`, `applyModelSpec`; each call recomputes the system prompt via `sysResolveFn` and calls `Agent.SetSystem`
- `internal/tui/settings_modal.go` — Settings modal (provider/theme/skills tabs); `applyProviders` also calls `SetSystem` on provider change
- `internal/tui/renderer.go` — Terminal output and view management
- `internal/logging/` — Ring buffer for debug overlay display

## How It Works
The TUI renders a multi-modal interface: conversation view (input + history), settings modal (provider/theme/skills tabs), and debug overlay. Input accepts `Enter` to submit, `Shift+Enter` for newlines, `Ctrl+L` toggles debug view. Skill commands are parsed and dispatched via `/slash-command` syntax.

The `Model` holds a `sysResolveFn func(model string) string` closure supplied via `Options.SystemResolverFn`. Every model-change path — `/model` (`applyModelSpec`), `/provider` (`switchProvider`), and the settings modal save (`applyProviders`) — invokes the resolver and pushes the new prompt into the agent with `Agent.SetSystem`, keeping per-family prompt addenda in sync with the active model.

## Where to Look
`internal/tui/app.go` for the `Model` + `Options` shape. `internal/tui/provider_cmd.go` and `settings_modal.go` for the three `SetSystem` call sites. `internal/tui/model_switch_system_test.go` verifies the plumbing end-to-end against a fake provider. README.md lists keybindings and `/command` summaries.
