# Per-Model System Prompts — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task.

**Spec:** `thoughts/shared/plans/system/2026-04-24-per-model-system-prompts-design.md`

**Goal:** Add per-model-family system prompts layered on top of the existing base prompt, with config-driven prefix matching, bundled content for the 6 default families, and dynamic resolution on TUI model switch.

**Architecture:** Extend `internal/config` with a `PromptFamily` type + `[prompt_families.<name>]` TOML surface + `FamilyForModel` method. Extend `internal/system` with a `prompts/<family>.md` embedded/disk file pair alongside the existing `system-prompt.md` + `tools/` layout. Thread a `SystemResolverFn` through the TUI so every provider/model switch path (`switchProvider`, `applyModelSpec`, `settings_modal.applyProviders`) recomputes the prompt and calls a new `Agent.SetSystem`. Existing `system_prompt_file` / `--system-prompt-file` totals-override remains unchanged.

**Tech Stack:** Go 1.26.2, `//go:embed`, BurntSushi/toml, standard `strings.HasPrefix` matching. No new deps.

---

### Task 0: `PromptFamily` type + config merge

**Files:**
- Create: `internal/config/prompt_families.go`
- Modify: `internal/config/config.go` (struct `Config` ~line 47, struct `rawConfig` ~line 127, `Load` initializer ~line 142, merge loop after line 183)
- Create: `internal/config/prompt_families_test.go`

**Step 1: Write the failing tests**

```go
// internal/config/prompt_families_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPromptFamilies_Shape(t *testing.T) {
	fams := DefaultPromptFamilies()
	want := map[string][]string{
		"claude":   {"claude-opus", "claude-sonnet", "claude-haiku"},
		"minimax":  {"MiniMax-"},
		"kimi-k2":  {"kimi-k2"},
		"trinity":  {"trinity-"},
		"gpt":      {"gpt-5", "gpt-4o", "gpt-4.1", "o1", "o3", "o4"},
		"deepseek": {"deepseek-"},
	}
	for name, prefixes := range want {
		got, ok := fams[name]
		if !ok {
			t.Errorf("family %q missing", name)
			continue
		}
		if len(got.Prefixes) != len(prefixes) {
			t.Errorf("family %q: prefix count %d want %d", name, len(got.Prefixes), len(prefixes))
			continue
		}
		for i, p := range prefixes {
			if got.Prefixes[i] != p {
				t.Errorf("family %q prefix[%d]: %q want %q", name, i, got.Prefixes[i], p)
			}
		}
	}
}

// cleanConfigEnv zeros every env var that can override config defaults.
// Every test that calls Load must use it — otherwise a developer's shell
// env (SAM_PROVIDER=foo) leaks into the test and makes Load fail
// validation. Mirrors the pattern in config_test.go lines 14-15.
func cleanConfigEnv(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("SAM_PROVIDER", "")
	t.Setenv("SAM_MODEL", "")
}

func TestLoadSeedsDefaultPromptFamilies(t *testing.T) {
	dir := t.TempDir()
	cleanConfigEnv(t, dir)
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, name := range []string{"claude", "minimax", "kimi-k2", "trinity", "gpt", "deepseek"} {
		if _, ok := cfg.PromptFamilies[name]; !ok {
			t.Errorf("default family %q missing after Load", name)
		}
	}
}

func TestPromptFamilies_ConfigFullReplace(t *testing.T) {
	dir := t.TempDir()
	cleanConfigEnv(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[prompt_families.claude]
prefixes = ["claude-5", "claude-6"]
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := cfg.PromptFamilies["claude"].Prefixes
	if len(got) != 2 || got[0] != "claude-5" || got[1] != "claude-6" {
		t.Fatalf("full-replace failed: %v", got)
	}
}

func TestPromptFamilies_UserFamilyAddsToBundled(t *testing.T) {
	dir := t.TempDir()
	cleanConfigEnv(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[prompt_families.qwen]
prefixes = ["qwen-", "Qwen", "openrouter/qwen/"]
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := cfg.PromptFamilies["qwen"]; !ok {
		t.Fatal("qwen family missing")
	}
	if _, ok := cfg.PromptFamilies["claude"]; !ok {
		t.Fatal("bundled claude disappeared after user family added")
	}
}

func TestPromptFamilies_ConfigDisable(t *testing.T) {
	dir := t.TempDir()
	cleanConfigEnv(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "sam"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[prompt_families.claude]
prefixes = []
`
	if err := os.WriteFile(filepath.Join(dir, "sam", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Overrides{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.PromptFamilies["claude"].Prefixes; len(got) != 0 {
		t.Fatalf("disable-by-empty failed: %v", got)
	}
}
```

**Step 2: Run tests to confirm they fail**

```bash
go test ./internal/config/... -run 'PromptFamilies|DefaultPromptFamilies' -count=1
```
Expected: FAIL — `undefined: DefaultPromptFamilies`, `cfg.PromptFamilies undefined`.

**Step 3: Write minimal implementation**

Create `internal/config/prompt_families.go`:

```go
package config

// PromptFamily groups model-name prefixes that share a per-family system
// prompt addendum. Matching is case-sensitive; declare case variants in
// Prefixes when needed.
type PromptFamily struct {
	Prefixes []string `toml:"prefixes"`
}

// DefaultPromptFamilies returns the bundled family→prefix map. Callers own
// the returned value and may mutate or replace entries. Every call returns
// a fresh copy.
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

Edit `internal/config/config.go`:

- Add to `Config` struct (after `Models` field):
  ```go
  PromptFamilies map[string]PromptFamily `toml:"prompt_families"`
  ```
- Add the same line to `rawConfig` struct (mirror location).
- In `Load`, extend the `cfg := &Config{...}` initializer:
  ```go
  Providers:      Presets(),
  PromptFamilies: DefaultPromptFamilies(),
  ```
- After the existing `for name, entry := range raw.Providers { ... }` merge loop, add:
  ```go
  for name, fam := range raw.PromptFamilies {
      cfg.PromptFamilies[name] = fam
  }
  ```

**Step 4: Run tests to confirm they pass**

```bash
go test ./internal/config/... -run 'PromptFamilies|DefaultPromptFamilies' -count=1
```
Expected: PASS.

**Step 5: Run full config package tests to confirm no regression**

```bash
go test ./internal/config/... -count=1
```
Expected: PASS.

**Step 6: Commit**

```bash
git add internal/config/config.go internal/config/prompt_families.go internal/config/prompt_families_test.go
git commit -m "feat(config): add PromptFamily type with bundled defaults and TOML merge"
```

---

### Task 1: `FamilyForModel` method with longest-prefix resolution

**Depends on: Task 0** (needs `PromptFamily` type + `Config.PromptFamilies` field).

**Files:**
- Modify: `internal/config/config.go` (append method)
- Modify: `internal/config/prompt_families_test.go` (append tests)

**Step 1: Write the failing tests**

Append to `internal/config/prompt_families_test.go`:

```go
func TestFamilyForModel_BundledDefaults(t *testing.T) {
	cfg := &Config{PromptFamilies: DefaultPromptFamilies()}
	cases := map[string]string{
		"MiniMax-M2.7":           "minimax",
		"claude-sonnet-4-5":      "claude",
		"claude-opus-4-7":        "claude",
		"claude-haiku-4-5":       "claude",
		"kimi-k2.6":              "kimi-k2",
		"kimi-k2.5":              "kimi-k2",
		"trinity-large-thinking": "trinity",
		"gpt-4o-mini":            "gpt",
		"gpt-5":                  "gpt",
		"o1-preview":             "gpt",
		"deepseek-r1":            "deepseek",
	}
	for model, want := range cases {
		if got := cfg.FamilyForModel(model); got != want {
			t.Errorf("FamilyForModel(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestFamilyForModel_LongestPrefix(t *testing.T) {
	cfg := &Config{PromptFamilies: map[string]PromptFamily{
		"kimi":    {Prefixes: []string{"kimi-"}},
		"kimi-k2": {Prefixes: []string{"kimi-k2"}},
	}}
	if got := cfg.FamilyForModel("kimi-k2.6"); got != "kimi-k2" {
		t.Fatalf("longest-prefix lost: got %q", got)
	}
	if got := cfg.FamilyForModel("kimi-k1.5"); got != "kimi" {
		t.Fatalf("shorter prefix expected: got %q", got)
	}
}

func TestFamilyForModel_TieBreakLexAsc(t *testing.T) {
	cfg := &Config{PromptFamilies: map[string]PromptFamily{
		"zeta":  {Prefixes: []string{"shared-"}},
		"alpha": {Prefixes: []string{"shared-"}},
	}}
	if got := cfg.FamilyForModel("shared-model"); got != "alpha" {
		t.Fatalf("tie-break lex-asc lost: got %q", got)
	}
}

func TestFamilyForModel_NoMatch(t *testing.T) {
	cfg := &Config{PromptFamilies: DefaultPromptFamilies()}
	if got := cfg.FamilyForModel("random-model-x"); got != "" {
		t.Fatalf("expected empty for unknown: got %q", got)
	}
}

func TestFamilyForModel_EmptyPrefixesSkipped(t *testing.T) {
	cfg := &Config{PromptFamilies: map[string]PromptFamily{
		"claude": {Prefixes: []string{}},
	}}
	if got := cfg.FamilyForModel(""); got != "" {
		t.Errorf("empty prefixes matched empty model: %q", got)
	}
	if got := cfg.FamilyForModel("claude-sonnet-4-5"); got != "" {
		t.Errorf("empty prefixes matched model: %q", got)
	}
}

func TestFamilyForModel_CaseSensitive(t *testing.T) {
	cfg := &Config{PromptFamilies: map[string]PromptFamily{
		"qwen": {Prefixes: []string{"qwen-"}},
	}}
	if got := cfg.FamilyForModel("Qwen-72B"); got != "" {
		t.Fatalf("unexpected case-insensitive match: %q", got)
	}
	if got := cfg.FamilyForModel("qwen-72b"); got != "qwen" {
		t.Fatalf("case-sensitive match lost: %q", got)
	}
}

func TestFamilyForModel_NilMap(t *testing.T) {
	cfg := &Config{}
	if got := cfg.FamilyForModel("claude-sonnet-4-5"); got != "" {
		t.Fatalf("nil map should yield empty: %q", got)
	}
}
```

**Step 2: Run tests to confirm they fail**

```bash
go test ./internal/config/... -run FamilyForModel -count=1
```
Expected: FAIL — `cfg.FamilyForModel undefined`.

**Step 3: Write minimal implementation**

Append to `internal/config/config.go`:

```go
import "sort"

// FamilyForModel returns the family name whose longest prefix matches model,
// using the merged bundled + user PromptFamilies map. Empty when no match.
// Families with len(Prefixes) == 0 are skipped (enables disable-via-empty).
// Ties on prefix length resolve by lexicographic family-name ascending.
func (c *Config) FamilyForModel(model string) string {
	type candidate struct {
		family string
		length int
	}
	var matches []candidate
	for name, fam := range c.PromptFamilies {
		if len(fam.Prefixes) == 0 {
			continue
		}
		for _, p := range fam.Prefixes {
			if p != "" && strings.HasPrefix(model, p) {
				matches = append(matches, candidate{family: name, length: len(p)})
				break
			}
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].length != matches[j].length {
			return matches[i].length > matches[j].length
		}
		return matches[i].family < matches[j].family
	})
	return matches[0].family
}
```

Note: `strings` and `sort` are already imported in `config.go`; verify before adding, skip redundant import lines.

**Step 4: Run tests**

```bash
go test ./internal/config/... -run FamilyForModel -count=1
```
Expected: PASS.

**Step 5: Run full package tests**

```bash
go test ./internal/config/... -count=1
```
Expected: PASS.

**Step 6: Commit**

```bash
git add internal/config/config.go internal/config/prompt_families_test.go
git commit -m "feat(config): add FamilyForModel with longest-prefix resolution"
```

---

### Task 2: Embedded family prompt content files

**Depends on: none** (independent; required before Task 3 so the extended `//go:embed` glob matches non-empty set).

**Files:**
- Create: `internal/system/defaults/prompts/claude.md`
- Create: `internal/system/defaults/prompts/minimax.md`
- Create: `internal/system/defaults/prompts/kimi-k2.md`
- Create: `internal/system/defaults/prompts/trinity.md`
- Create: `internal/system/defaults/prompts/gpt.md`
- Create: `internal/system/defaults/prompts/deepseek.md`

**Context:** Each file is appended verbatim after the base `system-prompt.md` with a single blank line separator. Content must be tailored to model-specific behaviors, NOT duplicate universal rules (caveman speech, thinking mandate, evidence rule) — those live in the base prompt. Keep each file short (5–15 lines). Match the caveman style of the base prompt.

**Step 1: Create all six files together (required by `//go:embed` glob — empty dir fails build)**

`internal/system/defaults/prompts/claude.md`:

```
CLAUDE FAMILY.
Extended thinking blocks signed — preserve signature on tool-call round-trip. Do not strip ContentThinking.
Context 200k tokens. Long files OK to read whole. Prune only on explicit signal.
Prefer structured tool use over inline shell when tool exists. Tool-call parallelism supported — batch independent reads.
```

`internal/system/defaults/prompts/minimax.md`:

```
MINIMAX FAMILY.
Anthropic wire. Extended thinking budget 32k default. Use thinking freely for multi-step plans.
Context 1M tokens. No need to prune history aggressively. Full file reads preferred over snippets.
Reasoning returned as thinking blocks — same round-trip rules as Claude. Preserve on tool rounds.
```

`internal/system/defaults/prompts/kimi-k2.md`:

```
KIMI-K2 FAMILY.
OpenAI wire. reasoning_effort=high by default — produce deliberate step-by-step work, not terse guesses.
Context 262k tokens. Room for long reads, keep transcript but trim tool outputs when very large.
No <think> tag parsing — reasoning comes via reasoning_content stream. Do not emit inline <think>.
```

`internal/system/defaults/prompts/trinity.md`:

```
TRINITY FAMILY.
OpenAI wire via Arcee Conductor. Reasoning surfaces through reasoning_content — do not inline <think>.
Context 512k tokens. Long sessions fine; keep full file contents when helpful.
Conductor may route across backends — assume standard OpenAI tool-call semantics.
```

`internal/system/defaults/prompts/gpt.md`:

```
GPT / O-SERIES FAMILY.
OpenAI wire. No inline thinking blocks — reasoning is internal. Output only final answer + tool calls.
Context varies: gpt-4o 128k, gpt-5 / o-series 400k. Be aware before doing multi-file reads.
Standard OpenAI chat + tool-call conventions. Parallel tool calls supported.
```

`internal/system/defaults/prompts/deepseek.md`:

```
DEEPSEEK FAMILY.
OpenAI wire. Reasoning emitted as inline <think>...</think> tags — parse_think_tags active, tags stripped from visible output.
Context 131k tokens. Moderate — prune large tool outputs when transcript grows.
R1 and V3 both surface reasoning inline. Keep thinking terse; every token in <think> costs context.
```

**Step 2: Verify files present**

```bash
ls internal/system/defaults/prompts/
```
Expected: `claude.md  deepseek.md  gpt.md  kimi-k2.md  minimax.md  trinity.md`.

**Step 3: Commit**

```bash
git add internal/system/defaults/prompts/
git commit -m "feat(system): add per-family embedded prompt content for 6 families"
```

Note: `//go:embed` directive is NOT yet extended — build stays green without it. Task 3 extends the directive and adds the loaders that reference these files.

---

### Task 3: Extend `//go:embed` + family loaders

**Depends on: Task 2** (the `//go:embed defaults/prompts/*.md` glob fails at build time if no matching files exist).

**Files:**
- Create: `internal/system/family.go`
- Modify: `internal/system/system.go` (extend `//go:embed` directive at line 13)
- Create: `internal/system/family_test.go`

**Step 1: Write the failing tests**

Create `internal/system/family_test.go`:

```go
package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedFamilyPrompt_BundledFamilies(t *testing.T) {
	for _, family := range []string{"claude", "minimax", "kimi-k2", "trinity", "gpt", "deepseek"} {
		t.Run(family, func(t *testing.T) {
			got := EmbeddedFamilyPrompt(family)
			if got == "" {
				t.Fatalf("embedded %s.md empty or missing", family)
			}
		})
	}
}

func TestEmbeddedFamilyPrompt_Unknown(t *testing.T) {
	if got := EmbeddedFamilyPrompt("does-not-exist"); got != "" {
		t.Fatalf("unknown family returned content: %q", got)
	}
}

func TestLoadFamilyPrompt_MissingAndPresent(t *testing.T) {
	dir := t.TempDir()
	// Missing → "".
	got, err := LoadFamilyPrompt(dir, "claude")
	if err != nil {
		t.Fatalf("unexpected err on missing: %v", err)
	}
	if got != "" {
		t.Fatalf("missing should be empty: %q", got)
	}
	// Present → content trimmed of trailing whitespace.
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "claude.md"), []byte("hello family   \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = LoadFamilyPrompt(dir, "claude")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "hello family" {
		t.Fatalf("content: %q", got)
	}
}

func TestFamilyPromptExists(t *testing.T) {
	dir := t.TempDir()
	if FamilyPromptExists(dir, "claude") {
		t.Fatal("missing file reported as existing")
	}
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "claude.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !FamilyPromptExists(dir, "claude") {
		t.Fatal("empty-but-present file reported as missing")
	}
}
```

**Step 2: Run tests to confirm they fail**

```bash
go test ./internal/system/... -run 'EmbeddedFamilyPrompt|LoadFamilyPrompt' -count=1
```
Expected: FAIL — `undefined: EmbeddedFamilyPrompt`, `undefined: LoadFamilyPrompt`.

**Step 3: Write minimal implementation**

Edit `internal/system/system.go` line 13:

```go
//go:embed defaults/system-prompt.md defaults/tools/*.md defaults/prompts/*.md
var defaultsFS embed.FS
```

Create `internal/system/family.go`:

```go
package system

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// familyFile returns the embedded/on-disk filename for a family name.
// Family names are used verbatim — no case transform (unlike tool names).
func familyFile(family string) string {
	return family + ".md"
}

// EmbeddedFamilyPrompt returns the embedded prompts/<family>.md content.
// Returns "" if the family is unknown. Trailing whitespace trimmed to match
// LoadFamilyPrompt behavior.
func EmbeddedFamilyPrompt(family string) string {
	b, err := defaultsFS.ReadFile("defaults/prompts/" + familyFile(family))
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\n\r\t ")
}

// LoadFamilyPrompt reads dir/prompts/<family>.md; returns "" if missing.
// Trailing whitespace trimmed for consistency with EmbeddedFamilyPrompt.
// Note: an empty file returns "" — callers must use FamilyPromptExists to
// distinguish "user wiped the file" from "file missing".
func LoadFamilyPrompt(dir, family string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "prompts", familyFile(family)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimRight(string(b), "\n\r\t "), nil
}

// FamilyPromptExists reports whether dir/prompts/<family>.md is present on
// disk, regardless of content. Used by the resolver to distinguish "user
// intentionally emptied this file" (no addendum) from "file missing" (fall
// through to embedded).
func FamilyPromptExists(dir, family string) bool {
	_, err := os.Stat(filepath.Join(dir, "prompts", familyFile(family)))
	return err == nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/system/... -run 'EmbeddedFamilyPrompt|LoadFamilyPrompt|FamilyPromptExists' -count=1
```
Expected: PASS.

**Step 5: Run full package tests**

```bash
go test ./internal/system/... -count=1
```
Expected: PASS. Existing seed tests remain green; subdir-seeding coverage lands in Task 4.

**Step 6: Commit**

```bash
git add internal/system/family.go internal/system/family_test.go internal/system/system.go
git commit -m "feat(system): add family prompt loaders and extend embed directive"
```

---

### Task 4: Seed verification for `prompts/` subdir

**Depends on: Task 3** (needs the extended embed directive so `Seed` can walk `defaults/prompts/*.md`).

**Files:**
- Modify: `internal/system/seed_test.go` (append test)

**Step 1: Write the failing test**

Append to `internal/system/seed_test.go`:

```go
func TestSeed_CreatesPromptsSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := Seed(dir, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, family := range []string{"claude", "minimax", "kimi-k2", "trinity", "gpt", "deepseek"} {
		path := filepath.Join(dir, "prompts", family+".md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s not seeded: %v", family, err)
			continue
		}
		if len(b) == 0 {
			t.Errorf("%s seeded empty", family)
		}
	}
	// Manifest has per-family hashes. Use the existing readManifest helper
	// defined at seed_test.go:11 to match house style.
	m := readManifest(t, dir)
	for _, family := range []string{"claude", "minimax", "kimi-k2", "trinity", "gpt", "deepseek"} {
		key := "prompts/" + family + ".md"
		if _, ok := m.Hashes[key]; !ok {
			t.Errorf("manifest missing %q", key)
		}
	}
}
```

Do NOT add a separate `TestSeed_PreservesEditedFamilyFile` — the existing `tools/` edit-preservation coverage already exercises the same code path (`fs.WalkDir` + manifest-hash comparison is not subdir-special-cased). Adding a prompts-specific variant duplicates coverage without new signal.

**Step 2: Run tests**

```bash
go test ./internal/system/... -run TestSeed -count=1
```
Expected: PASS (no implementation change required — existing `fs.WalkDir` handles subdir).

**Step 3: Commit**

```bash
git add internal/system/seed_test.go
git commit -m "test(system): cover prompts/ subdir seeding"
```

---

### Task 5: `Agent.SetSystem`

**Depends on: none** (independent of config/system tasks; can be parallelized with Tasks 0–4).

**Files:**
- Modify: `internal/agent/agent.go` (add method near line 258, after `SetProvider`)
- Create: `internal/agent/agent_set_test.go`

**Step 1: Locate existing agent test pattern**

```bash
ls internal/agent/*_test.go
```

Pick the file where `SetProvider` / `SetMaxIters` tests live, or create `internal/agent/agent_set_test.go` if none match.

**Step 2: Write the failing test**

```go
// internal/agent/agent_set_test.go (or append to existing test file)
package agent

import (
	"log/slog"
	"testing"
)

func TestSetSystem_UpdatesBaseAndEffective(t *testing.T) {
	// No skills registry — effective system == baseSystem after rebuild.
	a := &Agent{baseSystem: "old", system: "old", log: slog.Default()}
	a.SetSystem("new")
	if a.baseSystem != "new" {
		t.Fatalf("baseSystem: %q", a.baseSystem)
	}
	if a.system != "new" {
		t.Fatalf("effective system: %q", a.system)
	}
}

func TestSetSystem_ConcurrentSafe(t *testing.T) {
	a := &Agent{log: slog.Default()}
	done := make(chan struct{}, 2)
	go func() {
		for i := 0; i < 1000; i++ {
			a.SetSystem("a")
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 1000; i++ {
			a.SetSystem("b")
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	// Either "a" or "b" — just assert non-empty and no race panic.
	if a.baseSystem == "" {
		t.Fatal("empty baseSystem after concurrent writes")
	}
}
```

Run with `-race` to catch mutex regressions:

```bash
go test ./internal/agent/... -run TestSetSystem -race -count=1
```
Expected: FAIL — `a.SetSystem undefined`.

**Step 3: Add the method**

The agent stores the user-facing prompt in `baseSystem` and the effective prompt (base + skill catalog) in `system`. `SetSystem` must update `baseSystem` AND trigger the catalog rebuild so `system` reflects the new base. Re-use `RebuildSkillCatalog` rather than inlining its logic.

Append to `internal/agent/agent.go` immediately after `SetProvider` (around line 258):

```go
// SetSystem swaps the base system prompt and rebuilds the effective prompt
// (base + skill catalog, when the auto-invoke gate is on) for subsequent
// turns. Safe to call concurrently with turn execution.
func (a *Agent) SetSystem(s string) {
	a.mu.Lock()
	a.baseSystem = s
	a.mu.Unlock()
	a.RebuildSkillCatalog()
}
```

Note: the `a.mu.Unlock()` before `RebuildSkillCatalog` is deliberate — `RebuildSkillCatalog` acquires the same mutex at line 112.

**Step 4: Run tests**

```bash
go test ./internal/agent/... -run TestSetSystem -race -count=1
```
Expected: PASS.

**Step 5: Run full agent package tests**

```bash
go test ./internal/agent/... -race -count=1
```
Expected: PASS.

**Step 6: Commit**

```bash
git add internal/agent/
git commit -m "feat(agent): add SetSystem to swap system prompt mid-session"
```

---

### Task 6: `resolveSystemPrompt` in `cmd/sam/main.go` + integration tests

**Depends on: Tasks 0, 1, 3** (needs `config.DefaultPromptFamilies`, `config.FamilyForModel`, `system.EmbeddedFamilyPrompt`, `system.FamilyPromptExists`).

**Files:**
- Modify: `cmd/sam/main.go` (add function, replace prompt-loading block in `runTUI` and `runAgentOneShot`)
- Modify: `internal/system/integration_test.go` (append resolver tests)

**Step 1: Write the failing integration tests**

Append to `internal/system/integration_test.go`:

```go
// These tests exercise the main.go resolver indirectly by replicating its
// composition. Keeping the logic testable without shelling out requires
// either exposing resolveSystemPrompt or reproducing its steps here. Pick
// reproduction — main.go stays slim.

// resolveForTest mirrors main.go::resolveSystemPrompt without the logger
// argument. Keep the two in lockstep — any change to main's resolver must
// land here too, or these tests silently diverge from production.
func resolveForTest(cfg *config.Config, sysDir, model string) string {
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
		// Disk file present — use verbatim. Empty file = explicit user wipe,
		// do NOT fall back to embedded.
		add, _ = system.LoadFamilyPrompt(sysDir, family)
	} else {
		add = system.EmbeddedFamilyPrompt(family)
	}
	if add == "" {
		return base
	}
	return strings.TrimRight(base, "\n\t ") + "\n\n" + strings.TrimRight(add, "\n\t ")
}

func TestResolveSystemPrompt_OverrideTotal(t *testing.T) {
	dir := t.TempDir()
	override := filepath.Join(dir, "override.md")
	if err := os.WriteFile(override, []byte("OVERRIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		SystemPromptFile: override,
		PromptFamilies:   config.DefaultPromptFamilies(),
	}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	if got != "OVERRIDE" {
		t.Fatalf("override ignored: %q", got)
	}
}

func TestResolveSystemPrompt_BaseOnly_UnknownModel(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "random-model-x")
	if got != system.EmbeddedPrompt() {
		t.Fatalf("expected embedded base only, got %q", got)
	}
}

func TestResolveSystemPrompt_BasePlusFamily_Embedded(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	want := strings.TrimRight(system.EmbeddedPrompt(), "\n\t ") + "\n\n" + strings.TrimRight(system.EmbeddedFamilyPrompt("claude"), "\n\t ")
	if got != want {
		t.Fatalf("composition mismatch")
	}
}

func TestResolveSystemPrompt_DiskFamilyOverridesEmbedded(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompts", "claude.md"), []byte("DISK FAMILY"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	if !strings.HasSuffix(got, "\n\nDISK FAMILY") {
		t.Fatalf("disk family not applied: %q", got)
	}
}

func TestResolveSystemPrompt_EmptyFamilyFileMeansNoAppend(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Present-but-empty = explicit user wipe. Expect base only; must NOT
	// fall back to embedded family content.
	if err := os.WriteFile(filepath.Join(dir, "prompts", "claude.md"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	if got != system.EmbeddedPrompt() {
		t.Fatalf("expected base only, got %q", got)
	}
	if strings.Contains(got, "CLAUDE FAMILY") {
		t.Fatal("embedded family leaked through empty disk file")
	}
}

func TestResolveSystemPrompt_MissingFamilyFileFallsBackToEmbedded(t *testing.T) {
	dir := t.TempDir()
	// No prompts/ subdir at all — missing, not empty. Expect embedded family.
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	if !strings.Contains(got, "CLAUDE FAMILY") {
		t.Fatalf("embedded family should apply when disk file missing: %q", got)
	}
}

func TestResolveSystemPrompt_JoinFormatExactlyOneBlankLine(t *testing.T) {
	dir := t.TempDir()
	// Base file with trailing newlines — resolver must TrimRight before join.
	if err := os.WriteFile(filepath.Join(dir, "system-prompt.md"), []byte("BASE\n\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Family file with leading + trailing whitespace. LoadFamilyPrompt
	// TrimRight's trailing; leading is preserved (rare in practice).
	if err := os.WriteFile(filepath.Join(dir, "prompts", "claude.md"), []byte("FAM\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{PromptFamilies: config.DefaultPromptFamilies()}
	got := resolveForTest(cfg, dir, "claude-sonnet-4-5")
	want := "BASE\n\nFAM"
	if got != want {
		t.Fatalf("join format wrong: got %q want %q", got, want)
	}
}
```

Imports required: `"path/filepath"`, `"strings"`, plus the existing `cfg` + `system` imports. Confirm against current file header.

**Step 2: Run tests to confirm they fail**

```bash
go test ./internal/system/... -run TestResolveSystemPrompt -count=1
```
Expected: FAIL — `config.DefaultPromptFamilies` / `config.FamilyForModel` / `system.EmbeddedFamilyPrompt` references work only after prior tasks; if tests fail due to composition mismatch, that drives the main.go implementation.

**Step 3: Implement resolver in `cmd/sam/main.go`**

Add `"strings"` to imports if not already present.

Add function after `buildRegistry` (around line 122):

```go
// resolveSystemPrompt builds the effective system prompt for a given model.
// Total-override (cfg.SystemPromptFile) short-circuits family composition.
// Otherwise base (disk > embedded) is joined with family addendum
// (disk > embedded) via a single blank line.
func resolveSystemPrompt(cfg *config.Config, sysDir, model string, logger *slog.Logger) string {
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
		if logger != nil {
			logger.Info("system: family resolved", "model", model, "family", "", "source", "none")
		}
		return base
	}
	var add, source string
	if system.FamilyPromptExists(sysDir, family) {
		// Disk file present — use verbatim. Empty disk file = explicit user
		// wipe, do NOT fall back to embedded.
		add, _ = system.LoadFamilyPrompt(sysDir, family)
		source = "disk"
	} else {
		add = system.EmbeddedFamilyPrompt(family)
		source = "embedded"
	}
	if add == "" {
		if logger != nil {
			logger.Debug("system: family resolved with no content", "family", family, "source", source)
		}
		return base
	}
	if logger != nil {
		logger.Info("system: family resolved", "model", model, "family", family, "source", source)
	}
	return strings.TrimRight(base, "\n\t ") + "\n\n" + strings.TrimRight(add, "\n\t ")
}
```

Replace both existing prompt-loading blocks:

- `runTUI` (lines 131–135):
  ```go
  sys := resolveSystemPrompt(cfg, sysDir, cfg.Model, logger)
  ```
- `runAgentOneShot` (lines 200–204):
  ```go
  sys := resolveSystemPrompt(cfg, sysDir, cfg.Model, logger)
  ```

**Step 4: Run tests**

```bash
go test ./internal/system/... -run TestResolveSystemPrompt -count=1
go build ./...
```
Expected: PASS + clean build.

**Step 5: Commit**

```bash
git add cmd/sam/main.go internal/system/integration_test.go
git commit -m "feat(main): add resolveSystemPrompt and wire family composition"
```

---

### Task 7: Thread `SystemResolverFn` through TUI

**Depends on: Task 6** (closure in `main.go` calls `resolveSystemPrompt`).

**Files:**
- Modify: `internal/tui/app.go` (extend `Options` struct ~line 205, extend `Model` struct ~line 95, store in `New`)
- Modify: `cmd/sam/main.go` (pass closure in `tui.New` call ~line 154)

**Step 1: Write the failing test**

Add to a TUI test file (e.g., create `internal/tui/system_resolver_test.go`):

```go
package tui

import "testing"

func TestOptions_SystemResolverFn_DefaultNil(t *testing.T) {
	var opts Options
	if opts.SystemResolverFn != nil {
		t.Fatal("expected nil default")
	}
}

func TestOptions_SystemResolverFn_InvokesClosure(t *testing.T) {
	called := ""
	opts := Options{SystemResolverFn: func(m string) string {
		called = m
		return "RESOLVED:" + m
	}}
	got := opts.SystemResolverFn("claude-sonnet-4-5")
	if called != "claude-sonnet-4-5" || got != "RESOLVED:claude-sonnet-4-5" {
		t.Fatalf("closure misbehaved: called=%q got=%q", called, got)
	}
}
```

**Step 2: Run tests to confirm they fail**

```bash
go test ./internal/tui/... -run TestOptions_SystemResolverFn -count=1
```
Expected: FAIL — `SystemResolverFn undefined`.

**Step 3: Implementation**

Edit `internal/tui/app.go` `Options` struct (around line 205):

```go
type Options struct {
	Provider         string
	Model            string
	MaxIter          int
	ProviderFactory  ProviderFactory
	ContextWindowFn  func(model string) int
	SystemResolverFn func(model string) string
	Providers        map[string]config.ProviderEntry
}
```

Edit `Model` struct (around line 106) — add `sysResolveFn` right after `ctxWinFn`:

```go
factory       ProviderFactory
ctxWinFn      func(model string) int
sysResolveFn  func(model string) string
providers     map[string]config.ProviderEntry
```

In `New(...)` constructor at `internal/tui/app.go:252-270`, add `sysResolveFn` in the struct literal immediately after the existing `ctxWinFn:  opts.ContextWindowFn,` line (around line 269):

```go
factory:      opts.ProviderFactory,
ctxWinFn:     opts.ContextWindowFn,
sysResolveFn: opts.SystemResolverFn,
providers:    opts.Providers,
```

Edit `cmd/sam/main.go` `tui.New(...)` call (around line 154) — add:

```go
SystemResolverFn: func(m string) string {
	return resolveSystemPrompt(cfg, sysDir, m, logger)
},
```

**Step 4: Run tests + build**

```bash
go test ./internal/tui/... -run TestOptions_SystemResolverFn -count=1
go build ./...
```
Expected: PASS + clean build.

**Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/system_resolver_test.go cmd/sam/main.go
git commit -m "feat(tui): thread SystemResolverFn option through Model"
```

---

### Task 8: Wire `SetSystem` into all three model-change paths

**Depends on: Tasks 5, 7** (needs `Agent.SetSystem` + `Model.sysResolveFn`).

**Files:**
- Modify: `internal/tui/provider_cmd.go` (two call sites: `switchProvider` ~line 47, `applyModelSpec` ~line 23)
- Modify: `internal/tui/settings_modal.go` (`applyProviders` ~line 382)
- Create: `internal/tui/model_switch_system_test.go`

**Step 1: Write the failing tests**

Tests use `fake.LastSystem()` rather than adding an `Agent.System()` accessor — the fake provider at `internal/llm/fake/provider.go:29` already captures the last `Stream` request's system string, and exercising a real `Submit` covers the agent→provider round-trip. Avoids adding a test-only public API.

Create `internal/tui/model_switch_system_test.go`:

```go
package tui

import (
	"context"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/agent"
	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/llm"
	"github.com/stefanfaur/sam/internal/llm/fake"
	"github.com/stefanfaur/sam/internal/policy"
	"github.com/stefanfaur/sam/internal/tools"
)

// drainOneTurn runs a Submit so the provider captures the current system.
func drainOneTurn(t *testing.T, a *agent.Agent) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch := a.Submit(ctx, "ping")
	for range ch {
		// drain
	}
}

func newTestModelWithResolver(t *testing.T, resolved map[string]string) (*Model, *agent.Agent, map[string]*fake.Provider) {
	t.Helper()
	startProv := fake.New()
	a := agent.New(agent.Options{
		Provider: startProv,
		Tools:    tools.NewRegistry(),
		Policy:   policy.AllowAll(),
		System:   "START",
		Model:    "starter",
		MaxIters: 1,
	})
	a.Start()
	t.Cleanup(func() { a.Close() })

	providers := map[string]config.ProviderEntry{
		"anthropic": {Name: "anthropic", Wire: "anthropic", DefaultModel: "claude-sonnet-4-5"},
		"openai":    {Name: "openai", Wire: "openai", DefaultModel: "gpt-4o-mini"},
	}
	built := map[string]*fake.Provider{}
	factory := func(name, model string) (llm.Provider, error) {
		p := fake.New()
		built[name+"/"+model] = p
		return p, nil
	}
	m := New(a, nil, Options{
		Provider:         "anthropic",
		Model:            "claude-sonnet-4-5",
		MaxIter:          1,
		ProviderFactory:  factory,
		ContextWindowFn:  func(_ string) int { return 200_000 },
		SystemResolverFn: func(model string) string { return resolved[model] },
		Providers:        providers,
	})
	m.status.provider = "anthropic"
	m.status.model = "claude-sonnet-4-5"
	return m, a, built
}

func TestSwitchProvider_UpdatesSystemPrompt(t *testing.T) {
	resolved := map[string]string{
		"claude-sonnet-4-5": "SYS-CLAUDE",
		"gpt-4o-mini":       "SYS-GPT",
	}
	m, a, built := newTestModelWithResolver(t, resolved)
	_ = m.switchProvider("openai", "")
	drainOneTurn(t, a)
	prov := built["openai/gpt-4o-mini"]
	if prov == nil {
		t.Fatal("openai provider not built")
	}
	if got := prov.LastSystem(); got != "SYS-GPT" {
		t.Fatalf("system not updated after switchProvider: %q", got)
	}
}

func TestApplyModelSpec_SameProviderUpdatesSystem(t *testing.T) {
	resolved := map[string]string{
		"claude-sonnet-4-5": "SYS-SONNET",
		"claude-opus-4-7":   "SYS-OPUS",
	}
	// Seed the fake so the initial provider captures the first turn.
	m, a, _ := newTestModelWithResolver(t, resolved)
	// Same-provider switch uses existing provider; grab it before switch.
	startProv, ok := currentProvider(a).(*fake.Provider)
	if !ok {
		t.Fatal("current provider is not *fake.Provider")
	}
	_ = m.applyModelSpec("claude-opus-4-7")
	drainOneTurn(t, a)
	if got := startProv.LastSystem(); got != "SYS-OPUS" {
		t.Fatalf("system not updated after applyModelSpec: %q", got)
	}
}
```

Note: `currentProvider(a)` depends on an accessor. If `internal/agent` does not already expose "current provider", use the simpler route: in `newTestModelWithResolver`, return the `startProv` directly alongside `built`, and reference that in `TestApplyModelSpec_SameProviderUpdatesSystem`. Adjust helper signature accordingly. Do not add a public `Agent.CurrentProvider()` solely for tests.

Revised helper shape:

```go
func newTestModelWithResolver(t *testing.T, resolved map[string]string) (*Model, *agent.Agent, *fake.Provider, map[string]*fake.Provider)
```

Returning `(m, a, startProv, built)`. Same-provider test uses `startProv`; cross-provider test uses `built["openai/gpt-4o-mini"]`.

**Step 2: Run tests to confirm they fail**

```bash
go test ./internal/tui/... -run 'SwitchProvider_UpdatesSystemPrompt|ApplyModelSpec_SameProviderUpdatesSystem' -count=1
```
Expected: FAIL — `LastSystem()` returns `""` (empty) or `"START"` because `SetSystem` was never called after the switch.

**Step 3: Implementation**

In `internal/tui/provider_cmd.go::switchProvider`, lines 46–51 currently read:

```go
m.agent.SetProvider(p)
m.agent.SetModel(model)
m.status.provider = name
m.status.model = model
m.persistSelection()
```

Change to:

```go
m.agent.SetProvider(p)
m.agent.SetModel(model)
if m.sysResolveFn != nil {
	m.agent.SetSystem(m.sysResolveFn(model))
}
m.status.provider = name
m.status.model = model
m.persistSelection()
```

In `internal/tui/provider_cmd.go::applyModelSpec`, lines 23–25 currently read:

```go
m.agent.SetModel(model)
m.status.model = model
m.persistSelection()
```

Change to:

```go
m.agent.SetModel(model)
if m.sysResolveFn != nil {
	m.agent.SetSystem(m.sysResolveFn(model))
}
m.status.model = model
m.persistSelection()
```

In `internal/tui/settings_modal.go::applyProviders`, lines 382–386 currently read:

```go
root.agent.SetProvider(p)
root.agent.SetModel(model)
root.status.provider = m.provider
root.status.model = model
root.persistSelection()
```

Change to:

```go
root.agent.SetProvider(p)
root.agent.SetModel(model)
if root.sysResolveFn != nil {
	root.agent.SetSystem(root.sysResolveFn(model))
}
root.status.provider = m.provider
root.status.model = model
root.persistSelection()
```

Every call site preserves `m.persistSelection()` (or `root.persistSelection()`) after the new `SetSystem` block. Do NOT drop it.

**Step 4: Run tests + build**

```bash
go test ./internal/tui/... -count=1 -race
go test ./internal/agent/... -count=1 -race
go build ./...
```
Expected: PASS + clean build.

**Step 5: Commit**

```bash
git add internal/agent/agent.go internal/tui/provider_cmd.go internal/tui/settings_modal.go internal/tui/model_switch_system_test.go
git commit -m "feat(tui): recompute system prompt on every model switch path"
```

---

### Task 9: End-to-end smoke test (build + seed)

**Depends on: all prior tasks (0–8)** — verifies the full stack integrates.

**Files:** no code changes — verification only.

**Step 1: Build**

```bash
go build -o /tmp/sam ./cmd/sam
```
Expected: clean build, binary produced.

**Step 2: Full test suite with race detector**

```bash
go test ./... -race -count=1
```
Expected: PASS.

**Step 3: Smoke-run seed via a one-line Go program**

`cmd/sam/main.go` runs `system.Seed` unconditionally after `flag.Parse` (line 49). `--help` exits before `Seed` fires, so don't rely on it. Instead, exercise `Seed` directly:

```bash
SEED_DIR=$(mktemp -d)
go run -exec '' /dev/stdin <<EOF
package main

import (
	"fmt"
	"github.com/stefanfaur/sam/internal/system"
)

func main() {
	if err := system.Seed("$SEED_DIR/system", nil); err != nil {
		fmt.Println("seed error:", err); return
	}
	fmt.Println("seeded into $SEED_DIR/system")
}
EOF
ls -la "$SEED_DIR/system/prompts/"
```

Expected: all six `*.md` files present, each non-empty.

**Step 4: Optional one-shot invocation (skip unless a provider API key is set)**

```bash
# Only if ANTHROPIC_API_KEY is set:
ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" /tmp/sam -provider anthropic -model claude-sonnet-4-5 -p "say hi"
```

Expected: agent responds; log file at `~/.local/share/sam/sam.log` (or XDG equivalent) should contain `system: family resolved model=claude-sonnet-4-5 family=claude source=embedded`. If no API key, skip.

**Step 5: No commit** — verification task.

---

### Task 10: README update

**Depends on: none** (documentation only; can land any time after Tasks 0–3 have defined the user-visible surface).

**Files:**
- Modify: `README.md`

**Step 1: Draft doc section**

Add a new section (between existing system-prompt and provider-config sections — exact placement based on current README structure, check before editing):

```markdown
## Per-Model System Prompts

SAM ships a universal base prompt plus per-family addenda tailored to each model's capabilities. Family is resolved from the current model name via longest-prefix match.

### Precedence

1. `--system-prompt-file` / `config.system_prompt_file` — total override, no family addendum.
2. Disk base `~/.sam/system/system-prompt.md` (seeded on first run).
3. Embedded base.

When override is NOT set, the matching family addendum is appended:

4. Disk family `~/.sam/system/prompts/<family>.md`.
5. Embedded family.

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

### Adding a Custom Family

To add a qwen family served by OpenRouter or LM Studio:

```toml
# ~/.config/sam/config.toml
[prompt_families.qwen]
prefixes = ["qwen-", "Qwen", "openrouter/qwen/", "lmstudio-community/qwen"]
```

Drop the content file at `~/.sam/system/prompts/qwen.md`. Point SAM at a qwen model. The family addendum auto-applies.

Override a bundled family:

```toml
[prompt_families.claude]
prefixes = ["claude-opus", "claude-sonnet", "claude-haiku", "claude-5"]
```

Disable a bundled family:

```toml
[prompt_families.claude]
prefixes = []
```

Prefix match is case-sensitive — declare variants explicitly.
```

**Step 2: Verify doc renders**

Open README in an editor, confirm no broken markdown (tables, fenced blocks).

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document per-model system prompt families and extension flow"
```

---

## Post-implementation verification

Use `verification-before-completion`:

```bash
go test ./... -race -count=1           # all green
go vet ./...                           # no warnings
go build ./...                         # clean build

# Spot-check seeded prompts dir via direct Seed call (--help exits before seeding)
SEED_DIR=$(mktemp -d)
go run -exec '' /dev/stdin <<'EOF' > /dev/null
package main
import "github.com/stefanfaur/sam/internal/system"
func main() { _ = system.Seed("$SEED_DIR/system", nil) }
EOF
ls "$SEED_DIR/system/prompts/"         # six files
```

All must pass with no regressions before declaring done.

## File inventory summary

**New:**
- `internal/config/prompt_families.go`
- `internal/config/prompt_families_test.go`
- `internal/system/family.go`
- `internal/system/family_test.go`
- `internal/system/defaults/prompts/claude.md`
- `internal/system/defaults/prompts/minimax.md`
- `internal/system/defaults/prompts/kimi-k2.md`
- `internal/system/defaults/prompts/trinity.md`
- `internal/system/defaults/prompts/gpt.md`
- `internal/system/defaults/prompts/deepseek.md`
- `internal/agent/agent_set_test.go`
- `internal/tui/system_resolver_test.go`
- `internal/tui/model_switch_system_test.go`

**Modified:**
- `internal/config/config.go` — add `PromptFamilies` to `Config` + `rawConfig`, seed in `Load`, add merge loop, add `FamilyForModel` method.
- `internal/system/system.go` — extend `//go:embed` directive.
- `internal/system/seed_test.go` — add `prompts/` subdir seed test (single test, re-uses existing `readManifest` helper).
- `internal/system/integration_test.go` — add `resolveForTest` + resolver integration tests.
- `internal/agent/agent.go` — add `SetSystem` (delegates to `RebuildSkillCatalog`). No read-accessor — tests use `fake.Provider.LastSystem()`.
- `internal/tui/app.go` — add `SystemResolverFn` to `Options`, store `sysResolveFn` on `Model`.
- `internal/tui/provider_cmd.go` — call `SetSystem` in `switchProvider` (line 47 area) and `applyModelSpec` (line 23 area).
- `internal/tui/settings_modal.go` — call `SetSystem` in `applyProviders` (line 382 area).
- `cmd/sam/main.go` — add `resolveSystemPrompt`, use in `runTUI` + `runAgentOneShot`, pass `SystemResolverFn` closure to `tui.New`.
- `README.md` — document per-family prompts + extension.
