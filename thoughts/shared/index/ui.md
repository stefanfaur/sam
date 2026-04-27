# Domain: UI
> Last updated: 2026-04-27

## Key Files
- `internal/tui/app.go` — Bubbletea `Model` struct, `Options` (including `SystemResolverFn`), `New` constructor; carries `picker *filePicker` and `imageAttachments []imageAttachment`
- `internal/tui/update.go` — `Update`, `View`, `handleKey`; Enter/Esc routing including the steer-queue path; `@` boundary detection opens picker; `Ctrl+V` triggers `cmdPasteImage`
- `internal/tui/file_picker.go` — `filePicker` state, `getFilesForPicker` (git ls-files → recursive walk), `fuzzyFilter` (sahilm/fuzzy), `renderFilePicker` overlay
- `internal/tui/image_attach.go` — `imageAttachment`, `attachmentBadge`, `estimateTokens` (Anthropic (w*h)/750, OpenAI tile-based), `preprocessImage` (calls into internal/image)
- `internal/tui/image_paste.go` — `cmdPasteImage` (clipboard read → vision check → preprocess → emit `imageAttachedMsg`); `visionSupported(wire, model)` dispatches to `llm.AnthropicVisionSupported` or `openaicompat.DefaultCaps`
- `internal/tui/image_ref.go` — `parseImageSyntax`, `extractImageRefs`, `loadImageFromFile`, `attachImageRefs` (used by `startTurn`)
- `internal/tui/provider_cmd.go` — `switchProvider`, `applyModelSpec`; each call recomputes the system prompt via `sysResolveFn` and calls `Agent.SetSystem`
- `internal/tui/settings_modal.go` — Settings modal (provider/theme/skills tabs); `applyProviders` also calls `SetSystem` on provider change
- `internal/tui/render.go` — Terminal output, view management, `renderQueueIndicator`
- `internal/tui/theme.go` — Lipgloss styles including `QueueIndicator`
- `internal/logging/` — Ring buffer for debug overlay display

## How It Works
The TUI renders a multi-modal interface: conversation view (input + history), settings modal (provider/theme/skills tabs), and debug overlay. Input accepts `Enter` to submit, `Shift+Enter` for newlines, `Ctrl+L` toggles debug view. Skill commands are parsed and dispatched via `/slash-command` syntax.

The `Model` holds a `sysResolveFn func(model string) string` closure supplied via `Options.SystemResolverFn`. Every model-change path — `/model` (`applyModelSpec`), `/provider` (`switchProvider`), and the settings modal save (`applyProviders`) — invokes the resolver and pushes the new prompt into the agent with `Agent.SetSystem`, keeping per-family prompt addenda in sync with the active model.

**Mid-stream steer (Enter while pending).** When `m.pending != nil`, Enter no longer submits — it trims the input and calls `m.agent.QueueSteer(text)`, then resets the textarea. Empty/whitespace input is a no-op. The drain happens agent-side at the next clean iteration boundary. Esc-Esc within `m.escDoubleWindow` aborts the turn and additionally calls `m.agent.DiscardQueue()`; single Esc cancels granularly and leaves the queue intact. `View()` shows a one-line `queued (N): item1 ⏎ item2` indicator above the input via `renderQueueIndicator` whenever a turn is live and the queue is non-empty.

**File and image references (Feature D).** `@` typed at a token boundary opens an inline file picker over `git ls-files` (or recursive walk). The picker swallows Up/Down/Tab/Enter/Esc/Backspace/runes; selection inserts `@<path> ` via `textarea.InsertString`. `Ctrl+V` triggers `cmdPasteImage`, which reads binary image bytes via `internal/clipboard`, runs the preprocess pipeline (`internal/image.ResizeImage` → `SelectFormat` → `StripMetadata`), estimates tokens for the active provider, and emits `imageAttachedMsg`. `@image:/path` tokens are extracted from the user input on submit (`extractImageRefs` + `attachImageRefs`); files are loaded, preprocessed, and added to `m.imageAttachments`. `startTurn` calls `agent.SubmitWithAttachments` with the collected attachments and clears `m.imageAttachments` for the next turn. Vision capability is checked before any attach — refusal returns a one-line toast listing capable models. The badge renders above the input box: `📎 N images · K tok (provider) · WxH, ... · sizeKB`.

## Where to Look
`internal/tui/app.go` for the `Model` + `Options` shape. `internal/tui/update.go` for the Enter/Esc handlers and the queue-indicator branch in `View()`. `internal/tui/render.go` for `renderQueueIndicator`. `internal/tui/queue_test.go` covers Enter-queues, empty-input no-op, Esc-Esc discard, single-Esc preserve, and indicator rendering. `internal/tui/provider_cmd.go` and `settings_modal.go` for the three `SetSystem` call sites. `internal/tui/model_switch_system_test.go` verifies the plumbing end-to-end against a fake provider. README.md lists keybindings and `/command` summaries.
