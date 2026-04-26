# Skills System — Design

**Date:** 2026-04-22
**Status:** Design approved, ready for implementation planning
**Domain:** skills (cross-cuts `internal/skills/`, `internal/tui/`, `internal/agent/`, `internal/config/`)

## 1. Problem and Scope

Add a first-class skills system to sam so that:

- Users can drop model-invocable + user-invocable skill bundles into the filesystem and have sam discover, load, and expose them.
- Every user-invocable skill is reachable as a slash command (`/skill-name [args]`).
- Skills can be enabled, disabled, and selectively toggled between auto-invocation and manual-invocation through a dedicated tab in the existing settings modal.
- Skills from untrusted project directories do not silently modify agent behavior.

## 2. Standards and Prior Art

- **On-disk format:** [agentskills.io](https://agentskills.io/specification) — the open standard for SKILL.md bundles (frontmatter + body + optional `scripts/`, `references/`, `assets/`).
- **Not adopted as skill format:** AGENTS.md is free-form project instructions with no schema and no concept of skills. It is out of scope here; a future change may load a project `AGENTS.md` as system-prompt context, but this plan does not cover that.
- **Reference implementation studied:** Anthropic Claude Code, which implements agentskills.io and extends it with fields such as `disable-model-invocation`, `user-invocable`, `argument-hint`, and `when_to_use`. Sam adopts a conservative subset of the Claude extensions for v1.

## 3. Goals and Non-Goals

### Goals

- Load agentskills.io-compliant skill bundles from configurable filesystem roots.
- Expose every enabled user-invocable skill as a slash command.
- Inject a compact catalog of enabled model-invocable skills into the system prompt so the model can auto-invoke.
- Provide a Settings UI tab to toggle each skill's `enabled`, `auto` (model-invocable), and `manual` (user-invocable) flags, plus manage trust for project-scoped skills.
- Stay compatible with Claude-format skill directories (users should be able to drop them in and have them work).
- Never crash the session due to a malformed skill.

### Non-Goals (deferred past v1)

- `context: fork` sub-agent execution.
- Frontmatter `hooks`, `paths`, `model`, `effort`, `agent`.
- `!cmd` inline-bash and `@file` argument substitution.
- Named-positional arguments (`arguments:` frontmatter field).
- Skill signing, checksums, marketplace UX.
- Enforcement of `allowed-tools` at tool-dispatch time (parse + display only in v1).
- Live file-watching (reload requires explicit `/reload-skills` command).

## 4. Architecture

### 4.1 Package Layout

```
internal/skills/
  registry.go       # in-memory map, precedence resolution, reload
  loader.go         # fs scan, SKILL.md parse, validation, load-error capture
  skill.go          # Skill, Frontmatter, Source, Fingerprint, LoadError
  overrides.go      # ~/.config/sam/skills.toml read/write/GC (the "overrides file")
  trust.go          # trusted / denied project paths, pending gate
  catalog.go        # renders <available_skills> system-prompt block
  invoke.go         # renders collapsed user message for slash-invoke
  *_test.go
```

### 4.2 Touch Points in Existing Code

- `internal/tui/commands.go` — merge skill names into autocomplete and `/help`; add new built-in `/reload-skills`.
- `internal/tui/settings_modal.go` — add fourth tab `tabSkills`.
- `internal/tui/settings_skills.go` (new) — custom widget for the Skills tab (lipgloss rows, not `huh`).
- `internal/tui/settings.go` — no changes; `skill_roots` and overrides live in a separate `skills.toml`, not `settings.toml`.
- `internal/agent/loop.go` — inject catalog into system prompt at session start and after `/reload-skills`.
- `internal/agent/history.go` — new `MessageKindSkillInvoke` for collapsed rendering.
- `internal/tui/render.go` + `scrollback.go` — collapse skill-invoke messages by default, expand on focus.
- `cmd/sam/main.go` — construct `skills.Registry`, wire into app boot, hand reference to `agent` and `tui`.

### 4.3 External Dependencies

- `gopkg.in/yaml.v3` — YAML frontmatter parsing. New addition to a TOML-only codebase. Justified by agentskills.io mandating YAML frontmatter inside SKILL.md; hand-rolling a YAML subset is not viable because third-party skills use the full spec (multiline scalars, flow-style lists for `allowed-tools`, etc.). Added to `go.mod` in Phase 1. Parse pattern: hand-split on `---\n...\n---\n` fence, then `yaml.Unmarshal` on the frontmatter slice — keeps the YAML surface area small.

### 4.4 Public Package Surface

```go
type Registry interface {
    Load() error
    Reload() error
    List() []Skill           // includes shadowed and load-errored
    Get(fp Fingerprint) *Skill
    Resolve(name string) (*Skill, bool)  // respects precedence + namespacing
    Catalog() (block string, tokens int) // for system-prompt injection
    RenderInvocation(s *Skill, args string) string // for slash path
    SaveOverrides(map[Fingerprint]Override) error
}
```

Other packages import only `skills.Registry`. The concrete type is unexported.

## 5. Skill Loader and Discovery

### 5.1 Roots

Configured in `skills.toml` field `skill_roots`. Defaults:

1. `$PROJECT_ROOT/.sam/skills` — only loaded if a project root is detected (via existing `gitinfo` logic; falls back to cwd).
2. `~/.sam/skills` — personal.
3. `~/.agents/skills` — tool-agnostic shared location.

Roots are scanned in order; precedence rules in §5.5 handle cross-root conflicts.

### 5.2 Scan

For each root, `filepath.WalkDir` at maximum depth 2 looks for `<root>/<name>/SKILL.md`. Non-matching entries are silently ignored.

Symlinks are followed so users can share skills across repos, but with two guards:

1. After `filepath.EvalSymlinks`, the resolved path must remain under the configured root. Targets that escape the root are skipped with a WARN log.
2. A `seen map[inode]bool` (stat'd via `os.Stat` → `sys_stat.Ino`) breaks cycles. Revisited inodes are skipped silently.

### 5.3 Parse

A SKILL.md file is split on the first `---\n...\n---\n` fence. YAML frontmatter decodes into `SkillFrontmatter`; the remainder is the body.

Supported frontmatter fields for v1:

| Field                       | Required | Notes                                                    |
| --------------------------- | -------- | -------------------------------------------------------- |
| `name`                      | yes      | Must match `^[a-z0-9][a-z0-9-]{0,63}$` and parent dir.   |
| `description`               | yes      | ≤ 1024 chars.                                            |
| `argument-hint`             | no       | Displayed in autocomplete, e.g. `[pr-number]`.           |
| `allowed-tools`             | no       | Parsed, stored, displayed; not enforced in v1.           |
| `disable-model-invocation`  | no       | If true, skill never appears in the catalog.             |
| `user-invocable`            | no       | Defaults true. False hides from slash menu.              |
| `enabled`                   | no       | Sam-specific default; settings override wins.            |

Unknown fields are decoded into a `map[string]any` and preserved but unused. This keeps forward compatibility with future agentskills.io revisions and Claude-only extensions.

#### 5.3.1 Enable/Invocation Precedence

For each skill, the effective flags are resolved in this fixed order (first match wins):

| Layer                       | Fields                                        | Behavior                |
| --------------------------- | --------------------------------------------- | ----------------------- |
| Frontmatter hard-override   | `disable-model-invocation: true`, `user-invocable: false` | Always wins; cannot be re-enabled from settings. |
| Trust / pending state       | pending project skill → `Enabled=false`        | Applied before settings. |
| `skills.toml` override      | `enabled`, `auto`, `manual`                    | User's UI choice.        |
| Frontmatter default         | `enabled` (sam extension)                      | Author's default.        |
| Built-in default            | `Enabled=true`, `ModelInvocable=true` (personal/shared) or `false` (project until toggled) | Fallback. |

`disable-model-invocation` and `user-invocable: false` in frontmatter cannot be overridden by settings. Everything else can.

Pending project skills are a special case: because "Trust / pending state" sits above `skills.toml` override in the table, a user flipping `enabled=true` in the UI for a still-untrusted project skill has no effect until the project is trusted. The Skills tab UI grays out the toggles for pending rows and shows the hint *"trust this project first"* to avoid silent ignores.

### 5.4 Validation

Any of: missing `name`, bad name regex, name ≠ parent dir, missing `description`, description > 1024 chars, body > 50 KB, unreadable file, malformed YAML → skill added to the registry with `LoadError` populated, not registered as a command, not included in catalog. Error logged at INFO. Other skills keep loading.

### 5.5 Precedence and Namespacing

Within a root, paths are sorted lexicographically and the first wins. Across roots, **project > personal > other**. The winning skill is reachable as `/name`. Shadowed skills remain in the registry, flagged `Shadowed: true`, and are reachable only via the colon-namespace form `<root-label>:<name>`.

Root labels: `project`, `personal`, and the last path segment of "other" roots (e.g. `agents` for `~/.agents/skills`). Example: if `review-pr` exists in both `~/.sam/skills` and `.sam/skills`, `/review-pr` and `/project:review-pr` resolve to the project copy; the personal copy is reachable only as `/personal:review-pr`.

Name collision with a built-in slash command: the skill is **not registered** as `/name`; instead it is automatically renamed to `<root-label>:<name>` and advertised that way in both the catalog and the `/help` output. A WARN line is logged at startup. This avoids the situation where the catalog advertises a user path that silently resolves to an unrelated built-in.

### 5.6 Fingerprint

`fingerprint = "sha256:" + hex(sha256(absPath(SKILL.md)))[:16]`. 16 hex chars = 64 bits of sha256, which is ample for a single user's disk (collision probability negligible below ~10^9 entries). Stable across sessions. Used as the key in `skills.toml` overrides, so overrides survive path-case changes on case-preserving filesystems and edits to the SKILL.md body.

### 5.7 Reload

`Registry.Reload()` re-scans all roots, re-resolves precedence, preserves overrides, and triggers catalog rebuild. Invoked at startup and by the new `/reload-skills` built-in slash command.

## 6. Trust and Security

### 6.1 Threat Model

Project-scoped skills mean cloning a repo can auto-register skill files that the agent may invoke (manually or automatically). A malicious SKILL.md could inject adversarial instructions or shadow a personal skill.

`~/.agents/skills` is treated as **trusted by default** — it is a user-created path. Any external tool writing into it bypasses the trust gate. Users are warned of this in the docs shipped in Phase 5; a future phase may add opt-in change detection (hash manifest) for this root.

### 6.2 Defenses

1. **First-run trust prompt.** On registry load, sam counts project-root skills whose project path is absent from both `trust` and `deny` lists in `skills.toml`. If >0, a blocking modal (built on the existing `internal/tui/approval.go` pattern) prompts:
   ```
   Found N skills in .sam/skills/ from this project.
   Skills can inject instructions into your agent turns.
   Trust this project?
     [v]iew  [y]es permanent  [o]nce  [n]o (deny list)
   ```
   - `y` appends to `trust`; `o` clears pending in-memory only; `n` appends to `deny`; `v` shows skill names and first body line, then returns.
2. **Pending state.** Until trusted, project skills are loaded (for inspection) but force-overridden to `Enabled=false`.
3. **Default auto-invoke off for project skills.** Even after trust, project skills start `ModelInvocable=false`; the user must flip this per skill. Personal and shared skills default to `ModelInvocable=true`.
4. **Frontmatter hard overrides settings.** `disable-model-invocation: true` in frontmatter always wins.
5. **Size cap.** SKILL.md body > 50 KB → `LoadError`.
6. **No execution at load.** Nothing in a SKILL.md runs until an explicit user or model invocation.
7. **Catalog token caps.** See §7.2.

## 7. Invocation

### 7.1 Slash-Invoke (`/review-pr 123`)

1. `parseCommand` in `commands.go` is extended: after the built-in switch, it consults `Registry.Resolve(name)` and returns `(Command=CmdSkill, skill, args)` or `CmdUnknown`.
2. The update loop emits a `MsgSkillInvoke{Skill, Args}` event.
3. The agent layer calls `skills.RenderInvocation(skill, args)`, which strips the frontmatter and substitutes `$ARGUMENTS` (empty string when no args). Body size is re-checked against the 50 KB cap from §5.4 as defense-in-depth.
4. A standard user-role message is appended to `agent.History` with `Role: "user"` and `Content: <rendered body>` — this is what the provider sees and what survives `/clear` and session resume. Alongside it, the TUI records a lightweight annotation (`skillInvokeMeta{fingerprint, header: "/review-pr 123", source}`) keyed by message index in the TUI-layer scrollback model. The annotation is a rendering decoration only; it is not part of the wire-level history and is not sent to the provider.
5. The render layer (`render.go` + `scrollback.go`) consults the annotation map. Annotated messages render collapsed as `▸ /review-pr 123  (<source>)`; focus + Enter expands to the full body. Messages without an annotation render normally.
6. The message is sent to the provider as a normal user-role turn. The model sees the body as the user's prompt.
7. Disabled skill → `/review-pr` responds with an info toast: *"skill disabled; enable in /settings → Skills"*. When two skills share a name across roots, the winning one (highest-precedence root) takes `/name`; the shadowed one is reachable only via its namespaced form `<shadowed-root-label>:<name>` (e.g. if project shadows personal, the personal copy becomes `/personal:review-pr`).

### 7.2 Auto-Invoke (Model-Invoked)

At session start (and after `/reload-skills`), `agent.Loop` calls `Registry.Catalog()` filtered to `Enabled && ModelInvocable && !LoadError`. The returned block is appended to the existing system prompt **once per session**, not per turn. Format:

```
<available_skills>
- review-pr: Review a GitHub PR. <SKILL.md: /Users/x/.sam/skills/review-pr/SKILL.md>
- run-tests: Run project test suite. <SKILL.md: /Users/x/.sam/skills/run-tests/SKILL.md>
</available_skills>

To use a skill, Read its SKILL.md path, then follow the instructions inside.
```

Token budget: soft warn at 2,048 tokens, hard cap at 4,096 tokens. The `chars/4` estimator is advisory only — fine for English, drifts for dense JSON/code bodies. When the provider's tokenizer is available it is preferred; otherwise the estimator is labeled as approximate in the UI. Over the hard cap, skills are sorted by source priority (personal > project > other, then alphabetical) and the tail is silently demoted to manual-only with a WARN log.

Level 2 fetch uses the existing Read tool — no new plumbing, no new tool surface. Level 3 (bundled `scripts/`, `references/`, `assets/`) also piggybacks on Read + Bash with existing policy.

## 8. Settings UI

### 8.1 New Tab in `settingsModal`

Fourth tab `tabSkills` after Theme. Custom widget `internal/tui/settings_skills.go` because `huh` cannot cleanly render the multi-column row layout.

```
┌─ Skills ────────────────────────────────────────────────────┐
│ Roots: .sam/skills, ~/.sam/skills, ~/.agents/skills   [e]dit │
│ Catalog budget: 1,240 / 2,048 tok                            │
│                                                              │
│ ▸ review-pr        personal  [x]auto [x]manual  ~80 tok      │
│   Review a GitHub PR. Use when user asks to review...        │
│ ▸ run-tests        project   [x]auto [x]manual  ~65 tok      │
│   Run project test suite.                                    │
│   shadowed by project:run-tests                              │
│ ✗ broken-skill     personal  LOAD ERROR: missing name        │
│                                                              │
│ Trusted projects (2):  /Users/x/repo-a  /Users/x/repo-b  [t] │
└──────────────────────────────────────────────────────────────┘
```

Keys: `↑↓` row nav; `x` toggle `enabled`; `a` toggle auto; `m` toggle manual; `Enter` expand full description + path (when cursor on a skill row); `t` opens a trust-management sub-panel (when cursor on the trust footer); `r` edit roots list inline (single-line `huh.Input`); `Ctrl+S` save; `Esc` cancel.

Keybinding rationale: the existing settings-modal keymap (`internal/tui/settings_modal.go`) rebinds `Tab`/`Shift+Tab` for tab-nav and reuses `↑↓` and `Enter` for field nav inside `huh` forms. The Skills tab uses a custom widget (not `huh`) so it does not collide with `huh.KeyMap` defaults, but `e` is reserved because lowercase letters in other tabs reach `huh.Input` fields — `r` avoids any risk of confusion. `Space` is avoided because it is treated as input in `huh.Input` fields and users switching tabs with partially-typed input could trip it.

Pending changes held on the modal in `pendingOverrides map[Fingerprint]Override`. On save: `skills.SaveOverrides(pending)` writes `~/.config/sam/skills.toml`, then `agent.ReloadSkillCatalog()` rebuilds the system-prompt block.

### 8.2 Persistence Schema (`~/.config/sam/skills.toml`)

```toml
skill_roots = [".sam/skills", "~/.sam/skills", "~/.agents/skills"]

[trust]
"/Users/x/repo-a" = true

[deny]
"/Users/x/sketchy-repo" = true

[skills."sha256:abc123..."]
path    = "/Users/x/.sam/skills/review-pr/SKILL.md"  # debug only
enabled = true
auto    = true
manual  = true
```

Path is informational only; lookup is by fingerprint. Stale entries (path missing on disk, or nothing resolves to that fingerprint) are GC'd on load with an INFO log.

`skill_roots` deliberately lives in `skills.toml` (not `settings.toml`) because it is skills-scoped. `settings.toml` continues to hold TUI, theme, and provider configuration only.

## 9. Arguments

Only `$ARGUMENTS` substitution is supported in v1. It is replaced with the raw text after the slash-command name, with leading/trailing whitespace trimmed. If the user typed no arguments, `$ARGUMENTS` is substituted with the empty string — the skill body decides how to handle absence.

Escape grammar (simple, no multi-level):

- `$ARGUMENTS` → replaced with the argument text.
- `\$ARGUMENTS` → replaced with the literal string `$ARGUMENTS` (one leading backslash consumed).
- All other backslashes pass through unchanged. There is no `\\$ARGUMENTS` escape for "literal backslash followed by substitution"; if a skill author needs that sequence it must be written differently (e.g. split across lines).

Named positional arguments and `!cmd` / `@file` substitution are explicitly deferred.

## 10. Test Strategy

Follows the existing `*_test.go` style: table-driven, no mocking framework, standard-library assertions.

### 10.1 Unit

- `loader_test.go` — frontmatter parse (valid, missing required, unknown fields, bad YAML), body split, validation rules (regex, lengths), symlink following, depth-2 walk boundary, 50 KB cap.
- `registry_test.go` — precedence, shadowing, colon-namespace, built-in collision, fingerprint stability.
- `overrides_test.go` — overrides-file round-trip, stale GC, default-merge per the §5.3.1 precedence table.
- `trust_test.go` — pending gate, trust / deny / once transitions, path normalization.
- `catalog_test.go` — block rendering snapshot, empty case, token estimation, over-cap demotion ordering.
- `invoke_test.go` — `$ARGUMENTS` substitution (present, empty, multiple occurrences, `\$ARGUMENTS` escape).

### 10.2 TUI

- `settings_skills_test.go` — row render, keybinding dispatch, pending-override collection.
- Extend `settings_modal_test.go` — tab nav reaches Skills.
- Extend command-parsing tests in `commands.go` — skill names appear in autocomplete after registry load.

### 10.3 Integration

Under `internal/agent/`, driven by `internal/llm/fake/provider.go`:

- Fixture skills dir + `/review-pr x` → history contains a user-role message with the substituted body, and the TUI annotation map records a collapsed decoration for that index.
- Auto-invoke: first provider call's system prompt contains the catalog block; after toggling `ModelInvocable=false` + `/reload-skills`, the block is absent.
- Malformed skill never panics; session continues.

**Prerequisite spike** before Phase 3: confirm `internal/llm/fake/provider.go` exposes captured system-prompt text on recorded requests. If not, extend the fake provider to record system prompts before writing the auto-invoke integration test.

## 11. Rollout Phases

1. **Loader + registry + tests.** No TUI, no invocation. Internal-only API.
2. **Slash invocation + collapsed rendering + `/reload-skills`.** Usable via slash; auto-invoke globally disabled by a gate.
3. **Settings UI Skills tab + trust prompt.** Removes need to edit `skills.toml` by hand. Auto-invoke gate remains off. (Swapped with former Phase 3 so that trust UI lands **before** any auto-invoke path can reach the catalog.)
4. **Catalog injection + auto-invoke.** Gated by `[skills] auto_invoke_enabled = true` in `skills.toml`, which the Skills tab surfaces as a top-level toggle. Users who stopped at Phase 3 can opt in explicitly.
5. **Docs + seed skills** (`/review-pr`, `/run-tests`, `/commit-message` as reference examples in `~/.agents/skills/`).

## 12. Acceptance Criteria

- Dropping a Claude-format skill directory into any configured root loads it without modification.
- Slash command `/skill-name` with and without arguments works end-to-end and produces a collapsed history entry.
- Auto-invoke path: the catalog block appears in the system prompt, and the model can read the referenced SKILL.md via the existing Read tool and proceed.
- A project skill in a fresh repo requires the trust prompt before any loading effect; denying adds the path to `deny` and skips it on future loads.
- Settings UI toggles persist to `skills.toml` and apply in the running session without restart.
- A malformed skill is surfaced as a red `LOAD ERROR` row in the Skills tab and never crashes the session.
- Disabling a skill in the Settings UI removes it from `/` autocomplete and the `/help` listing immediately, without requiring a restart.

## 13. Open Questions

None blocking. Revisit when agentskills.io defines signing/checksums; revisit `context: fork` sub-agent execution after v1 ships.
