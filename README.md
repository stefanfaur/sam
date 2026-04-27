# SAM — Coding Agent

SAM is a CLI coding agent built in Go with a Bubbletea TUI, pluggable providers
(Anthropic-compatible + OpenAI-compatible wires), core tools (Read/Write/Edit/Bash),
per-tool approval policy, and in-TUI debug overlay.

## Installation

```bash
go install ./cmd/sam@latest
```

Or build from source:

```bash
git clone https://github.com/stefanfaur/sam.git
cd sam
go build -o sam ./cmd/sam
```

## Configuration

SAM reads configuration from `~/.config/sam/config.toml` (or `$XDG_CONFIG_HOME/sam/config.toml`).

### Providers

SAM ships six bundled provider presets — `minimax`, `anthropic`, `openai`,
`arcee`, `moonshot`, `deepseek` — and accepts arbitrary user-defined entries
keyed by name. Each entry declares its `wire` (`anthropic` or `openai`) plus
the env var holding its API key.

**DeepSeek v4:** 1M context, 192k `max_tokens`, 120k thinking budget by
default. Override per-model under `[models.<name>]` (e.g.
`max_tokens = 64000`).

### Example config.toml

```toml
provider = "openai"                 # bundled preset or user-defined key
model    = "gpt-4o-mini"            # bare or "provider/model"
max_tokens     = 4096
max_iterations = 25

# The four built-in presets can be overridden by redeclaring the key.
# Any redeclaration REPLACES the preset entry in full (no field-by-field merge).
[providers.openai]
wire          = "openai"
base_url      = "https://api.openai.com/v1"
api_key_env   = "OPENAI_API_KEY"
default_model = "gpt-4o-mini"

[providers.arcee]
wire             = "openai"
base_url         = "https://api.arcee.ai/api/v1"
api_key_env      = "ARCEE_API_KEY"
default_model    = "trinity-large-thinking"
parse_think_tags = false            # flip to true for self-hosted reasoning
                                    # servers without a reasoning-parser

[providers.moonshot]
wire          = "openai"
base_url      = "https://api.moonshot.ai/v1"
api_key_env   = "KIMI_API_KEY"
default_model = "kimi-k2.6"         # kimi-k2.5 also works; both 262k ctx

# User-defined (not a preset) — appends to the provider map.
[providers.groq]
wire          = "openai"
base_url      = "https://api.groq.com/openai/v1"
api_key_env   = "GROQ_API_KEY"
default_model = "llama-3.3-70b-versatile"

# Per-entry capability overrides (uncommon; defaults from the prefix table
# at internal/llm/openaicompat/caps.go usually suffice).
[providers.openai.caps]
system_role               = "developer"
max_tokens_field          = "max_completion_tokens"
supports_sampling_params  = false
supports_reasoning_effort = true

# Per-model overrides. The reasoning_effort field feeds the OpenAI-wire
# reasoning_effort request parameter for gpt-5 / o-series models.
[models."gpt-5"]
context_window   = 400000
reasoning_effort = "medium"

[models."trinity-large-thinking"]
context_window = 512000

[tui]
theme = "dark"
```

### secrets.toml

API keys are kept in `~/.config/sam/secrets.toml`, keyed by provider name:

```toml
[api_keys]
minimax   = "..."
anthropic = "..."
openai    = "sk-..."
arcee     = "..."
moonshot  = "sk-..."
deepseek  = "sk-..."
```

On startup each entry's `api_key_env` is set from `[api_keys].<name>` unless the
env var is already set in the environment.

### Environment Variables

| Variable | Description |
|----------|-------------|
| `MINIMAX_API_KEY`    | Minimax preset |
| `ANTHROPIC_API_KEY`  | Anthropic preset |
| `OPENAI_API_KEY`     | OpenAI preset (gpt-4o, gpt-5, o-series) |
| `ARCEE_API_KEY`      | Arcee Conductor preset (trinity-large-thinking) |
| `KIMI_API_KEY`       | Moonshot AI preset (kimi-k2.6, kimi-k2.5) |
| `DEEPSEEK_API_KEY`   | DeepSeek preset (deepseek-v4-pro, deepseek-v4-flash) |
| `SAM_PROVIDER`       | Override default provider (accepts any preset or user-defined key) |
| `SAM_MODEL`          | Override default model (bare or `provider/model`) |
| `XDG_CONFIG_HOME`    | Override config directory |

### System prompt and tool descriptions

On first run SAM seeds `~/.sam/system/` with the built-in defaults:

```
~/.sam/system/
  system-prompt.md
  tools/
    read.md
    write.md
    edit.md
    bash.md
  .seed-manifest.json
```

Edit any file to customize SAM's behavior. User edits are preserved across
upgrades — the manifest tracks sha256 hashes so new defaults only overwrite
files you have not touched. Delete a file (and its entry in
`.seed-manifest.json`) to force a re-seed from the embedded default.

System prompt precedence (first non-empty wins):

1. `--system-prompt <file>` CLI flag
2. `system_prompt_file` in `config.toml`
3. `~/.sam/system/system-prompt.md`
4. Built-in embedded default

Tool descriptions follow `~/.sam/system/tools/<name>.md` → embedded default.

Set `$SAM_HOME` to relocate the SAM root (default `~/.sam`); the system
directory always lives under it.

## Per-Model System Prompts

SAM ships a universal base prompt plus per-family addenda tailored to each
model's capabilities. The family is resolved from the current model name via
longest-prefix match and the addendum is appended to the base prompt on every
model switch.

### Precedence

1. `--system-prompt <file>` / `system_prompt_file` in `config.toml` — total
   override; family addendum is skipped entirely.
2. Disk base `~/.sam/system/system-prompt.md` (seeded on first run).
3. Embedded base.

When the override is not set, the matching family addendum is appended:

4. Disk family `~/.sam/system/prompts/<family>.md`.
5. Embedded family.

An empty disk family file is treated as an explicit user wipe — no addendum is
appended and SAM does not fall back to the embedded family content.

### Bundled Families

| Family     | Prefixes                                                  |
|------------|-----------------------------------------------------------|
| `claude`   | `claude-opus`, `claude-sonnet`, `claude-haiku`            |
| `minimax`  | `MiniMax-`                                                |
| `kimi-k2`  | `kimi-k2`                                                 |
| `trinity`  | `trinity-`                                                |
| `gpt`      | `gpt-5`, `gpt-4o`, `gpt-4.1`, `o1`, `o3`, `o4`            |
| `deepseek` | `deepseek-`                                               |

Longest prefix wins on overlap; ties break lexicographically by family name.
Prefix match is case-sensitive — declare case variants explicitly.

### Adding a Custom Family

Declare the family prefixes in `config.toml` and drop the addendum file on disk:

```toml
# ~/.config/sam/config.toml
[prompt_families.qwen]
prefixes = ["qwen-", "Qwen", "openrouter/qwen/", "lmstudio-community/qwen"]
```

Write the content at `~/.sam/system/prompts/qwen.md`. Point SAM at a qwen model
— the family addendum auto-applies on the next model switch.

Override a bundled family (full replacement of its prefix list):

```toml
[prompt_families.claude]
prefixes = ["claude-opus", "claude-sonnet", "claude-haiku", "claude-5"]
```

Disable a bundled family:

```toml
[prompt_families.claude]
prefixes = []
```

### Migration from pre-map config

The nested `[providers.minimax]` / `[providers.anthropic]` blocks are still
accepted when they use the new `wire = ...` + `api_key_env = ...` shape. The
legacy `[secrets]` fields `minimax_api_key` / `anthropic_api_key` are gone —
move keys to the `[api_keys]` map shown above.

## Usage

### CLI flags

```
sam [-p <prompt>] [--provider <name>] [--model <spec>] [--system-prompt <file>]
```

`--model` accepts either a bare model name (resolved via the provider map's
default-model, `models = [...]`, and `model_prefixes = [...]` fields, plus the
hardcoded OpenAI prefix table) or an explicit `provider/model` form.

### One-shot mode

```bash
OPENAI_API_KEY=sk-... sam -p "read /path/to/file.go and summarize" --model openai/gpt-4o-mini
```

### Interactive mode

```bash
OPENAI_API_KEY=sk-... sam
```

## Tools

- **Read** — Read file contents with line numbers (default: Allow). Supports optional `raw: true` to skip rtk compression and return exact lines (use for precise editing).
- **Write** — Write files (default: Ask, requires Read first)
- **Edit** — Edit files with exact string replacement (default: Ask, requires Read first)
- **Bash** — Execute shell commands (default: Ask). Supports optional `raw: true` to skip rtk compression and run the command verbatim (use when output bytes matter, e.g. applying diffs).

## RTK Integration

SAM integrates [rtk](https://github.com/rtk-ai/rtk) (Rust Token Killer) as a
transparent compression layer for the `Bash` and `Read` tools. When rtk is on
PATH, Bash commands flow through `rtk rewrite <cmd>` and full-file Reads flow
through `rtk read --level minimal -n <path>`, cutting token usage on common dev
operations without changing what the agent sees logically.

### Configuration

```toml
[rtk]
mode = "auto"  # "auto" (default) | "on" | "off"
```

### UX block

```toml
[ux]
checkpoint_enabled    = true   # default; set false to disable Esc-Esc rewind
esc_double_window_ms  = 500    # ms within which two Esc presses chord
```

`checkpoint_enabled = false` keeps SAM from writing any `refs/sam/checkpoints/`
refs and disables the rewind picker even inside a git repo.
`esc_double_window_ms` tunes both the abort chord (during a turn) and the
rewind-open chord (idle).

- `mode = "auto"` — use rtk if the binary is on PATH; otherwise fall through
  to the native, uncompressed paths. No startup error.
- `mode = "on"` — rtk must be installed; startup fails if not.
- `mode = "off"` — rtk disabled entirely; no PATH probe.

Unknown values clamp to `auto`.

### Per-tool escape hatch

Both tools accept an optional `raw: true` to bypass rtk when exact bytes matter:

```json
{"command": "git diff HEAD~1", "raw": true}
{"file_path": "/abs/path/to/file", "raw": true}
```

`Read` also bypasses rtk automatically when `offset` or `limit` is set, since
windowed reads need precise line numbering.

### Approval and display

- **Approval policy matches the original command**, not the rewritten form.
  Allowlists keep working; a rewrite can't sneak past an `Ask` rule.
- **TUI** shows the original command as the primary line and, when a rewrite
  occurred, the rewritten form on a dimmed subline prefixed with `↳` so you
  always see what actually executed.
- **Cap:** rtk read output is capped at 1 MiB; oversize responses return an
  error that hints the agent to retry with `raw: true`.

### Installing rtk

rtk is optional; when missing and `mode = "auto"`, SAM just uses native paths.
See [rtk's repo](https://github.com/rtk-ai/rtk) for install instructions, then
verify with `rtk --version`.

## Keybindings

| Key | Action |
|-----|--------|
| `Enter` | Submit prompt |
| `Shift+Enter` | Newline in input |
| `Ctrl+C` | Cancel current turn; double-tap within 500ms to quit |
| `Ctrl+D` | Quit (when input empty) |
| `Ctrl+L` | Toggle debug overlay |
| `Ctrl+V` | Attach binary image from clipboard |
| `Esc` `Esc` (idle) | Open the rewind picker (time-travel through prior turns) |
| `Ctrl+Z` (idle) | Undo the most recent rewind (one-slot buffer) |
| `@` | Open file picker (fuzzy filter) |
| `Esc` (in picker) | Close picker |
| `Tab` / `Enter` (in picker) | Insert `@<path>` reference |

### File and image references

- **`@file` picker** — typing `@` at a token boundary opens an inline picker
  sourced from `git ls-files`, falling back to a recursive walk that skips
  hidden directories, `node_modules`, `vendor`, `dist`, `build`, and `target`.
  Up/Down moves the cursor, Tab or Enter inserts `@<path> ` into the input,
  Esc closes the overlay.
- **`Ctrl+V` clipboard image** — reads a binary image from the system
  clipboard, runs the preprocess pipeline (resize long-edge to ≤1568px with
  Lanczos, format-select PNG/JPEG, strip EXIF/ICC), and queues it as an
  attachment on the next user message. Cmd+V remains the terminal's bracketed
  paste; for binary image attach, use Ctrl+V explicitly.
- **`@image:/path/to/file.png`** — inline syntax. The token is stripped from
  the outgoing text and the file is loaded, preprocessed, and attached.
  Supported extensions: `.png`, `.jpg`/`.jpeg`, `.gif`, `.webp`.
- **Vision gating** — attaches are refused with a refusal toast listing
  vision-capable models when the active provider/model doesn't accept image
  content. Anthropic Claude 3.x/4.x and OpenAI gpt-4o, gpt-4-turbo, gpt-4.1
  pass the gate.
- **Token estimate badge** — when attachments are queued, a one-line badge
  renders above the input box: count, total token estimate (Anthropic
  `(w*h)/750`; OpenAI tile-based `tilesW*tilesH*170+85`; unknown providers
  show `≈ unknown`), provider name, dimensions, and total bytes.

### Rewind (Esc-Esc time travel)

When SAM runs inside a git repository, every gracefully-completed agent turn
writes a shadow git commit under `refs/sam/checkpoints/<session-id>/<turn>`.
These refs never touch your `HEAD`, current branch, working tree, or index —
they are pure objects in `.git/objects` reachable only through the reserved
namespace.

- **Esc-Esc (idle)** opens the picker: a list of completed turns with
  user-message preview, tool calls used, and a `⚠` glyph on turns that
  invoked `Bash` (Bash side-effects cannot be rolled back).
- **Enter** in the picker opens the restore-confirm modal where you choose
  `[c]` (rewind conversation only) or `[b]` (rewind conversation **and**
  restore working-tree files the agent touched). Files you edited by hand
  after the agent's last turn are flagged `! path (modified by you)` and
  excluded from restore unless you explicitly toggle `o` to include them.
- **Ctrl+Z (idle)** or **`/unrewind`** undoes the last rewind. The undo slot
  holds exactly one rewind and is cleared on the next user submission.

Snapshots are best-effort and never break the live agent flow: if go-git
errors during snapshot the turn still succeeds, just without a rewind point.
Cancelled or errored turns never produce a snapshot — rewind targets always
represent stable end-of-turn states.

The reserved ref namespace `refs/sam/checkpoints/` should not be used for
any other purpose. SAM scrubs every ref it created at clean exit, and on
startup sweeps refs whose session sidecar (`$XDG_STATE_HOME/sam/sessions/<id>.json`)
is missing or older than 7 days.

When SAM is launched outside a git repo, rewind is silently disabled —
Esc-Esc surfaces "rewind unavailable: not in a git repo" instead.

### CGO requirement

Binary clipboard image read uses `golang.design/x/clipboard`, which requires
CGO. Builds must run with `CGO_ENABLED=1` (the default on macOS/Linux dev
environments). On Linux, the X11 development headers (`libx11-dev`) are
needed at build time.

## Logs

JSON logs are written to `$XDG_STATE_HOME/sam/logs/sam-YYYY-MM-DD.log` (or
`~/.local/state/sam/logs/`) and mirrored into an in-memory ring buffer for the
debug overlay. Files older than seven days are removed on startup. HTTP requests
redact `Authorization` and `X-Api-Key` headers.

## Slash Commands

- `/exit`, `/quit` — Exit the program
- `/clear` — Clear conversation history
- `/reset` — Reset history and session allowlist
- `/model <spec>` — Switch model (`provider/model` explicit form, or bare name
  resolved across the provider map)
- `/provider <name>` — Switch provider; defaults model to the entry's preset
- `/auth [<name>]` — Without an argument: list all providers with key-set
  status. With `<name>`: prompt for an API key, persist to
  `[api_keys].<name>` in `secrets.toml`, refresh the env var, and rebuild the
  active provider if it matches.
- `/cwd` — Show current working directory
- `/settings` — Open settings modal (statusline, providers, theme, skills)
- `/reload-skills` — Re-scan skill roots for new or changed skills
- `/show-tool [N]` — Expand a previously-collapsed tool call (default: latest)
- `/show-thinking [N]` — Expand a previously-collapsed thinking block
- `/help` — Show this help

Tool calls and thinking blocks render as rounded-border cards with live
spinners; once the turn settles they collapse to compact one-line markers in
scrollback (`● Bash · 47 lines · 1.2s [/show-tool 3]`, `✧ thought for 4.2s`).
Toggle thinking stream depth (`full` vs `header`) via `/settings` →
**Statusline** → Thinking stream.

User-invocable skills also appear here as `/skill-name [args]`.

## Reasoning models

Reasoning-capable models carry their chain-of-thought back into the next turn
via a `ContentThinking` block on the assistant history:

- **OpenAI GPT-5 / o-series** — the server keeps reasoning hidden; we pass
  `reasoning_effort` from the per-model config but never echo reasoning back.
- **DeepSeek-R1, Arcee Trinity-Large-Thinking, Moonshot Kimi-K2.x, and other
  OpenAI-compat reasoning models** — reasoning arrives as
  `delta.reasoning_content` / `delta.reasoning` on the stream and is
  round-tripped into subsequent requests' assistant messages. The caps
  table routes the outbound field name (`reasoning_content` vs `reasoning`)
  per model family. Kimi-K2.x defaults to `reasoning_effort = "high"`;
  override via `[models."kimi-k2.6"] reasoning_effort = "low"`. If your
  self-hosted server emits inline `<think>…</think>` instead of
  server-parsed fields, set `parse_think_tags = true` on the provider entry.
- **Anthropic extended thinking** — not round-tripped; Anthropic requires
  cryptographically signed thinking blocks we do not carry today.

## Skills

SAM loads [agentskills.io](https://agentskills.io)-compliant skill bundles
from three roots (in priority order):

1. `$PROJECT/.sam/skills` — project-scoped, requires trust on first load.
2. `~/.sam/skills` — personal.
3. `~/.agents/skills` — tool-agnostic, shared across agents. **Trusted by
   default** — anything written here runs without a prompt, so be careful what
   you drop in.

Each skill is a directory whose `SKILL.md` starts with YAML frontmatter:

```markdown
---
name: review-pr
description: Review a GitHub PR. Use when the user asks for a PR review.
argument-hint: "[pr-number]"
---

Please review PR #$ARGUMENTS. Look for logic errors, missing tests, …
```

Invoke as `/review-pr 123`; `$ARGUMENTS` is substituted into the body and the
rendered message is sent as a normal user turn.

Manage skills via `/settings` → **Skills** tab:

- `x` toggle enabled, `a` toggle auto (model-invocable), `m` toggle manual
  (user-invocable)
- `g` toggle the global auto-invocation gate (off by default; when on, a
  compact `<available_skills>` catalog is injected into the system prompt so
  the model can pull in skills on demand)
- `t` opens the trust sub-panel for managing trusted/denied project paths
- `Ctrl+S` saves to `~/.config/sam/skills.toml`, `Esc` cancels

Reference skills live in `testdata/skills/` — copy any of them into
`~/.agents/skills/` as a starting point.

## Architecture

See `thoughts/shared/plans/providers/2026-04-22-openai-compat-design.md` and
`2026-04-23-openai-compat-implementation.md` for the provider architecture.
The foundational agent design lives in
`thoughts/shared/plans/sam/2026-04-22-foundation.md`.
