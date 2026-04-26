# Domain: System

> Last updated: 2026-04-26

## Key Files
- `internal/system/system.go` — Embedded FS (`//go:embed`), EmbeddedPrompt, EmbeddedToolDescription, DefaultDir
- `internal/system/family.go` — EmbeddedFamilyPrompt, LoadFamilyPrompt, FamilyPromptExists for per-family addenda under `prompts/<family>.md`
- `internal/system/seed.go` — Seed manifest (sha256), idempotent seeding of embedded defaults to `~/.sam/system/`
- `internal/system/defaults/system-prompt.md` — Universal base prompt (CAVEMAN/EVIDENCE/SCOPE/SAFETY/OUTPUT blocks; ~1.1KB, budget 1280)
- `internal/system/defaults/tools/*.md` — Per-tool descriptions
- `internal/system/defaults/prompts/*.md` — Per-family addenda, 9 files: claude, minimax, kimi-k2, trinity, gpt, gpt-reasoning, deepseek, deepseek-reasoner, deepseek-v4 (behavioral-only, no wire/context metadata)
- `docs/families/*.md` — Maintainer-facing reference per family: wire transport, context window, API field names, recommended sampling params, known upstream bugs, source links. Not seeded, not sent to any model.
- `cmd/sam/main.go::resolveSystemPrompt` — Composition pipeline: CLI/config override → base (disk > embedded) → family addendum (disk > embedded)
- `internal/config/prompt_families.go` — `PromptFamily` type, `DefaultPromptFamilies` (9 entries), `Config.FamilyForModel` (longest-prefix, lex-asc tiebreak)

## How It Works

SAM composes the system prompt at runtime from two layers: a universal base
prompt and a per-family addendum resolved from the current model name. On first
run the embedded defaults are seeded to `~/.sam/system/` (via a sha256 manifest
so user edits are preserved on upgrade). `resolveSystemPrompt` applies the
precedence chain — `cfg.SystemPromptFile` override short-circuits composition,
otherwise base (disk > embedded) is joined with the family addendum
(disk > embedded) via a single blank line. An empty disk family file is an
explicit user wipe (no fallback to embedded).

Family matching uses `Config.FamilyForModel`: longest case-sensitive prefix
wins, length ties break lexicographically by family name, families with empty
`prefixes` are skipped (enables disable-via-empty). User-defined families in
`[prompt_families.<name>]` TOML merge into the bundled defaults.

The 9 bundled families split reasoning vs non-reasoning where wire behavior
diverges: `gpt` (gpt-4o, gpt-4., gpt-4-) vs `gpt-reasoning` (gpt-5, o1, o3,
o4); `deepseek` catch-all vs `deepseek-reasoner` (R1-series) vs `deepseek-v4`
(v4-pro, v4-flash). Addenda are behavioral-only — wire/context/API-field metadata
moved to `docs/families/*.md`.

The TUI plumbs a `SystemResolverFn` closure through `tui.Options` and stores it
as `sysResolveFn` on `Model`. Every model-switch path (`switchProvider`,
`applyModelSpec`, `settings_modal.applyProviders`) calls `Agent.SetSystem`
with the re-resolved prompt so the next turn hits the provider with the
correct family addendum.

## Where to Look

- `internal/system/family.go` for the loader trio.
- `internal/config/prompt_families.go` for `FamilyForModel` resolution rules.
- `cmd/sam/main.go::resolveSystemPrompt` for the composition pipeline.
- `internal/tui/provider_cmd.go` + `internal/tui/settings_modal.go` for the three `SetSystem` call sites.
- Integration tests in `internal/system/integration_test.go` cover precedence
  and composition; `internal/tui/model_switch_system_test.go` verifies the
  plumbing reaches the provider on every switch path.
