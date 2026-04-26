# Status Bar Redesign & Settings Modal — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use executing-plans to implement this plan task-by-task.

**Goal:** Deliver the status bar + settings modal described in `2026-04-22-statusbar-and-settings-design.md`. Move status bar below the input, split rendering into segments, build a tabbed `/settings` modal with Statusline/Providers/Theme tabs, plumb token usage from providers, and persist user prefs to `settings.toml`.

**Tech Stack:** Go 1.26.2, Bubbletea 1.3.10, Glamour 1.0.0, Lipgloss 1.1.1, Huh (forms), BurntSushi/toml.

**Landing order rationale:** Infrastructure first (settings struct, theme engine, token plumbing), then UI (segments, statusbar, modal) — each step compiles and is independently testable. Theme refactor lands early to surface render-site breakage before modal work depends on it.

---

## Task 1: Settings persistence layer

**Files:**
- Create: `internal/tui/settings.go`
- Create: `internal/tui/settings_test.go`

**Step 1: Write `settings.go`**

```go
package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Settings struct {
	Statusbar StatusbarSettings `toml:"statusbar"`
	Theme     ThemeSettings     `toml:"theme"`
}

type StatusbarSettings struct {
	Enabled  bool               `toml:"enabled"`
	Position string             `toml:"position"` // "below" only for v1
	Layout   string             `toml:"layout"`   // "one-line" | "two-line"
	Spinner  bool               `toml:"spinner"`
	Colors   bool               `toml:"colors"`
	Elapsed  bool               `toml:"elapsed"`
	Segments StatusbarSegments  `toml:"segments"`
}

type StatusbarSegments struct {
	State      bool `toml:"state"`
	Model      bool `toml:"model"`
	Provider   bool `toml:"provider"`
	Cwd        bool `toml:"cwd"`
	Git        bool `toml:"git"`
	Iterations bool `toml:"iterations"`
	Context    bool `toml:"context"`
	Tokens     bool `toml:"tokens"`
	Keybinds   bool `toml:"keybinds"`
}

type ThemeSettings struct {
	GlamourStyle      string `toml:"glamour_style"`
	Accent            string `toml:"accent"`
	Muted             string `toml:"muted"`
	UserBorder        string `toml:"user_border"`
	AssistantFg       string `toml:"assistant_fg"`
	ErrorFg           string `toml:"error_fg"`
	StateThinking     string `toml:"state_thinking"`
	StateResponding   string `toml:"state_responding"`
	StateTool         string `toml:"state_tool"`
	StateError        string `toml:"state_error"`
	StateApproval     string `toml:"state_approval"`
}

func DefaultSettings() Settings {
	return Settings{
		Statusbar: StatusbarSettings{
			Enabled:  true,
			Position: "below",
			Layout:   "two-line",
			Spinner:  true,
			Colors:   true,
			Elapsed:  true,
			Segments: StatusbarSegments{
				State: true, Model: true, Provider: true,
				Cwd: true, Git: true, Iterations: true,
				Context: true, Tokens: true, Keybinds: true,
			},
		},
		Theme: ThemeSettings{
			GlamourStyle:    "dark",
			Accent:          "#6366f1",
			Muted:           "#737373",
			UserBorder:      "#8b5cf6",
			AssistantFg:     "",
			ErrorFg:         "#ef4444",
			StateThinking:   "#60a5fa",
			StateResponding: "#34d399",
			StateTool:       "#fbbf24",
			StateError:      "#f87171",
			StateApproval:   "#a78bfa",
		},
	}
}

func settingsPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sam", "settings.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sam", "settings.toml")
}

func LoadSettings() Settings {
	s := DefaultSettings()
	path := settingsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	// Unmarshal over defaults — sparse TOML keeps defaults
	_ = toml.Unmarshal(data, &s)
	validate(&s)
	return s
}

func SaveSettings(s Settings) error {
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(s)
}

// validate fixes up unknown enum values to defaults; silent (no error).
func validate(s *Settings) {
	if s.Statusbar.Layout != "one-line" && s.Statusbar.Layout != "two-line" {
		s.Statusbar.Layout = "two-line"
	}
	if s.Statusbar.Position != "below" {
		s.Statusbar.Position = "below"
	}
}
```

**Step 2: Write `settings_test.go`**

Cover:
- `DefaultSettings()` — required fields populated
- `LoadSettings()` when file missing → defaults
- `SaveSettings → LoadSettings` round-trip equality
- Sparse TOML fills defaults (e.g. file with only `[statusbar] layout = "one-line"`)
- Unknown `layout` value is normalized to `"two-line"`

Use `t.Setenv("XDG_CONFIG_HOME", tmpDir)` to isolate test files.

**Step 3: Verify**

```bash
go test ./internal/tui -run TestSettings -v
```

**Step 4: Commit**

```
feat(tui): add settings persistence layer for ~/.config/sam/settings.toml
```

---

## Task 2: Theme engine

**Files:**
- Create: `internal/tui/theme.go`
- Modify: `internal/tui/styles.go`
- Modify: `internal/tui/app.go`, `render.go`, `update.go`, `statusbar.go`, `forms.go`

**Step 1: Create `theme.go`**

```go
package tui

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	// raw colors
	Accent, Muted, UserBorder, ErrorFg   lipgloss.Color
	AssistantFg                           lipgloss.Color
	StateThinking, StateResponding       lipgloss.Color
	StateTool, StateError, StateApproval lipgloss.Color
	GlamourStyle                         string

	// derived styles — rebuilt by Apply()
	StatusBar           lipgloss.Style
	InputBox            lipgloss.Style
	InputBoxFocus       lipgloss.Style
	InputPrompt         lipgloss.Style
	UserMsg             lipgloss.Style
	Info                lipgloss.Style
	ToolHeader          lipgloss.Style
	ToolInput           lipgloss.Style
	ToolResult          lipgloss.Style
	ToolError           lipgloss.Style
	DebugPanel          lipgloss.Style
	Thinking            lipgloss.Style
	ThinkingHeader      lipgloss.Style
	Suggest             lipgloss.Style
	SuggestSelected     lipgloss.Style

	glam *glamour.TermRenderer
}

func NewTheme(t ThemeSettings) *Theme {
	th := &Theme{
		Accent:          parseColor(t.Accent, "63"),
		Muted:           parseColor(t.Muted, "244"),
		UserBorder:      parseColor(t.UserBorder, "63"),
		AssistantFg:     parseColor(t.AssistantFg, ""),
		ErrorFg:         parseColor(t.ErrorFg, "160"),
		StateThinking:   parseColor(t.StateThinking, "39"),
		StateResponding: parseColor(t.StateResponding, "42"),
		StateTool:       parseColor(t.StateTool, "214"),
		StateError:      parseColor(t.StateError, "160"),
		StateApproval:   parseColor(t.StateApproval, "141"),
		GlamourStyle:    fallback(t.GlamourStyle, "dark"),
	}
	th.Apply(80)
	return th
}

func (t *Theme) Apply(width int) {
	t.UserMsg = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), false, false, false, true).
		BorderForeground(t.UserBorder).
		Padding(0, 1).MarginBottom(1).Bold(true)

	t.ToolHeader = lipgloss.NewStyle().Foreground(t.Muted)
	t.ToolInput = lipgloss.NewStyle().Foreground(t.Muted).Italic(true)
	t.ToolResult = lipgloss.NewStyle().Foreground(t.Muted)
	t.ToolError = lipgloss.NewStyle().Foreground(t.ErrorFg)
	t.StatusBar = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252"))
	t.DebugPanel = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).BorderForeground(t.Accent).Padding(1)
	t.Info = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Italic(true)
	t.Thinking = lipgloss.NewStyle().
		Foreground(t.Muted).Italic(true).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("238")).Padding(0, 1)
	t.ThinkingHeader = lipgloss.NewStyle().Foreground(t.Muted).Bold(true).Italic(true)
	t.Suggest = lipgloss.NewStyle().Foreground(t.Muted)
	t.SuggestSelected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Bold(true)
	t.InputPrompt = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	t.InputBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(t.Muted).Padding(0, 1)
	t.InputBoxFocus = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(t.Accent).Padding(0, 1)

	t.glam, _ = glamour.NewTermRenderer(
		glamour.WithStandardStyle(t.GlamourStyle),
		glamour.WithWordWrap(width),
	)
}

func (t *Theme) Glamour() *glamour.TermRenderer { return t.glam }

func parseColor(s, dflt string) lipgloss.Color {
	if s == "" {
		return lipgloss.Color(dflt)
	}
	return lipgloss.Color(s)
}

func fallback(s, d string) string {
	if s == "" { return d }
	return s
}
```

**Step 2: Shrink `styles.go`**

Remove all `var` style blocks — they now live on `Theme`. Keep only `glamourForWidth` helper (re-export via `Theme.Apply(width)` if needed; remove the standalone helper after audit).

**Step 3: Wire `Theme` into `Model`**

In `app.go`:
- Add `settings Settings` and `theme *Theme` fields to `Model`
- In `New()`: `settings := LoadSettings(); theme := NewTheme(settings.Theme)`; store both; use `theme.Glamour()` instead of building `glam` inline; drop `Model.glam` field (read via `m.theme.Glamour()`)

**Step 4: Replace all global style refs**

Grep for `userMsgStyle`, `toolHeaderStyle`, `toolInputStyle`, `toolResultStyle`, `toolErrorStyle`, `statusBarStyle`, `debugPanelStyle`, `infoStyle`, `thinkingStyle`, `thinkingHeaderStyle`, `suggestStyle`, `suggestSelectedStyle`, `inputPromptStyle`, `inputBoxStyle`, `inputBoxFocusStyle`. Replace each with `m.theme.<Field>` at call sites. Pass `*Theme` into pure render helpers (`renderToolCall`, `renderToolResult`, `renderError`, `renderThinkingCard`, `renderUserMsg`).

**Step 5: Update resize path**

In `resize()`: after width changes, call `m.theme.Apply(w)` to rebuild glamour with the new word-wrap width.

**Step 6: Verify**

```bash
go build ./... && go test ./internal/tui
```

Expected: build passes, existing tests still pass, visual output unchanged from defaults.

**Step 7: Commit**

```
refactor(tui): move styles into Theme engine with runtime-mutable palette
```

---

## Task 3: Git probe

**Files:**
- Create: `internal/tui/gitinfo.go`
- Create: `internal/tui/gitinfo_test.go`
- Modify: `app.go`

**Step 1: Write `gitinfo.go`**

```go
package tui

import (
	"os/exec"
	"strings"
)

type gitInfo struct {
	branch string
	dirty  bool
}

func probeGit(dir string) gitInfo {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return gitInfo{}
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return gitInfo{}
	}
	status, _ := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	return gitInfo{branch: branch, dirty: len(strings.TrimSpace(string(status))) > 0}
}
```

**Step 2: Write `gitinfo_test.go`**

Cover:
- Non-git directory → empty struct
- Clean repo (init + commit) → `branch = "main"` or `"master"`, `dirty = false`
- Dirty repo (touch untracked file) → `dirty = true`

Use `t.TempDir()` and exec git commands inline in test.

**Step 3: Wire into `Model`**

In `app.go`, add `git gitInfo` field. In `New()` (or defer to first `resize` since LaunchDir is known): `m.git = probeGit(a.LaunchDir())`.

**Step 4: Verify and commit**

```bash
go test ./internal/tui -run TestProbeGit -v
```

```
feat(tui): startup git branch + dirty probe
```

---

## Task 4: Spinner state + tick

**Files:**
- Create: `internal/tui/spinner.go`
- Create: `internal/tui/spinner_test.go`
- Modify: `messages.go`, `update.go`, `app.go`

**Step 1: Write `spinner.go`**

```go
package tui

import "time"

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerTick = 80 * time.Millisecond

type spinnerState struct {
	frame int
	on    bool
}

func (s *spinnerState) advance() { s.frame = (s.frame + 1) % len(spinnerFrames) }
func (s *spinnerState) glyph() string {
	if !s.on {
		return ""
	}
	return spinnerFrames[s.frame]
}
```

**Step 2: Add `spinnerTickMsg`**

In `messages.go`: `type spinnerTickMsg struct{}`.

**Step 3: Wire tick in `update.go`**

- Add helper `func (m *Model) startSpinner() tea.Cmd` that sets `m.spinner.on = true` and returns `tea.Tick(spinnerTick, func(time.Time) tea.Msg { return spinnerTickMsg{} })`
- In `startTurn()` (after building `pending`): chain `m.startSpinner()` into the returned `tea.Sequence`
- Add case `spinnerTickMsg` in `Update()`: if `m.pending == nil`, set `m.spinner.on = false`, return `m, nil`. Else `m.spinner.advance()`, return `m, tea.Tick(...)` for next tick.
- In `TurnDone`, `ErrorEvent`, Ctrl-C cancel, `turnClosedMsg`: set `m.spinner.on = false`

**Step 4: Add `turnStart time.Time` to `Model`**

Set `m.turnStart = time.Now()` in `startTurn`. Cleared on turn end.

**Step 5: Write `spinner_test.go`**

Trivial unit tests: advance wraps, glyph respects `on` flag.

**Step 6: Verify and commit**

```bash
go test ./internal/tui -run TestSpinner -v && go build ./...
```

```
feat(tui): spinner state + 80ms tick gated to active turns
```

---

## Task 5: Model context window config

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/context_test.go`

**Step 1: Add `[models]` table**

```go
type ModelConfig struct {
	ContextWindow int `toml:"context_window"`
}

type Config struct {
	// ...existing fields...
	Models map[string]ModelConfig `toml:"models"`
}

// ModelContextWindow returns the configured context window in tokens,
// falling back to 128_000 for unknown models.
func (c *Config) ModelContextWindow(name string) int {
	if m, ok := c.Models[name]; ok && m.ContextWindow > 0 {
		return m.ContextWindow
	}
	// Sensible hardcoded defaults for known families
	switch {
	case strings.HasPrefix(name, "claude-opus"), strings.HasPrefix(name, "claude-sonnet"):
		return 200_000
	case strings.HasPrefix(name, "claude-haiku"):
		return 200_000
	case strings.HasPrefix(name, "MiniMax-M2"), strings.HasPrefix(name, "MiniMax-M1"):
		return 1_000_000
	}
	return 128_000
}
```

**Step 2: Add tests**

Hardcoded defaults + user override + unknown → 128k.

**Step 3: Commit**

```
feat(config): [models] table + ModelContextWindow helper
```

---

## Task 6: LLM usage plumbing

**Files:**
- Modify: `internal/agent/agent.go` (new event)
- Modify: `internal/agent/loop.go` (emit)
- Modify: `internal/llm/*/client.go` (parse + return usage)

**Step 1: Add `UsageEvent` to agent**

```go
type UsageEvent struct {
	InputTokens        int
	OutputTokens       int
	CacheReadInput     int
	CacheCreationInput int
}
```

Register in the event-type switch inside loop (wherever events are fan-out).

**Step 2: Modify provider interface**

`internal/llm/provider.go` (or wherever `Provider` is defined): the complete-response type gains usage fields. Example:

```go
type Usage struct {
	InputTokens, OutputTokens, CacheReadInput, CacheCreationInput int
}

type CompletionResponse struct {
	// existing fields
	Usage Usage
}
```

If the Provider streams, surface Usage via a terminal event on the stream channel (most providers send usage in the final event).

**Step 3: Anthropic client**

In `internal/llm/anthropiccompat/client.go`: parse `usage.input_tokens`, `usage.output_tokens`, `usage.cache_read_input_tokens`, `usage.cache_creation_input_tokens` (all optional). Include in final response / emit as trailing event.

**Step 4: Minimax client**

Find all `internal/llm/minimax*/client.go`. Parse `usage.prompt_tokens` → InputTokens, `usage.completion_tokens` → OutputTokens; cache fields zero.

**Step 5: Agent loop forwards**

In `loop.go`: after successful LLM call, emit `UsageEvent` on the events channel before continuing to tool dispatch.

**Step 6: TUI accumulates**

In `update.go handleAgentEvent`, add:

```go
case agent.UsageEvent:
	m.status.lastIterIn = ev.InputTokens
	m.status.turnIn += ev.InputTokens
	m.status.turnOut += ev.OutputTokens
	m.status.turnCacheRead += ev.CacheReadInput
	m.status.sessionIn += ev.InputTokens
	m.status.sessionOut += ev.OutputTokens
	return m, waitAgent(m.pending.events)
```

Reset `turnIn/turnOut/turnCacheRead` in `startTurn`. Reset session fields + `lastIterIn` in `/reset`.

**Step 7: Tests**

- Mock provider that emits known usage → assert accumulators update correctly
- `/reset` clears all fields

**Step 8: Commit**

```
feat(llm): parse provider usage + forward UsageEvent to TUI
```

---

## Task 7: Segment catalogue

**Files:**
- Create: `internal/tui/segments.go`
- Create: `internal/tui/segments_test.go`

**Step 1: Write `segments.go`**

One function per segment. Signature:

```go
type segment struct {
	text     string // already styled
	priority int    // 1 = drop last; higher = drop first
	visible  bool
}

func segState(m *Model) segment
func segSpinner(m *Model) segment // merged inline into state rendering
func segElapsed(m *Model) segment
func segIter(m *Model) segment
func segModel(m *Model) segment
func segProvider(m *Model) segment
func segCwd(m *Model) segment
func segGit(m *Model) segment
func segCtx(m *Model, ctxWindow int) segment
func segTokens(m *Model) segment
func segKeybinds(m *Model) segment
```

Each returns `visible=false` when its data is empty/disabled (agent idle, no git, etc.).

**Implementation highlights:**

- `segState`: maps `m.status.state` to colored glyph + label using `m.theme.State*` palette. Spinner glyph prepended when `m.spinner.on`.
- `segElapsed`: `time.Since(m.turnStart).Round(100ms)` formatted; hides when `m.pending == nil`.
- `segCwd`: home-collapse with `~/`; if longer than 30 chars, truncate middle with `…`.
- `segGit`: `" " + branch` with `"●"` suffix when dirty; hidden when `branch == ""`.
- `segCtx`: uses `lastIterIn / contextWindow`. Shows `ctx —` when `lastIterIn == 0`.
- `segTokens`: `fmt.Sprintf("%s↓ %s↑", humanK(turnIn), humanK(turnOut))`.
- `segKeybinds`: static string.

**Step 2: Add `humanK` helper**

```go
func humanK(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
```

**Step 3: Write `segments_test.go`**

Exhaustively test each segment for every relevant `Model` state (idle/thinking/responding/tool/error; pending or not; git present or not; empty tokens; wide vs narrow).

**Step 4: Commit**

```
feat(tui): segment catalogue with pure per-segment renderers
```

---

## Task 8: Statusbar rewrite + layout move below input

**Files:**
- Rewrite: `internal/tui/statusbar.go`
- Modify: `internal/tui/update.go` (View layout)

**Step 1: Rewrite `statusbar.go`**

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar renders either one or two styled rows according to Settings.
func (m *Model) StatusBar() []string {
	if !m.settings.Statusbar.Enabled {
		return nil
	}
	ctxMax := m.configContextWindow()
	segs := []segment{
		segState(m),
		segElapsed(m),
		segIter(m),
	}
	rowBottom := []segment{
		segProvider(m),
		segModel(m),
		segCwd(m),
		segGit(m),
		segCtx(m, ctxMax),
		segTokens(m),
		segKeybinds(m),
	}

	layout := m.settings.Statusbar.Layout
	segs = filterEnabled(segs, m.settings.Statusbar.Segments)
	rowBottom = filterEnabled(rowBottom, m.settings.Statusbar.Segments)

	rows := []string{}
	if layout == "one-line" {
		all := append(segs, rowBottom...)
		rows = append(rows, composeRow(all, m.width))
	} else {
		if len(segs) > 0 {
			rows = append(rows, composeRow(segs, m.width))
		}
		if len(rowBottom) > 0 {
			rows = append(rows, composeRow(rowBottom, m.width))
		}
	}
	return rows
}

// composeRow joins visible segments with " · " separator and drops lowest-priority
// segments until total width fits maxW.
func composeRow(segs []segment, maxW int) string {
	active := []segment{}
	for _, s := range segs {
		if s.visible {
			active = append(active, s)
		}
	}
	for {
		joined := joinSegs(active)
		if lipgloss.Width(joined) <= maxW || len(active) == 1 {
			return joined
		}
		active = dropLowestPriority(active)
	}
}

func joinSegs(segs []segment) string {
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		parts = append(parts, s.text)
	}
	return strings.Join(parts, " · ")
}

func dropLowestPriority(segs []segment) []segment {
	idx := 0
	for i, s := range segs {
		if s.priority > segs[idx].priority {
			idx = i
		}
	}
	return append(segs[:idx], segs[idx+1:]...)
}

func filterEnabled(segs []segment, on StatusbarSegments) []segment {
	// (match segment name -> enabled flag via a small local map keyed by a tag
	// stored on segment; for simplicity segments already honor enabled flag
	// internally and set visible=false when disabled)
	out := segs[:0]
	for _, s := range segs {
		if s.visible {
			out = append(out, s)
		}
	}
	return out
}
```

**Step 2: Move status below input in `View()`**

In `update.go View()`, replace current assembly with:

```go
var parts []string
// live tail
if m.pending != nil && m.pending.committed < len(m.pending.raw) {
    parts = append(parts, string(m.pending.raw[m.pending.committed:]))
}
// thinking card
if m.pending != nil && len(m.pending.thinkRaw) > 0 {
    parts = append(parts, renderThinkingCard(m.theme, string(m.pending.thinkRaw), true))
}

// bottom region (modal / approval / suggestions / input)
bottom := m.renderInputBox()
if m.modal != nil {
    bottom = m.modal.View()
} else if m.approval != nil {
    bottom = m.approval.View()
} else if m.suggest.active {
    bottom = m.renderSuggestions() + "\n" + m.renderInputBox()
}
parts = append(parts, bottom)

// status BELOW input
for _, row := range m.StatusBar() {
    parts = append(parts, row)
}
return lipgloss.JoinVertical(lipgloss.Left, parts...)
```

Drop the legacy `m.status.View()` call and the blank-line separator above input.

**Step 3: Delete old statusbarModel.View()**

`statusbarModel.View()` is no longer the renderer. Keep `statusbarModel` as pure data (provider/model/state/tokens/etc), but remove its `View()` method.

**Step 4: Verify**

```bash
go build ./... && ./sam   # manual smoke: visually confirm status renders below input
```

**Step 5: Commit**

```
refactor(tui): move status bar below input and render via segments
```

---

## Task 9: Settings modal scaffolding

**Files:**
- Create: `internal/tui/settings_modal.go`

**Step 1: Composite tab modal**

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type settingsTab int

const (
	tabStatusline settingsTab = iota
	tabProviders
	tabTheme
	numTabs
)

type settingsModal struct {
	forms       [numTabs]*huh.Form
	active      settingsTab
	done        bool
	cancelled   bool

	pending      Settings   // buffered edits
	pendingTheme *Theme     // live-preview theme

	// provider tab state
	provider, minimaxKey, anthropicKey string

	factory ProviderFactory
}

func newSettingsModal(current Settings, curProvider string, factory ProviderFactory) *settingsModal {
	m := &settingsModal{
		pending:      current,
		pendingTheme: NewTheme(current.Theme),
		provider:     curProvider,
		factory:      factory,
	}
	m.forms[tabStatusline] = m.buildStatuslineForm()
	m.forms[tabProviders]  = m.buildProvidersForm()
	m.forms[tabTheme]      = m.buildThemeForm()
	return m
}

func (m *settingsModal) Init() tea.Cmd { return m.forms[m.active].Init() }

func (m *settingsModal) Update(msg tea.Msg) tea.Cmd {
	// Tab-switch interception
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.Type {
		case tea.KeyCtrlRight, tea.KeyTab:
			if km.Type == tea.KeyCtrlRight || (km.Type == tea.KeyTab && km.Alt) {
				m.active = (m.active + 1) % numTabs
				return m.forms[m.active].Init()
			}
		case tea.KeyCtrlLeft:
			m.active = (m.active - 1 + numTabs) % numTabs
			return m.forms[m.active].Init()
		case tea.KeyEsc:
			m.cancelled = true
			m.done = true
			return nil
		case tea.KeyCtrlS:
			m.done = true
			return nil
		}
	}

	form, cmd := m.forms[m.active].Update(msg)
	if ff, ok := form.(*huh.Form); ok {
		m.forms[m.active] = ff
	}
	// rebuild pending theme on every keystroke for live preview
	m.pendingTheme = NewTheme(m.pending.Theme)
	return cmd
}

func (m *settingsModal) View() string {
	tabs := m.renderTabs()
	body := m.forms[m.active].View()
	var preview string
	switch m.active {
	case tabStatusline:
		preview = m.renderStatuslinePreview()
	case tabTheme:
		preview = m.renderThemePreview()
	}
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).
		Render("↹ switch tab  Ctrl+S save  Esc cancel")
	return lipgloss.JoinVertical(lipgloss.Left, tabs, body, preview, footer)
}

func (m *settingsModal) Done() bool { return m.done }

func (m *settingsModal) Apply(root *Model) tea.Cmd {
	if m.cancelled {
		return root.addInfo("settings cancelled")
	}
	if err := SaveSettings(m.pending); err != nil {
		return root.addInfo("save settings failed: " + err.Error())
	}
	// providers save
	if cmd := m.applyProviders(root); cmd != nil {
		// cmd may be an error info — still apply settings + theme
	}
	root.settings = m.pending
	root.theme = NewTheme(m.pending.Theme)
	root.theme.Apply(root.width)
	return root.addInfo("settings saved")
}

func (m *settingsModal) renderTabs() string {
	labels := []string{"Statusline", "Providers", "Theme"}
	var sb strings.Builder
	for i, l := range labels {
		style := lipgloss.NewStyle().Padding(0, 1)
		if settingsTab(i) == m.active {
			style = style.Background(lipgloss.Color("63")).Foreground(lipgloss.Color("230")).Bold(true)
		} else {
			style = style.Foreground(lipgloss.Color("244"))
		}
		sb.WriteString(style.Render(l))
	}
	return sb.String()
}
```

Leave `buildStatuslineForm`, `buildProvidersForm`, `buildThemeForm`, `renderStatuslinePreview`, `renderThemePreview`, `applyProviders` as stubs returning empty/noop for now — Tasks 10/11/12 fill them.

**Step 2: Verify scaffolding compiles**

```bash
go build ./internal/tui
```

**Step 3: Commit**

```
feat(tui): settings modal scaffolding with tab-switch interception
```

---

## Task 10: Statusline tab form + live preview

**Files:**
- Modify: `internal/tui/settings_modal.go`

**Step 1: Build huh form**

```go
func (m *settingsModal) buildStatuslineForm() *huh.Form {
	p := &m.pending.Statusbar
	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Status bar"),
			huh.NewConfirm().Title("Enabled").Value(&p.Enabled),
			huh.NewSelect[string]().
				Title("Layout").
				Options(
					huh.NewOption("two-line", "two-line"),
					huh.NewOption("one-line", "one-line"),
				).Value(&p.Layout),
			huh.NewConfirm().Title("Spinner animation").Value(&p.Spinner),
			huh.NewConfirm().Title("Colored state").Value(&p.Colors),
			huh.NewConfirm().Title("Show elapsed time").Value(&p.Elapsed),
		),
		huh.NewGroup(
			huh.NewNote().Title("Segments"),
			huh.NewMultiSelect[string]().
				Title("Visible segments").
				Options(
					huh.NewOption("state", "state"),
					huh.NewOption("model", "model"),
					huh.NewOption("provider", "provider"),
					huh.NewOption("cwd", "cwd"),
					huh.NewOption("git", "git"),
					huh.NewOption("iterations", "iterations"),
					huh.NewOption("context", "context"),
					huh.NewOption("tokens", "tokens"),
					huh.NewOption("keybinds", "keybinds"),
				).Value(m.segmentsSelectedPtr()),
		),
	).WithShowHelp(true).WithShowErrors(false)
}

// segmentsSelectedPtr returns a pointer-backed []string bound to pending.Statusbar.Segments.
// huh requires a slice; we translate both directions via a shim field in settingsModal.
```

**Step 2: Segment shim**

Since huh multi-select works on `[]string`, add `selectedSegments []string` on `settingsModal`. Seed from `pending.Statusbar.Segments` at construct time; convert back to struct before save.

**Step 3: Live preview**

```go
func (m *settingsModal) renderStatuslinePreview() string {
	// Build a fake Model snapshot for preview
	preview := &Model{
		settings: m.pending,
		theme:    m.pendingTheme,
		width:    80,
		status: statusbarModel{
			provider: "anthropic",
			model:    "sonnet-4-6",
			state:    "responding",
			iter:     3, maxIter: 25,
		},
		// ...mock enough state to render a rich preview
	}
	rows := preview.StatusBar()
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1).
		Render(strings.Join(rows, "\n"))
}
```

**Step 4: Commit**

```
feat(tui): statusline settings tab with live preview
```

---

## Task 11: Providers tab form

**Files:**
- Modify: `internal/tui/settings_modal.go`
- Modify: `internal/config/secrets.go` (if masking needed)
- Modify: `internal/tui/forms.go` (drop old auth/provider forms)

**Step 1: Build form**

```go
func (m *settingsModal) buildProvidersForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Active provider"),
			huh.NewSelect[string]().
				Title("Provider").
				Options(
					huh.NewOption("Minimax", "minimax"),
					huh.NewOption("Anthropic", "anthropic"),
				).Value(&m.provider),
		),
		huh.NewGroup(
			huh.NewNote().Title("API keys (leave blank to keep existing)"),
			huh.NewInput().Title("Minimax API key").
				EchoMode(huh.EchoModePassword).Value(&m.minimaxKey),
			huh.NewInput().Title("Anthropic API key").
				EchoMode(huh.EchoModePassword).Value(&m.anthropicKey),
		),
	).WithShowHelp(true)
}
```

**Step 2: Apply logic**

```go
func (m *settingsModal) applyProviders(root *Model) tea.Cmd {
	s := config.LoadSecrets()
	changed := false
	if m.minimaxKey != "" {
		s.MinimaxAPIKey = m.minimaxKey; changed = true
	}
	if m.anthropicKey != "" {
		s.AnthropicAPIKey = m.anthropicKey; changed = true
	}
	if changed {
		if err := config.SaveSecrets(s); err != nil {
			return root.addInfo("save secrets failed: " + err.Error())
		}
		s.ApplyEnv()
	}
	if m.factory != nil && m.provider != root.status.provider {
		p, err := m.factory(m.provider, root.status.model)
		if err != nil {
			return root.addInfo("provider build failed: " + err.Error())
		}
		root.agent.SetProvider(p)
		root.status.provider = m.provider
	}
	return nil
}
```

**Step 3: Delete `authForm` + `providerForm` from `forms.go`**

They're now inside the modal. Also remove `newAuthForm`, `newProviderForm`.

**Step 4: Verify**

```bash
go build ./... && go vet ./...
```

**Step 5: Commit**

```
feat(tui): providers settings tab replaces /auth and /provider modals
```

---

## Task 12: Theme tab form + live preview

**Files:**
- Modify: `internal/tui/settings_modal.go`

**Step 1: Build form**

```go
func (m *settingsModal) buildThemeForm() *huh.Form {
	t := &m.pending.Theme
	validateHex := func(s string) error {
		if s == "" { return nil }
		if !strings.HasPrefix(s, "#") || len(s) != 7 {
			return fmt.Errorf("expected #rrggbb")
		}
		return nil
	}
	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Markdown renderer"),
			huh.NewSelect[string]().
				Title("Glamour style").
				Options(
					huh.NewOption("dark", "dark"),
					huh.NewOption("light", "light"),
					huh.NewOption("dracula", "dracula"),
					huh.NewOption("notty", "notty"),
				).Value(&t.GlamourStyle),
		),
		huh.NewGroup(
			huh.NewNote().Title("Palette"),
			huh.NewInput().Title("Accent (#rrggbb)").Value(&t.Accent).Validate(validateHex),
			huh.NewInput().Title("Muted").Value(&t.Muted).Validate(validateHex),
			huh.NewInput().Title("User border").Value(&t.UserBorder).Validate(validateHex),
			huh.NewInput().Title("Error fg").Value(&t.ErrorFg).Validate(validateHex),
		),
		huh.NewGroup(
			huh.NewNote().Title("State colors"),
			huh.NewInput().Title("Thinking").Value(&t.StateThinking).Validate(validateHex),
			huh.NewInput().Title("Responding").Value(&t.StateResponding).Validate(validateHex),
			huh.NewInput().Title("Tool").Value(&t.StateTool).Validate(validateHex),
			huh.NewInput().Title("Error").Value(&t.StateError).Validate(validateHex),
			huh.NewInput().Title("Approval").Value(&t.StateApproval).Validate(validateHex),
		),
	).WithShowHelp(true).WithShowErrors(true)
}
```

**Step 2: Live preview**

```go
func (m *settingsModal) renderThemePreview() string {
	sample := "# Heading\n\nSome **bold** text and `code`.\n\n- bullet one\n- bullet two"
	rendered, _ := m.pendingTheme.Glamour().Render(sample)

	status := "⟳ responding · 1.2s · ↻ 3/25 · ctx 42% · anthropic/sonnet-4-6"
	styled := m.pendingTheme.StatusBar.Render(status)

	border := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1)
	return border.Render(rendered + "\n\n" + styled)
}
```

**Step 3: Commit**

```
feat(tui): theme settings tab with live color + glamour preview
```

---

## Task 13: Slash command wiring

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/update.go` (`dispatchCommand`)

**Step 1: Commands**

- Add `CmdSettings`
- Remove `CmdAuth`, `CmdProvider`
- Update `commandSuggestions` list (drop `/auth`, `/provider`; add `/settings`)
- Update `helpText` to reflect new commands

**Step 2: Update `dispatchCommand`**

```go
case CmdSettings:
    m.modal = newSettingsModal(m.settings, m.status.provider, m.factory)
    m.input.Blur()
    return m, m.modal.Init()
```

Remove the `CmdAuth` and `CmdProvider` cases entirely.

**Step 3: Verify**

- Typing `/sett` suggests `/settings`
- `/settings` opens modal on Statusline tab
- `/auth` and `/provider` no longer parsed (fall into `CmdUnknown`)

**Step 4: Commit**

```
feat(tui): wire /settings slash command; retire /auth and /provider
```

---

## Task 14: Atomic save + integration test

**Files:**
- Modify: `internal/tui/settings_modal.go` (Apply atomicity)
- Create: `internal/tui/settings_modal_test.go`

**Step 1: Wrap Apply in a try-then-commit pattern**

```go
func (m *settingsModal) Apply(root *Model) tea.Cmd {
	if m.cancelled {
		return root.addInfo("settings cancelled")
	}
	// dry-run: render the theme to catch malformed colors
	probeTheme := NewTheme(m.pending.Theme)
	if probeTheme.Glamour() == nil {
		return root.addInfo("save aborted: glamour style invalid")
	}
	// persist settings first (smallest blast radius)
	if err := SaveSettings(m.pending); err != nil {
		return root.addInfo("save settings failed: " + err.Error())
	}
	if cmd := m.applyProviders(root); cmd != nil {
		// Failure in provider save is surfaced via addInfo but settings stay.
		return cmd
	}
	root.settings = m.pending
	root.theme = probeTheme
	root.theme.Apply(root.width)
	return root.addInfo("settings saved")
}
```

**Step 2: Integration test**

Spin up a `settingsModal`, inject pending state with a broken hex color, call `Apply` on a mock `Model`, assert:
- Settings file not written when glamour probe fails
- `root.settings` / `root.theme` unchanged
- Error message surfaced via a `tea.Cmd` that resolves to an info-style `tea.Printf`

**Step 3: Commit**

```
test(tui): settings modal apply-atomicity integration test
```

---

## Task 15: Smoke test, vet, fmt, polish

**Files:**
- Review: all modified files

**Step 1: Run full verification**

```bash
go vet ./...
gofmt -w internal/
go test ./...
go build -o sam ./cmd/sam
```

**Step 2: Manual smoke checklist**

- `./sam` launches, default settings render two-line status bar below input
- `/settings` opens modal on Statusline tab
- Ctrl+Tab cycles to Providers, then Theme, then back
- Statusline tab: toggling a segment updates live preview immediately
- Providers tab: changing provider and saving rebuilds agent
- Theme tab: editing a hex color updates preview markdown + status immediately
- Ctrl+S saves all tabs, settings.toml appears on disk
- Esc cancels, no files modified
- `/auth` and `/provider` now show "unknown command"
- `/help` shows updated command list
- Spinner animates during streaming, stops at idle
- Status bar segments drop gracefully on 60-column terminal

**Step 3: README / help updates**

Update `helpText` in `commands.go` and any README sections referencing old commands.

**Step 4: Commit polish**

```
polish(tui): smoke fixes, vet clean, help text refresh
```

---

## File changes summary

| File | Change |
|---|---|
| `internal/tui/settings.go` | Create |
| `internal/tui/settings_test.go` | Create |
| `internal/tui/theme.go` | Create |
| `internal/tui/gitinfo.go` | Create |
| `internal/tui/gitinfo_test.go` | Create |
| `internal/tui/spinner.go` | Create |
| `internal/tui/spinner_test.go` | Create |
| `internal/tui/segments.go` | Create |
| `internal/tui/segments_test.go` | Create |
| `internal/tui/settings_modal.go` | Create |
| `internal/tui/settings_modal_test.go` | Create |
| `internal/tui/statusbar.go` | Rewrite |
| `internal/tui/styles.go` | Shrink |
| `internal/tui/update.go` | View layout + UsageEvent + spinner tick + /settings |
| `internal/tui/app.go` | New fields: settings, theme, git, turnStart, spinner, token counters |
| `internal/tui/forms.go` | Remove authForm, providerForm |
| `internal/tui/commands.go` | Add CmdSettings, drop CmdAuth/CmdProvider |
| `internal/tui/render.go` | Theme-parameterized rendering |
| `internal/tui/messages.go` | Add spinnerTickMsg |
| `internal/config/config.go` | Add `[models]` + `ModelContextWindow` |
| `internal/config/context_test.go` | Create |
| `internal/agent/agent.go` | Add `UsageEvent` |
| `internal/agent/loop.go` | Emit usage |
| `internal/llm/provider.go` | Usage type on completion response |
| `internal/llm/anthropiccompat/client.go` | Parse usage |
| `internal/llm/minimax*/client.go` | Parse usage |

---

## Expected outcomes

- Status bar renders below the input in either one- or two-line layout, with spinner, state colors, and keybind hints
- `/settings` opens a tabbed modal covering Statusline, Providers, Theme — settings persist across restarts
- Theme changes apply live with hex-color pickers
- Token counts and context % display in real time during turns
- Legacy `/auth` and `/provider` commands retired; users directed to `/settings`

## Risks & mitigations (recap)

| Risk | Mitigation |
|---|---|
| huh tab-switch key collision | Only Ctrl+Tab/Ctrl+Left/Ctrl+Right, no numeric jumps |
| Theme refactor blast radius | Ship Task 2 independently before modal work depends on it |
| Provider usage parse shape mismatch | Per-provider parsers; skip emission on parse failure |
| Spinner re-renders interacting with tea.Printf | Ticks only live-frame, never Printf; gated to `pending != nil` |
| Malformed hex colors in saved TOML | `validate()` on load + probe theme on save; fall back to defaults, log warning |
