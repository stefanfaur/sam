# Domain: Config
> Last updated: 2026-04-24

## Key Files
- `internal/config/config.go` — TOML parsing and defaults (Config struct, rawConfig, Load, merge loops)
- `internal/config/secrets.go` — Secrets.toml handling and env var injection
- `internal/config/prompt_families.go` — `PromptFamily` type, `DefaultPromptFamilies`, `FamilyForModel` longest-prefix resolver
- `cmd/sam/main.go` — Config file discovery (XDG paths) and provider wiring

## How It Works
SAM reads config.toml from `~/.config/sam/` (respects XDG_CONFIG_HOME), parses provider presets, model defaults, TUI theme, per-model overrides, and prompt-family prefix tables. Secrets are loaded from secrets.toml and injected into env vars. Providers and prompt-families can be overridden (full replacement per key, no field-by-field merge) in user config. CLI flags take highest priority.

`Config.PromptFamilies` merges bundled defaults (claude, minimax, kimi-k2, trinity, gpt, deepseek) with user-declared `[prompt_families.<name>]` entries; an empty `prefixes = []` disables a bundled family. `FamilyForModel` walks the merged map and returns the family whose longest prefix matches the model name (lex-asc tiebreak on length).

Per-model caps live on `ModelConfig` (`ContextWindow`, `ReasoningEffort`, `ThinkingBudgetTokens`, `MaxTokens`) and are resolved by three helpers on `*Config`: `ModelContextWindow`, `ModelThinkingBudget`, `ModelMaxTokens`. Each prefers explicit user config over family defaults; `ModelMaxTokens` returns 0 when no family default exists so callers fall back to the global `MaxTokens`. The agent calls `ModelMaxTokens` via `Options.MaxTokensResolverFn` per turn, so live provider/model switches pick up the right cap without rebuilding the agent.

## Where to Look
`internal/config/config.go` for the Config struct, TOML merge loops, and provider validation. `internal/config/prompt_families.go` for family resolution. `cmd/sam/main.go` shows file loading and env var override flow. README.md has complete config.toml, secrets.toml, and prompt-family examples.
