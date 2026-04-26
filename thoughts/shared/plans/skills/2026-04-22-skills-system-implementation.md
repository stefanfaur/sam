# Skills System — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use executing-plans to implement this plan task-by-task.

**Goal:** Deliver the skills system described in `2026-04-22-skills-system-design.md`. Add a `skills` package, expose enabled skills as slash commands, inject a model-invocable catalog into the system prompt, and build a Settings UI tab to toggle skills + manage project trust.

**Tech Stack:** Go 1.26.2, Bubbletea 1.3.10, Glamour 1.0.0, Lipgloss 1.1.1, Huh, BurntSushi/toml, new dep `gopkg.in/yaml.v3`.

**Landing order rationale:** Pure loader + registry land first (zero TUI/agent coupling, fully unit-testable). Slash-invoke plumbing second (thin wire through TUI → agent). Settings UI third — **before** auto-invoke, per design §11. Auto-invoke catalog last, gated by config flag. Each phase compiles and ships value on its own.

---

## Phase 1 — Loader, Registry, Overrides

### Task 1.1: Add yaml.v3 dependency and scaffold `internal/skills/` package

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/skills/skill.go`
- Create: `internal/skills/doc.go`

**Step 1: Add dependency**

```
go get gopkg.in/yaml.v3
```

**Step 2: `skill.go` — core types**

```go
package skills

import "time"

type Fingerprint string // "sha256:<16 hex>"

type Source string
const (
    SourceProject  Source = "project"
    SourcePersonal Source = "personal"
    SourceOther    Source = "other" // labeled by last path segment (e.g. "agents")
)

type Frontmatter struct {
    Name                    string   `yaml:"name"`
    Description             string   `yaml:"description"`
    ArgumentHint            string   `yaml:"argument-hint"`
    AllowedTools            string   `yaml:"allowed-tools"` // stored raw, not enforced
    DisableModelInvocation  bool     `yaml:"disable-model-invocation"`
    UserInvocable           *bool    `yaml:"user-invocable"`     // pointer = unset vs false
    Enabled                 *bool    `yaml:"enabled"`            // sam extension
    Unknown                 map[string]any `yaml:",inline"`
}

type LoadError struct {
    Path   string
    Reason string
}

type Skill struct {
    Fingerprint Fingerprint
    Path        string // abs path to SKILL.md
    Root        string // abs path of its root dir
    RootLabel   string // "project" | "personal" | last-path-segment
    Source      Source
    Name        string
    Body        string // frontmatter-stripped
    FM          Frontmatter
    LoadError   *LoadError
    Shadowed    bool
    Pending     bool // project skill in untrusted project

    // effective flags (resolved per §5.3.1 precedence table)
    Enabled         bool
    ModelInvocable  bool
    UserInvocable   bool

    LoadedAt time.Time
}

const MaxBodyBytes = 50 * 1024
```

**Verification:** `go build ./internal/skills/...` succeeds.

---

### Task 1.2: Loader — scan, parse, validate

**Files:**
- Create: `internal/skills/loader.go`
- Create: `internal/skills/loader_test.go`
- Create: `internal/skills/testdata/skills/ok/SKILL.md` (fixture)
- Create: `internal/skills/testdata/skills/bad-name/SKILL.md` (fixture)
- Create: `internal/skills/testdata/skills/missing-desc/SKILL.md` (fixture)
- Create: `internal/skills/testdata/skills/oversize/SKILL.md` (fixture, >50KB)

**Step 1: `loader.go`**

```go
package skills

import (
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "fmt"
    "io/fs"
    "os"
    "path/filepath"
    "regexp"
    "strings"

    "gopkg.in/yaml.v3"
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ScanRoot walks <root>/<name>/SKILL.md at max depth 2.
// Returns both successfully loaded skills and skills with LoadError set.
func ScanRoot(root, rootLabel string, source Source) ([]*Skill, error) {
    abs, err := filepath.Abs(root)
    if err != nil { return nil, err }
    info, err := os.Stat(abs)
    if errors.Is(err, os.ErrNotExist) { return nil, nil }
    if err != nil { return nil, err }
    if !info.IsDir() { return nil, fmt.Errorf("skills: %s not a directory", abs) }

    seen := map[uint64]bool{}
    var out []*Skill
    entries, err := os.ReadDir(abs)
    if err != nil { return nil, err }
    for _, e := range entries {
        if !e.IsDir() { continue }
        skillDir := filepath.Join(abs, e.Name())
        resolved, err := filepath.EvalSymlinks(skillDir)
        if err != nil { continue }
        if !strings.HasPrefix(resolved, abs+string(os.PathSeparator)) && resolved != abs {
            // symlink escapes root; skip with WARN (caller logs)
            continue
        }
        st, err := os.Stat(resolved)
        if err != nil { continue }
        if ino, ok := inode(st); ok {
            if seen[ino] { continue }
            seen[ino] = true
        }
        path := filepath.Join(resolved, "SKILL.md")
        if _, err := os.Stat(path); err != nil { continue }
        out = append(out, loadSkillFile(path, abs, rootLabel, source, e.Name()))
    }
    // lexicographic order, stable
    sortByPath(out)
    return out, nil
}

func loadSkillFile(path, root, rootLabel string, source Source, dirName string) *Skill {
    sk := &Skill{Path: path, Root: root, RootLabel: rootLabel, Source: source, Fingerprint: fingerprint(path)}
    data, err := os.ReadFile(path)
    if err != nil {
        sk.LoadError = &LoadError{Path: path, Reason: "read: " + err.Error()}
        return sk
    }
    if len(data) > MaxBodyBytes {
        sk.LoadError = &LoadError{Path: path, Reason: "body exceeds 50 KB"}
        return sk
    }
    fm, body, err := splitFrontmatter(data)
    if err != nil {
        sk.LoadError = &LoadError{Path: path, Reason: "frontmatter: " + err.Error()}
        return sk
    }
    if err := yaml.Unmarshal(fm, &sk.FM); err != nil {
        sk.LoadError = &LoadError{Path: path, Reason: "yaml: " + err.Error()}
        return sk
    }
    if err := validate(&sk.FM, dirName); err != nil {
        sk.LoadError = &LoadError{Path: path, Reason: err.Error()}
        return sk
    }
    sk.Name = sk.FM.Name
    sk.Body = string(body)
    return sk
}

func splitFrontmatter(data []byte) (fm, body []byte, err error) {
    const fence = "---\n"
    if !strings.HasPrefix(string(data), fence) {
        return nil, nil, fmt.Errorf("missing opening ---")
    }
    rest := data[len(fence):]
    idx := strings.Index(string(rest), "\n"+fence)
    if idx < 0 {
        return nil, nil, fmt.Errorf("missing closing ---")
    }
    return rest[:idx+1], rest[idx+len("\n"+fence):], nil
}

func validate(fm *Frontmatter, dirName string) error {
    if fm.Name == "" { return fmt.Errorf("name required") }
    if !nameRe.MatchString(fm.Name) { return fmt.Errorf("name invalid: %q", fm.Name) }
    if fm.Name != dirName { return fmt.Errorf("name %q != dir %q", fm.Name, dirName) }
    if fm.Description == "" { return fmt.Errorf("description required") }
    if len(fm.Description) > 1024 { return fmt.Errorf("description > 1024 chars") }
    return nil
}

func fingerprint(absPath string) Fingerprint {
    h := sha256.Sum256([]byte(absPath))
    return Fingerprint("sha256:" + hex.EncodeToString(h[:8]))
}
```

**Step 2: `inode` helper** — platform-split between `loader_unix.go` and `loader_windows.go` using build tags. On darwin/linux, `sys_stat.Ino`; on windows, return `(0, false)` so the seen-map is a no-op.

**Step 3: `loader_test.go`** — table-driven:
- Valid skill parses and returns populated Skill.
- Missing `name` → LoadError set, Skill.Name empty.
- `name` not matching dir → LoadError.
- Invalid regex → LoadError.
- Body > 50 KB → LoadError.
- Missing fence → LoadError.
- Unknown fields preserved in `FM.Unknown`.
- Symlink escaping root → skipped.
- Cycle via self-link → no infinite loop.

**Verification:** `go test ./internal/skills/...` green.

---

### Task 1.3: Registry — precedence, shadowing, resolve, reload

**Files:**
- Create: `internal/skills/registry.go`
- Create: `internal/skills/registry_test.go`

**Step 1: `registry.go`**

```go
package skills

import (
    "fmt"
    "log/slog"
    "sort"
    "strings"
    "sync"
)

type RootSpec struct {
    Path  string
    Label string
    Source Source
}

type Registry struct {
    mu       sync.RWMutex
    roots    []RootSpec
    skills   []*Skill                // all loaded; shadowed flagged
    byName   map[string]*Skill       // primary (winning) skill by short name
    byFP     map[Fingerprint]*Skill
    overrides Overrides              // loaded from skills.toml
    trust    TrustList
    builtins map[string]bool         // names reserved by built-in slash commands
    log      *slog.Logger
}

func NewRegistry(roots []RootSpec, overrides Overrides, trust TrustList, builtins []string, log *slog.Logger) *Registry {
    bi := make(map[string]bool, len(builtins))
    for _, b := range builtins { bi[b] = true }
    if log == nil { log = slog.Default() }
    return &Registry{roots: roots, overrides: overrides, trust: trust, builtins: bi, log: log}
}

func (r *Registry) Load() error {
    r.mu.Lock()
    defer r.mu.Unlock()
    var all []*Skill
    for _, rs := range r.roots {
        got, err := ScanRoot(rs.Path, rs.Label, rs.Source)
        if err != nil {
            r.log.Warn("skills: scan root failed", "root", rs.Path, "err", err)
            continue
        }
        all = append(all, got...)
    }
    r.applyPrecedence(all)
    r.applyOverridesAndTrust()
    return nil
}

func (r *Registry) Reload() error { return r.Load() }

// applyPrecedence: project > personal > other; within a source, lexicographic.
// On cross-root name collision, winner keeps /name; losers marked Shadowed.
// On built-in collision, skill is NOT registered under /name; it gets a namespaced
// form only. Flagged via Skill.Shadowed=true + internally via builtinCollision.
func (r *Registry) applyPrecedence(all []*Skill) {
    sort.SliceStable(all, func(i, j int) bool {
        if rank(all[i].Source) != rank(all[j].Source) { return rank(all[i].Source) < rank(all[j].Source) }
        return all[i].Path < all[j].Path
    })
    r.skills = all
    r.byName = map[string]*Skill{}
    r.byFP   = map[Fingerprint]*Skill{}
    for _, sk := range all {
        r.byFP[sk.Fingerprint] = sk
        if sk.LoadError != nil { continue }
        if r.builtins[sk.Name] {
            sk.Shadowed = true // advertised only as <label>:<name>
            r.log.Warn("skills: name collides with built-in; only reachable as namespaced", "name", sk.Name)
            continue
        }
        if _, exists := r.byName[sk.Name]; exists {
            sk.Shadowed = true
            continue
        }
        r.byName[sk.Name] = sk
    }
}

func rank(s Source) int {
    switch s {
    case SourceProject: return 0
    case SourcePersonal: return 1
    default: return 2
    }
}

// Resolve implements name lookup including colon-namespaced form.
func (r *Registry) Resolve(input string) (*Skill, bool) {
    r.mu.RLock(); defer r.mu.RUnlock()
    if idx := strings.IndexByte(input, ':'); idx >= 0 {
        label, name := input[:idx], input[idx+1:]
        for _, sk := range r.skills {
            if sk.Name == name && sk.RootLabel == label && sk.LoadError == nil {
                return sk, true
            }
        }
        return nil, false
    }
    sk, ok := r.byName[input]
    return sk, ok
}

func (r *Registry) List() []*Skill {
    r.mu.RLock(); defer r.mu.RUnlock()
    out := make([]*Skill, len(r.skills))
    copy(out, r.skills)
    return out
}

func (r *Registry) Get(fp Fingerprint) *Skill {
    r.mu.RLock(); defer r.mu.RUnlock()
    return r.byFP[fp]
}
```

**Step 2: `applyOverridesAndTrust`** — walks `r.skills`, computes effective `Enabled`/`ModelInvocable`/`UserInvocable`/`Pending` per the §5.3.1 precedence table. Project skills in an untrusted project get `Pending=true, Enabled=false`. Project skills in a trusted project default `ModelInvocable=false`.

**Step 3: `registry_test.go`** — cases:
- Project skill beats personal with same name; personal flagged Shadowed, reachable as `personal:foo`.
- Built-in collision: skill flagged Shadowed, `Resolve("clear")` returns the built-in's nil path (i.e. not in byName), `Resolve("personal:clear")` works.
- Fingerprint stable across reload.
- Two personal skills same name: lexicographic-first wins.
- Effective flags honor precedence table (fixture frontmatter + fixture override file).
- Pending gating: untrusted project skill has Enabled=false regardless of override value; trusted project skill defaults ModelInvocable=false.

**Verification:** registry tests green.

---

### Task 1.4: Overrides file — `skills.toml` read/write/GC

**Files:**
- Create: `internal/skills/overrides.go`
- Create: `internal/skills/overrides_test.go`

**Step 1: `overrides.go`**

```go
package skills

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/BurntSushi/toml"
)

type Overrides struct {
    SkillRoots       []string                          `toml:"skill_roots"`
    AutoInvokeEnable *bool                             `toml:"auto_invoke_enabled"` // pointer = unset
    Trust            map[string]bool                   `toml:"trust"`
    Deny             map[string]bool                   `toml:"deny"`
    Skills           map[string]SkillOverride          `toml:"skills"`
}

type SkillOverride struct {
    Path    string `toml:"path"` // informational
    Enabled *bool  `toml:"enabled"`
    Auto    *bool  `toml:"auto"`
    Manual  *bool  `toml:"manual"`
}

func OverridesPath() string {
    if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
        return filepath.Join(xdg, "sam", "skills.toml")
    }
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".config", "sam", "skills.toml")
}

func LoadOverrides() (Overrides, error) {
    var o Overrides
    data, err := os.ReadFile(OverridesPath())
    if os.IsNotExist(err) { return o, nil }
    if err != nil { return o, err }
    if err := toml.Unmarshal(data, &o); err != nil { return o, err }
    return o, nil
}

func SaveOverrides(o Overrides) error {
    p := OverridesPath()
    if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { return err }
    f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
    if err != nil { return err }
    defer f.Close()
    return toml.NewEncoder(f).Encode(o)
}

// GC: drop entries whose fingerprint is not in validFP.
func (o *Overrides) GC(validFP map[Fingerprint]bool) (removed int) {
    for k := range o.Skills {
        if !validFP[Fingerprint(k)] { delete(o.Skills, k); removed++ }
    }
    return
}
```

**Step 2: `overrides_test.go`** — round-trip, GC, pointer-fields distinguishing unset vs false.

**Verification:** green tests.

---

### Task 1.5: Trust list + pending gate

**Files:**
- Create: `internal/skills/trust.go`
- Create: `internal/skills/trust_test.go`

**Step 1: `trust.go`**

```go
package skills

import (
    "path/filepath"
    "strings"
)

type TrustList struct {
    Trusted map[string]bool // abs project paths
    Denied  map[string]bool
}

func (t TrustList) State(projectDir string) TrustState {
    abs, _ := filepath.Abs(projectDir)
    abs = strings.TrimRight(abs, string(filepath.Separator))
    if t.Denied[abs] { return TrustDenied }
    if t.Trusted[abs] { return TrustAllowed }
    return TrustPending
}

type TrustState int
const (
    TrustPending TrustState = iota
    TrustAllowed
    TrustDenied
)
```

**Step 2: tests** — normalization, state transitions.

**Verification:** green.

---

## Phase 2 — Slash-Invoke Pipeline

### Task 2.1: Invocation renderer + `$ARGUMENTS` substitution

**Files:**
- Create: `internal/skills/invoke.go`
- Create: `internal/skills/invoke_test.go`

```go
package skills

import "strings"

func RenderInvocation(sk *Skill, args string) (string, error) {
    args = strings.TrimSpace(args)
    body := sk.Body
    // escape: \$ARGUMENTS → literal $ARGUMENTS
    body = strings.ReplaceAll(body, `\$ARGUMENTS`, "\x00")
    body = strings.ReplaceAll(body, `$ARGUMENTS`, args)
    body = strings.ReplaceAll(body, "\x00", `$ARGUMENTS`)
    if len(body) > MaxBodyBytes {
        return "", errBodyTooLarge
    }
    return body, nil
}

var errBodyTooLarge = constError("skills: rendered body exceeds 50 KB")
type constError string
func (e constError) Error() string { return string(e) }
```

Tests: empty args, args present, multiple `$ARGUMENTS`, escape, escape-then-substitute ordering.

**Verification:** green.

---

### Task 2.2: Wire registry into agent + add `SkillInvoke` submit path

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/loop.go`
- Create: `internal/agent/skill_invoke_test.go`

**Step 1: Extend `Agent`** — add `skills *skills.Registry` field + `SetSkills(*skills.Registry)` setter. Options struct gets `Skills *skills.Registry`.

**Step 2: `SubmitSkill(ctx, name, args)`** — new public method. Resolves the skill via registry, renders, calls `Submit` with the rendered body as the user message. Emits a lightweight `SkillInvoked{Fingerprint, Header}` event on the returned channel **before** the normal turn events, so the TUI can attach its annotation.

```go
func (a *Agent) SubmitSkill(ctx context.Context, nameOrNS, args string) (<-chan Event, error) {
    sk, ok := a.skills.Resolve(nameOrNS)
    if !ok { return nil, fmt.Errorf("unknown skill: %s", nameOrNS) }
    if !sk.Enabled || !sk.UserInvocable {
        return nil, fmt.Errorf("skill disabled: %s", sk.Name)
    }
    body, err := skills.RenderInvocation(sk, args)
    if err != nil { return nil, err }
    out := make(chan Event, 64)
    header := "/" + nameOrNS
    if args != "" { header += " " + args }
    // write annotation first, then pipe through normal Submit
    go func() {
        emitToChan(out, SkillInvoked{Fingerprint: string(sk.Fingerprint), Header: header, Source: string(sk.Source)}, ctx)
        src := a.Submit(ctx, body)
        for e := range src { emitToChan(out, e, ctx) }
        close(out)
    }()
    return out, nil
}
```

**Step 3: New event type in `internal/agent/events.go`**:

```go
type SkillInvoked struct {
    Fingerprint string
    Header      string
    Source      string
}
```

**Step 4: Test** using `internal/llm/fake/provider.go` — drive `SubmitSkill("review-pr", "123")`, assert first event is `SkillInvoked`, assert history contains user message with substituted body, assert disabled skill returns error.

**Verification:** `go test ./internal/agent/...` green.

---

### Task 2.3: TUI command parsing — recognize skills, emit skill-invoke

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/app.go` (skills field on `Model`)
- Modify: `internal/tui/update.go` (handle skill invoke)
- Modify: `internal/tui/messages.go` (if needed for annotation store)

**Step 1: Extend `parseCommand`** — add `CmdSkill` constant, add `CmdReloadSkills`; after the builtins switch, if a registry is attached, consult `Resolve(name)`:

```go
const (
    ...
    CmdSkill         Command = "skill"
    CmdReloadSkills  Command = "reload-skills"
)

// new signature: parseCommand(text string, reg *skills.Registry) (Command, string, *skills.Skill)
```

The returned `*skills.Skill` is nil for everything except `CmdSkill`. Builtin list (`commands.go`) grows `/reload-skills`.

**Step 2: Autocomplete** — rebuild `commandSuggestions` dynamically from built-ins + enabled user-invocable skills sorted alphabetically. Skills appear as `/skill-name <argument-hint> — description`. Shadowed skills appear in their namespaced form.

**Step 3: `/help` output** — append a `Skills:` section listing enabled user-invocable skills with one-line descriptions (truncate to term width). If >10 skills, collapse to `... and N more (see /settings → Skills)`.

**Step 4: Update loop** — on `CmdSkill`, call `model.agent.SubmitSkill(...)`, and when the `SkillInvoked` event fires, record `annotationMap[historyIdx] = skillAnnotation{...}` on the Model.

**Step 5: Tests** — extend `parseCommand` test; add a table covering builtin precedence, skill resolution, namespaced form.

**Verification:** `go test ./internal/tui/...` green.

---

### Task 2.4: Scrollback — collapsed render for annotated user messages

**Files:**
- Modify: `internal/tui/scrollback.go`
- Modify: `internal/tui/render.go`
- Create: `internal/tui/scrollback_skill_test.go`

**Step 1:** Add `skillAnnotations map[int]skillAnnotation` to the scrollback/Model. Index = position in the rendered message list.

**Step 2:** When rendering a user message, if its index appears in the annotation map, render the collapsed form:

```
▸ /review-pr 123  (personal)
```

…unless the user's cursor is on it and expanded. Expanded = normal markdown render of the body with a header line.

**Step 3:** Existing "focus row" infrastructure in scrollback handles navigation; add `Enter` binding to toggle expansion for annotated rows.

**Step 4:** Snapshot-style test — fixture history + annotations → expected rendered output.

**Verification:** existing scrollback tests still green; new test green.

---

### Task 2.5: `/reload-skills` builtin

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/update.go`

On `CmdReloadSkills`: call `registry.Reload()`; on success, emit info toast `skills reloaded: N enabled, M shadowed, E errors`. Rebuild slash-command autocomplete.

**Verification:** manual run — drop a new skill into `~/.sam/skills`, hit `/reload-skills`, confirm `/` menu lists it.

---

### Task 2.6: Boot wiring

**Files:**
- Modify: `cmd/sam/main.go`

**Steps:**
1. Load overrides via `skills.LoadOverrides()` (errors → warn log, continue with zero value).
2. Build `TrustList` from `Overrides.Trust` / `Overrides.Deny`.
3. Resolve skill roots: start with `Overrides.SkillRoots` if present, else defaults `[".sam/skills", "~/.sam/skills", "~/.agents/skills"]`. Expand `~` to home. For the project root, use `gitinfo` (or fallback to cwd). Drop roots that don't exist (silent).
4. Built-in names list: `["exit","quit","clear","reset","model","provider","cwd","help","settings","reload-skills"]`.
5. `registry := skills.NewRegistry(...)`; `registry.Load()`.
6. Pass registry to `agent.New` (via `Options.Skills`) and to the TUI Model.
7. After registry load, if any pending project skills, push trust prompt (Task 3.3) via the existing approval-modal event pipeline.

**Verification:** `sam` boots with an empty `~/.sam/skills/`, boots with a fixture skills dir (prints "N skills loaded" in debug overlay), boots after deleting `skills.toml` without panic.

---

## Phase 3 — Settings UI Skills Tab + Trust Prompt

### Task 3.1: Settings tab scaffolding

**Files:**
- Modify: `internal/tui/settings_modal.go`
- Create: `internal/tui/settings_skills.go`
- Create: `internal/tui/settings_skills_test.go`

**Step 1:** Add `tabSkills` to the `settingsTab` iota (before `numTabs`). Extend `settingsModal` with `skillsWidget *skillsWidget`. Extend `buildXForm` pattern with `buildSkillsTab()` returning a `tea.Model`-ish component (not `huh`).

**Step 2:** Implement `skillsWidget` as a custom component: holds `registry`, `pendingOverrides map[skills.Fingerprint]skills.SkillOverride`, `pendingRoots []string`, `pendingTrust map[string]bool`, `pendingDeny map[string]bool`, cursor index, expanded-rows set.

**Step 3:** Row renderer per design §8.1. Token estimator = `len(name)+len(description)+path_overhead` divided by 4; footer shows `Catalog budget: X / 2048 tok`.

**Step 4:** Keybindings per design §8.1: `↑↓` row nav; `x` toggle enabled; `a` toggle auto; `m` toggle manual; `Enter` expand detail; `t` opens trust sub-panel; `r` edits roots inline.

**Step 5:** On save (modal `Ctrl+S`): merge pending into `Overrides`, `skills.SaveOverrides`, `registry.Reload`, then emit `addInfo("skills saved: N enabled")`.

**Step 6:** Pending project-skill rows → graying + hint `"trust this project first"`, per design §5.3.1 note.

**Tests:**
- Render with fixture registry → key segments present (name, source, tokens).
- `a`/`m`/`x` key dispatch updates pending overrides.
- Pending skill → toggles disabled.

**Verification:** `go test ./internal/tui/...` green.

---

### Task 3.2: Trust-management sub-panel

**Files:**
- Modify: `internal/tui/settings_skills.go`

Sub-panel triggered by `t`: list of currently-trusted project paths + denied paths, with `d` to demote (trusted → pending by removing from trust list), `u` to un-deny, `Esc` back to list. Changes held in `pendingTrust`/`pendingDeny`, flushed on `Ctrl+S`.

**Verification:** manual — trust a project, open settings, demote it, save, reopen sam — skill is pending again.

---

### Task 3.3: First-run trust prompt modal

**Files:**
- Create: `internal/tui/trust_prompt.go`
- Modify: `internal/tui/update.go` (dispatch)
- Modify: `cmd/sam/main.go` (fire on boot)

**Step 1:** Build on the existing `approvalModal` pattern (`internal/tui/approval.go`). Modal displays:

```
Found 3 skills in .sam/skills/ from this project.
Skills can inject instructions into your agent turns.

Trust this project?
  [v]iew  [y]es permanent  [o]nce  [n]o (deny list)
```

`v` → toggles an inner view showing `name` + first-body-line per skill, then returns. `y` → mutates in-memory `TrustList`, persists via `SaveOverrides`, re-calls `registry.Reload()`. `o` → mutates in-memory trust only (not persisted). `n` → adds to `Denied` + persists + reload.

**Step 2:** Fire condition: after `registry.Load()` on boot, if any skill has `Source==SourceProject && Pending==true`, push the modal before the first user prompt is accepted.

**Step 3:** Test — fixture project with untrusted skills → modal fires; `y` path persists trust + makes skill Enabled.

**Verification:** manual end-to-end — clone a repo with `.sam/skills/`, run `sam`, answer prompt, confirm skill appears in `/`.

---

## Phase 4 — Auto-Invoke Catalog

### Task 4.1: Catalog renderer

**Files:**
- Create: `internal/skills/catalog.go`
- Create: `internal/skills/catalog_test.go`

```go
package skills

import (
    "fmt"
    "sort"
    "strings"
)

const (
    CatalogSoftCapTokens = 2048
    CatalogHardCapTokens = 4096
)

type CatalogResult struct {
    Block          string
    TokensEstimate int
    Included       int
    Demoted        []*Skill // skills demoted to manual-only due to hard cap
}

func (r *Registry) Catalog() CatalogResult {
    r.mu.RLock(); defer r.mu.RUnlock()
    var elig []*Skill
    for _, sk := range r.skills {
        if sk.LoadError != nil || !sk.Enabled || !sk.ModelInvocable || sk.Shadowed || sk.Pending {
            continue
        }
        elig = append(elig, sk)
    }
    // sort by source priority + alphabetical
    sort.SliceStable(elig, func(i, j int) bool {
        if rank(elig[i].Source) != rank(elig[j].Source) { return rank(elig[i].Source) < rank(elig[j].Source) }
        return elig[i].Name < elig[j].Name
    })

    var lines []string
    running := 0
    res := CatalogResult{}
    for _, sk := range elig {
        line := fmt.Sprintf("- %s: %s <SKILL.md: %s>", sk.Name, sk.FM.Description, sk.Path)
        tok := estimateTokens(line)
        if running+tok > CatalogHardCapTokens {
            res.Demoted = append(res.Demoted, sk)
            continue
        }
        lines = append(lines, line)
        running += tok
        res.Included++
    }
    if len(lines) == 0 { return res }
    res.Block = "<available_skills>\n" + strings.Join(lines, "\n") +
        "\n</available_skills>\n\nTo use a skill, Read its SKILL.md path, then follow the instructions inside.\n"
    res.TokensEstimate = running
    return res
}

func estimateTokens(s string) int { return (len(s) + 3) / 4 } // chars/4, advisory
```

**Tests:** empty case, ordering, hard-cap demotion, block format snapshot, excluded-when-shadowed/pending/load-error.

**Verification:** green.

---

### Task 4.2: Inject catalog into agent system prompt

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/loop.go`
- Modify: `cmd/sam/main.go`

**Step 1:** Add `baseSystem string` + `catalog string` fields on `Agent`. `New` keeps original system string as `baseSystem`; `a.system = baseSystem`. New method `RebuildSkillCatalog()`:

```go
func (a *Agent) RebuildSkillCatalog() {
    a.mu.Lock(); defer a.mu.Unlock()
    if a.skills == nil { return }
    cr := a.skills.Catalog()
    a.catalog = cr.Block
    if a.catalog == "" { a.system = a.baseSystem } else {
        a.system = a.baseSystem + "\n\n" + a.catalog
    }
    if cr.TokensEstimate > skills.CatalogSoftCapTokens {
        a.log.Warn("skills: catalog exceeds soft cap", "tokens", cr.TokensEstimate)
    }
}
```

**Step 2:** Call `RebuildSkillCatalog()` at end of `New()` and at end of every place the TUI calls `registry.Reload()` (wire through a callback or from the update loop after `/reload-skills` or settings save).

**Step 3:** Gate: respect `Overrides.AutoInvokeEnable` (default nil = off in Phase 4's initial ship; users opt in through Settings tab global toggle). Skip catalog when gate is off.

**Step 4:** Integration test in `internal/agent/` — set up fake provider that records requests, enable a skill with `ModelInvocable=true`, call `Submit`, assert the recorded `Request.System` contains `<available_skills>`. Toggle gate off, reload, assert absent.

**Prerequisite spike** (per design §10.3): confirm `internal/llm/fake/provider.go` captures `Request.System`. If not, extend it first.

**Verification:** `go test ./internal/agent/...` green; manual — enable auto-invoke in settings, ask a relevant prompt, observe model reading SKILL.md via Read tool.

---

### Task 4.3: Settings UI — global auto-invoke toggle

**Files:**
- Modify: `internal/tui/settings_skills.go`

Add a top-of-tab toggle: `[ ] Enable model auto-invocation (catalog in system prompt)`. Persists to `Overrides.AutoInvokeEnable`. When off, grays out `auto` column on all skill rows.

**Verification:** manual — toggle, save, confirm system prompt block appears/disappears in debug overlay.

---

## Phase 5 — Docs, Seed Skills, Acceptance

### Task 5.1: README updates

**Files:**
- Modify: `README.md`

Add:
- `## Skills` section: where skills live, how to write one (minimal SKILL.md example), how to enable/disable, trust model, `$ARGUMENTS` substitution.
- `## Slash Commands` table gets `/reload-skills` row + a note that user-invocable skills show up here too.
- Warning about `~/.agents/skills` being trusted by default.

### Task 5.2: Seed reference skills

**Files:**
- Create: `testdata/skills/review-pr/SKILL.md`
- Create: `testdata/skills/run-tests/SKILL.md`
- Create: `testdata/skills/commit-message/SKILL.md`

Each with valid frontmatter + a short body demonstrating `$ARGUMENTS` usage. These double as test fixtures and a copy-paste starting point users can drop into `~/.agents/skills/`.

### Task 5.3: Acceptance sweep

Run through the design §12 acceptance criteria as a manual checklist; capture any issues as follow-up tasks rather than blocking the ship.

**Verification:** all 7 criteria pass.

---

## Test Matrix Summary

| Package            | Unit | Integration | TUI snapshot |
| ------------------ | :-:  | :-:         | :-:          |
| `internal/skills`  | ✓    | —           | —            |
| `internal/agent`   | ✓    | ✓ (fake)    | —            |
| `internal/tui`     | ✓    | —           | ✓            |

No new external test harness; all tests use stdlib + existing `internal/llm/fake`.

---

## Out-of-Scope Reminders (design §3)

Do **not** implement in this plan:
- `context: fork`, `hooks`, `paths`, `model`, `effort`, `agent` frontmatter fields
- `!cmd` or `@file` substitution
- Named positional `arguments:`
- `allowed-tools` enforcement at dispatch time (parsed & displayed only)
- File-watcher hot reload
- Skill signing / checksums

All deferred items flagged `TODO(skills-v2)` in code where a natural seam exists.
