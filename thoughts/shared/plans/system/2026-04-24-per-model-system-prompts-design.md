# Per-Model System Prompts — Design

> Status: draft · 2026-04-24
> Extends the `internal/system/` externalization work from 2026-04-23.

## Problem

Today SAM has one system prompt: `internal/system/defaults/system-prompt.md`, seeded to `~/.sam/system/system-prompt.md` on first run, overridable via `cfg.SystemPromptFile` and `--system-prompt-file`. The prompt is model-agnostic. But each model family we ship a preset for has distinct characteristics (signed thinking blocks on Claude, inline `<think>` on DeepSeek, reasoning budgets on MiniMax, OpenAI vs Anthropic wire behavior). A single universal prompt either leaves those capabilities untapped or bloats with conditionals.

## Goal

Ship a default prompt that covers unknown models, plus a layered per-family addendum for each configured model family. Keep the override knobs backward-compatible. Make it trivial to add future families (e.g., qwen via OpenRouter or LM Studio) without code changes.

## Non-goals

- Per-(provider, model) keying. One prompt per family, matched purely off the model name string.
- Conditional composition ("append A if thinking enabled, B otherwise").
- Auto-scaffolded stub files when a user declares a new family in config.

## Design

### Precedence (full resolution order)

```
Highest priority:
  1. CLI --system-prompt-file / config system_prompt_file  → TOTAL override, no family append
  2. Disk base:     ~/.sam/system/system-prompt.md         → base layer
  3. Embedded base: defaults/system-prompt.md              → base fallback

Layered on top (only when (1) is not set):
  4. Disk family:     ~/.sam/system/prompts/<family>.md    → append
  5. Embedded family: defaults/prompts/<family>.md         → append fallback

Family resolution:
  - Walk configured prompt_families map (user + bundled defaults merged).
  - Find entries whose any prefix matches cfg.Model.
  - Pick family with LONGEST matching prefix. Tie → lexicographic family name asc.
  - No match → base only.

Join format:
  TrimRight(base) + "\n\n" + TrimRight(family).
  If family file is empty/missing, no join, base unchanged.
```

### On-disk layout

```
~/.sam/system/
├── system-prompt.md           # base, existing
├── tools/                     # existing
│   ├── read.md
│   ├── write.md
│   ├── edit.md
│   └── bash.md
├── prompts/                   # NEW
│   ├── claude.md
│   ├── minimax.md
│   ├── kimi-k2.md
│   ├── trinity.md
│   ├── gpt.md
│   └── deepseek.md
└── .seed-manifest.json        # existing
```

### Embedded defaults

```
internal/system/defaults/
├── system-prompt.md           # unchanged
├── tools/*.md                 # unchanged
└── prompts/                   # NEW
    ├── claude.md
    ├── minimax.md
    ├── kimi-k2.md
    ├── trinity.md
    ├── gpt.md
    └── deepseek.md
```

Content guidance per file (tailored, concise; universal rules stay in base):

- `claude.md` — signed thinking-block round-trip, `ContentThinking` awareness, 200k context so less pruning needed.
- `minimax.md` — extended thinking budget default 32k, Anthropic wire, 1M window so do not prune aggressively.
- `kimi-k2.md` — `reasoning_effort=high` preset, 262k window, OpenAI wire, no `parse_think_tags`.
- `trinity.md` — reasoning surfaced via `reasoning_content`, 512k window, OpenAI wire.
- `gpt.md` — no inline thinking blocks, 128k–400k window by model, OpenAI chat conventions.
- `deepseek.md` — inline `<think>` tags (`parse_think_tags`), 131k window, OpenAI wire.

### Config surface

`internal/config/config.go`:

```go
type PromptFamily struct {
    Prefixes []string `toml:"prefixes"`
}

type Config struct {
    // ... existing fields ...
    PromptFamilies map[string]PromptFamily `toml:"prompt_families"`
}
```

**Three edits required in `config.go`** (parallels the existing `Providers` handling — do not miss any of them, or the TOML key is silently dropped):

1. Add the field to `Config` (above).
2. Add the **same** field with the same TOML tag to the `rawConfig` struct at `internal/config/config.go:127` — TOML unmarshal targets `rawConfig`, not `Config`.
3. In `Load` initializer (around line 142), seed the map:
   ```go
   cfg := &Config{
       // ... existing fields ...
       Providers:      Presets(),
       PromptFamilies: DefaultPromptFamilies(),
   }
   ```
4. After the existing `Providers` merge loop (around line 183), add a parallel loop:
   ```go
   for name, fam := range raw.PromptFamilies {
       cfg.PromptFamilies[name] = fam
   }
   ```

Bundled defaults in new file `internal/config/prompt_families.go`:

```go
func DefaultPromptFamilies() map[string]PromptFamily {
    return map[string]PromptFamily{
        "claude":   {Prefixes: []string{"claude-opus", "claude-sonnet", "claude-haiku"}},
        "minimax":  {Prefixes: []string{"MiniMax-"}},
        "kimi-k2":  {Prefixes: []string{"kimi-k2"}},
        "trinity":  {Prefixes: []string{"trinity-"}},
        "gpt":      {Prefixes: []string{"gpt-5", "gpt-4o", "gpt-4.1", "o1", "o3", "o4"}},
        "deepseek": {Prefixes: []string{"deepseek-"}},
    }
}
```

Merge rule (mirrors existing `Providers` pattern): bundled defaults seed the map; any `[prompt_families.<name>]` in user TOML fully replaces that entry (no per-field merge). Users add new families freely; disable a bundled family with `prefixes = []`.

**Prefix matching is case-sensitive**, matching the existing `strings.HasPrefix` behavior in `ModelContextWindow`. Users needing case variants declare them explicitly in `prefixes` (the `qwen` example already does: `["qwen-", "Qwen", "openrouter/qwen/"]`).

Example user TOML:

```toml
[prompt_families.claude]
prefixes = ["claude-opus", "claude-sonnet", "claude-haiku", "claude-5"]

[prompt_families.qwen]
prefixes = ["qwen-", "Qwen", "openrouter/qwen/"]
```

### Resolver API

New in `internal/system/family.go`:

```go
// LoadFamilyPrompt reads dir/prompts/<family>.md; returns "" if missing.
// Trailing whitespace trimmed.
func LoadFamilyPrompt(dir, family string) (string, error)

// FamilyPromptExists reports whether dir/prompts/<family>.md is present on
// disk (regardless of content). Lets the resolver distinguish "user wiped
// the file" (empty file present → no append) from "file missing" (fall
// through to embedded).
func FamilyPromptExists(dir, family string) bool

// EmbeddedFamilyPrompt reads defaults/prompts/<family>.md; returns "" if missing.
func EmbeddedFamilyPrompt(family string) string
```

Embed directive extended in `internal/system/system.go`:

```go
//go:embed defaults/system-prompt.md defaults/tools/*.md defaults/prompts/*.md
var defaultsFS embed.FS
```

New method on `*config.Config`:

```go
// FamilyForModel returns the family name whose longest prefix matches model,
// using the merged bundled + user PromptFamilies map. Empty when no match.
// Families with len(Prefixes) == 0 are skipped (enables disable-via-empty).
func (c *Config) FamilyForModel(model string) string
```

Semantic pin: `len(Prefixes) == 0` means "skip this family entirely" — not "match everything". That is what enables the disable-by-replace flow documented in the config section. No logging inside this method; the `config` package stays logger-free (zero slog imports today). All observability fires from the resolver in `main.go`.

Top-level resolver in `cmd/sam/main.go` (composes across `config` + `system`, stays out of both packages):

```go
func resolveSystemPrompt(cfg *config.Config, sysDir, model string) string {
    // Total-override short-circuit (preserves existing semantics).
    if cfg.SystemPromptFile != "" {
        if s := cfg.LoadSystemPrompt(""); s != "" {
            return s
        }
    }
    base, _ := system.LoadSystemPrompt(sysDir)
    if base == "" {
        base = system.EmbeddedPrompt()
    }
    family := cfg.FamilyForModel(model)
    if family == "" {
        return base
    }
    var add string
    if system.FamilyPromptExists(sysDir, family) {
        // Disk file present — use it verbatim. Empty file means the user
        // explicitly wiped this family; do NOT fall back to embedded.
        add, _ = system.LoadFamilyPrompt(sysDir, family)
    } else {
        add = system.EmbeddedFamilyPrompt(family)
    }
    if add == "" {
        return base
    }
    return strings.TrimRight(base, "\n\t ") + "\n\n" + strings.TrimRight(add, "\n\t ")
}
```

**Empty-vs-missing semantics** (important): a present-but-empty `prompts/<family>.md` is an explicit user wipe → no family addendum, base only. A missing file falls through to the embedded content. This makes "remove this family's addendum for my setup" a simple `: > ~/.sam/system/prompts/claude.md` without needing to edit config.

### Agent wiring

`internal/agent/agent.go`:

The agent stores the user-facing prompt in `baseSystem` and the effective prompt (base + skill catalog) in `system`. `RebuildSkillCatalog` overwrites `system` from `baseSystem` on every skill-registry or auto-invoke-gate event — writing only to `system` would be silently undone on the next rebuild. `SetSystem` must update `baseSystem` and re-derive `system` via the existing rebuild path.

```go
// SetSystem swaps the base system prompt and rebuilds the effective prompt
// (base + skill catalog when auto-invoke is on) for subsequent turns.
func (a *Agent) SetSystem(s string) {
    a.mu.Lock()
    a.baseSystem = s
    a.mu.Unlock()   // unlock BEFORE RebuildSkillCatalog — it reacquires a.mu
    a.RebuildSkillCatalog()
}
```

### Call sites that recompute the prompt

- `cmd/sam/main.go::runTUI` and `::runAgentOneShot` — compute at startup, pass into `agent.Options.System`.
- `internal/tui/provider_cmd.go::switchProvider` (line 47 area, after `a.SetProvider(p)` + `a.SetModel(model)`) — call `a.SetSystem(resolver(model))`.
- `internal/tui/provider_cmd.go::applyModelSpec` (line 23, same-provider branch that only calls `SetModel`) — call `a.SetSystem(resolver(model))`. This is a third, distinct model-change path separate from `switchProvider`.
- `internal/tui/settings_modal.go` (around line 382, model change via settings) — same call.

Note: `saveAuthKey` at `provider_cmd.go:124` also rebuilds the provider but keeps (provider, model) identical — no family change, no `SetSystem` call needed there.

To avoid leaking `config` + `system` packages into `internal/tui`, extend `tui.Options` with:

```go
type Options struct {
    // ... existing fields ...
    SystemResolverFn func(model string) string
}
```

Wired from `main.go` closure: `func(m string) string { return resolveSystemPrompt(cfg, sysDir, m) }`. Mirrors the existing `ContextWindowFn` pattern.

### Seeding & upgrade behavior

- `fs.WalkDir` in existing `Seed` already recurses subdirs (handles `tools/`). `prompts/` seeds automatically — **zero `Seed` code change required**.
- Manifest tracks per-file hashes. Adding a new family file in a future release seeds on next run if disk copy is absent or matches prior embedded hash; edited files preserved, "new default available" logged (existing behavior).
- User deleting `prompts/<family>.md` → falls through to embedded (same pattern as base prompt today).
- User declaring a family in config without a content file → base-only, logged info line, no error.

## Extensibility

**Add a new family at runtime (no rebuild)**:

1. Drop `~/.sam/system/prompts/qwen.md`.
2. Add to user `config.toml`:
   ```toml
   [prompt_families.qwen]
   prefixes = ["qwen-", "Qwen", "openrouter/qwen/", "lmstudio-community/qwen"]
   ```
3. Point SAM at a qwen model — family matches by prefix, prompt appended.

**Add a new family as a bundled default (future release)**:

1. Add entry to `DefaultPromptFamilies()`.
2. Add `internal/system/defaults/prompts/<family>.md`.
3. Ship. Embed directive + existing `Seed` handle the rest.

**Extensibility guarantees baked in**:

- Family matching is pure `cfg.Model` prefix match, decoupled from provider preset. The same qwen family applies regardless of which OpenAI-wire provider is serving it.
- Multi-prefix per family covers naming drift across aggregators.
- Full-replace merge on `[prompt_families.<name>]` lets users redefine bundled families wholesale (matches existing `[providers.<name>]` semantics).
- User disk file > embedded file at the same family name (mirrors base prompt precedence).
- `prefixes = []` disables a bundled family without needing a dedicated `disabled` flag.

## Observability

- `logger.Info("system: family resolved", "model", m, "family", f, "prefix_match", p)` — fires from the resolver in `cmd/sam/main.go` (not from `FamilyForModel`). Triggers at startup (both TUI and one-shot paths) and on every TUI model switch. In one-shot mode this fires exactly once; there is no mid-session switch.
- `logger.Debug("system: family declared without content", "family", f)` — fires when a configured family has neither disk nor embedded content. Debug level (not Info) because users following the "declare in TOML now, drop content file later" extension flow would otherwise see noisy warnings every startup.
- TUI debug overlay: show active family alongside active model/provider. Updates on every model switch via the same resolver call.

## Testing

Mirror existing `internal/system` and `internal/config` test patterns. No shell-out; disk I/O against `t.TempDir()`.

### `internal/system`

- `TestEmbeddedFamilyPrompt_NonEmpty` — one subtest per bundled family (claude, minimax, kimi-k2, trinity, gpt, deepseek).
- `TestEmbeddedFamilyPrompt_Missing` — unknown family name returns "".
- `TestLoadFamilyPrompt_MissingAndPresent` — empty dir returns "", after write returns content verbatim (with trailing whitespace trimmed to match base behavior).
- `TestSeed_CreatesPromptsSubdir` — fresh temp dir + `Seed` creates `prompts/` with all bundled files and manifest hashes. Primary guard.
- `TestSeed_PreservesEditedFamilyFile` — seed, edit `prompts/claude.md`, re-seed with new embedded content simulated → edit kept, log entry emitted. Kept as explicit regression guard in case a future change special-cases the `prompts/` subdir (e.g., migration logic); current `fs.WalkDir` logic makes this redundant with existing `tools/` coverage today.

### `internal/config`

- `TestFamilyForModel_LongestPrefix` — with `kimi` and `kimi-k2` both configured, model `kimi-k2.6` resolves to `kimi-k2`.
- `TestFamilyForModel_TieBreak` — identical prefix length across two families → lexicographic family name asc.
- `TestFamilyForModel_NoMatch` — unknown model returns "".
- `TestFamilyForModel_BundledDefaults` — each bundled preset's `DefaultModel` resolves to its expected family. Explicit cases (sub-tests, one per preset): `MiniMax-M2.7` → `minimax`, `claude-sonnet-4-5` → `claude`, `kimi-k2.6` → `kimi-k2`, `trinity-large-thinking` → `trinity`, `gpt-4o-mini` → `gpt`. Locking every preset individually guards against a future preset rename silently breaking family resolution.
- `TestFamilyForModel_EmptyPrefixesSkipped` — a family with `Prefixes: []` never matches, even for the empty string model.
- `TestFamilyForModel_CaseSensitive` — `Qwen-72B` does not match a family with `prefixes = ["qwen-"]`, documenting the case-sensitive contract.
- `TestPromptFamilies_ConfigReplace` — user `[prompt_families.claude] prefixes = [...]` fully replaces bundled prefixes.
- `TestPromptFamilies_ConfigDisable` — user `prefixes = []` disables a bundled family (no prior match now resolves to it).

### Resolver (integration)

- `TestResolveSystemPrompt_OverrideTotal` — `cfg.SystemPromptFile` set → contents used verbatim, no family append, even when model matches a family.
- `TestResolveSystemPrompt_BaseOnly_UnknownModel` — model `random-model-x` → base only.
- `TestResolveSystemPrompt_BaseEmbeddedPlusFamilyDisk` — no seed yet, disk has only `prompts/claude.md` → base from embedded + family from disk.
- `TestResolveSystemPrompt_BaseDiskPlusFamilyEmbedded` — seed default, model matches family with no disk file → base from disk + family from embedded.
- `TestResolveSystemPrompt_EmptyFamilyFile` — family file present but empty → base only, no trailing `\n\n`.
- `TestResolveSystemPrompt_JoinFormat` — exactly one blank line between base and family, no trailing whitespace on either side.

### Agent

- `TestSetSystem_UpdatesNextTurn` — `SetSystem` mid-session, fake provider captures the new system string on next `Submit`.

### TUI

- `TestModelSwitch_UpdatesSystemPrompt` — switch model via `switchProvider`, verify `a.System` changed and matches expected family resolution. Uses `fake.Provider` + stub `SystemResolverFn`.

## Migration & compatibility

- `system_prompt_file` in TOML and `--system-prompt-file` CLI flag: semantics unchanged — total override, no family append.
- Existing `~/.sam/system/system-prompt.md` on upgrade: untouched. `prompts/` subdir newly created and populated by `Seed`.
- No config-file breaking change; `prompt_families` is additive and optional.
- Documentation update in `README.md`: new section on per-family prompts with the full precedence chain and the qwen extension example.

## File inventory

**New:**
- `internal/system/family.go`
- `internal/system/defaults/prompts/claude.md`
- `internal/system/defaults/prompts/minimax.md`
- `internal/system/defaults/prompts/kimi-k2.md`
- `internal/system/defaults/prompts/trinity.md`
- `internal/system/defaults/prompts/gpt.md`
- `internal/system/defaults/prompts/deepseek.md`
- `internal/config/prompt_families.go`
- Test files mirroring the above.

**Build-time constraint**: all six `defaults/prompts/*.md` files must land in the **same change-set** as the extended `//go:embed defaults/prompts/*.md` directive. Go's `//go:embed` with a glob fails at build time if it matches zero files. Do not split the embed directive edit from the content files across commits.

**Modified:**
- `internal/system/system.go` — extend `//go:embed` directive.
- `internal/config/config.go` — add `PromptFamilies` field, merge bundled defaults, add `FamilyForModel`.
- `internal/agent/agent.go` — add `SetSystem`.
- `internal/tui/app.go` / `internal/tui/forms.go` — add `SystemResolverFn` to `Options` and model.
- `internal/tui/provider_cmd.go` — call `SetSystem` after `SetProvider` in `switchProvider`.
- `internal/tui/settings_modal.go` — same call on model-change path.
- `cmd/sam/main.go` — add `resolveSystemPrompt`, thread resolver into TUI options, use in both TUI and one-shot paths.
- `README.md` — document per-family prompts + extensibility.
