# SAM — Coding Agent

SAM is a CLI coding agent built in Go with a Bubbletea TUI, streaming Minimax (Anthropic-compatible) provider, core tools (Read/Write/Edit/Bash), per-tool approval policy, and in-TUI debug overlay.

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

### Example config.toml

```toml
provider = "minimax"          # or "anthropic"
model = "MiniMax-M2.7"
max_tokens = 4096
max_iterations = 25

[providers.minimax]
base_url = "https://api.minimax.io/anthropic"

[providers.anthropic]
base_url = ""

[tui]
theme = "dark"                # dark|light|auto
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `MINIMAX_API_KEY` | API key for Minimax provider |
| `ANTHROPIC_API_KEY` | API key for Anthropic provider |
| `SAM_PROVIDER` | Override default provider |
| `SAM_MODEL` | Override default model |
| `XDG_CONFIG_HOME` | Override config directory |

## Usage

### CLI flags

```
sam [-p <prompt>] [--provider minimax|anthropic] [--model <name>] [--system-prompt <file>]
```

### One-shot mode

```bash
SAM_PROVIDER=minimax MINIMAX_API_KEY=xxx sam -p "read /path/to/file.go and summarize"
```

### Interactive mode

```bash
SAM_PROVIDER=minimax MINIMAX_API_KEY=xxx sam
```

## Tools

- **Read** — Read file contents with line numbers (default: Allow)
- **Write** — Write files (default: Ask, requires Read first)
- **Edit** — Edit files with exact string replacement (default: Ask, requires Read first)
- **Bash** — Execute shell commands (default: Ask)

## Keybindings

| Key | Action |
|-----|--------|
| `Enter` | Submit prompt |
| `Shift+Enter` | Newline in input |
| `Ctrl+C` | Cancel current turn; double-tap within 500ms to quit |
| `Ctrl+D` | Quit (when input empty) |
| `Ctrl+L` | Toggle debug overlay |

## Logs

JSON logs are written to `$XDG_STATE_HOME/sam/logs/sam-YYYY-MM-DD.log` (or
`~/.local/state/sam/logs/`) and mirrored into an in-memory ring buffer for the
debug overlay. Files older than seven days are removed on startup. HTTP requests
redact `Authorization` and `X-Api-Key` headers.

## Slash Commands

- `/exit`, `/quit` — Exit the program
- `/clear` — Clear conversation history
- `/reset` — Reset history and session allowlist
- `/model <name>` — Set the model for subsequent turns
- `/provider <name>` — Set the provider (minimax/anthropic)
- `/cwd` — Show current working directory
- `/settings` — Open settings modal (statusline, providers, theme, skills)
- `/reload-skills` — Re-scan skill roots for new or changed skills
- `/help` — Show this help

User-invocable skills also appear here as `/skill-name [args]`.

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

See `thoughts/shared/plans/sam/2026-04-22-foundation.md` for the full implementation plan.
