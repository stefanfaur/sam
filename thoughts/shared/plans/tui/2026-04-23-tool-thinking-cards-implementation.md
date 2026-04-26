# Tool + Thinking Cards Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use `caveroach:executing-plans` to implement this plan task-by-task.

**Goal:** Implement the TUI design spec at `thoughts/shared/plans/tui/2026-04-23-tool-thinking-cards-design.md` — merged per-tool cards (running/success/error/cancelled), thinking cards with full/header streaming modes + settled `💭` marker, `/show-tool N` and `/show-thinking N` expansion backed by monotonic-index ring buffers, subtle bg tint, narrow-terminal hint wrap, render-time truncation.

**Architecture:** Extend `pendingTurn` with `*thinkingCardState` + `[]*toolCardState`. Event handlers mutate these in-place; `View()` live-renders on 80ms spinner ticks; `TurnDone`/`ErrorEvent` flush settled cards to scrollback via `tea.Printf` and record to bounded ring buffers. Model holds monotonic `nextToolIndex`/`nextThinkingIndex` counters so expansion indices never collide after wraparound. No new agent event types; no rendered-string cache in v1.

**Tech Stack:** Go 1.26.2 · Bubbletea 1.3.10 · Lipgloss 1.1.1 · BurntSushi/toml · existing `skillCardState` pattern (`internal/tui/app.go:27-34`, `internal/tui/render.go:71-107`).

**Spec is authoritative.** Where this plan says "per spec §X", the spec prose is the contract; the plan only gives file paths, test structure, and verification steps.

---

## Conventions

- **Strict TDD.** Each task writes the failing test first, confirms the failure, implements, confirms pass, commits.
- **Verification command.** Every task ends with `go test ./internal/tui/... -count=1 -race` passing; Task 10 also runs `go test ./... -count=1 -race` and `go build ./...`.
- **Commits.** Conventional style: `feat(tui): …` / `test(tui): …`. One commit per task unless noted.
- **Time source.** Tests use `time.Now()`-free injection via explicit `now time.Time` parameters on render functions (already matches skill-card signature style). No global clock.
- **Lipgloss width.** Inner width = `m.width - 4` (border + padding). Plan assumes callers pass this; defaults to 80 if zero.

---

### Task 1: Extend `pendingTurn` + add card state types + ring buffers

**Files:**
- Modify: `internal/tui/app.go:16-23` (extend `pendingTurn`)
- Modify: `internal/tui/app.go:36-59` (add ring buffer fields + counters to `Model`)
- Modify: `internal/tui/app.go:61-77` (add new types + helpers near existing `skillInvocation`)
- Create: `internal/tui/cards_test.go`

**Step 1: Write failing tests** in `internal/tui/cards_test.go`:

```go
package tui

import (
	"testing"
)

func TestModel_RecordToolBoundedRing(t *testing.T) {
	m := &Model{}
	for i := 0; i < maxRecentTools+5; i++ {
		m.recordTool(toolInvocation{Index: i + 1, Header: "Bash · done", IsError: false})
	}
	if len(m.recentTools) != maxRecentTools {
		t.Errorf("len = %d, want %d", len(m.recentTools), maxRecentTools)
	}
	// Oldest indices evicted; newest retained.
	if m.recentTools[0].Index != 6 {
		t.Errorf("front Index = %d, want 6", m.recentTools[0].Index)
	}
}

func TestModel_RecordThinkingBoundedRing(t *testing.T) {
	m := &Model{}
	for i := 0; i < maxRecentThinking+3; i++ {
		m.recordThinking(thinkingInvocation{Index: i + 1, Header: "thought for 1s"})
	}
	if len(m.recentThinking) != maxRecentThinking {
		t.Errorf("len = %d, want %d", len(m.recentThinking), maxRecentThinking)
	}
}

func TestModel_MonotonicToolIndex(t *testing.T) {
	m := &Model{}
	a := m.nextToolIdx()
	b := m.nextToolIdx()
	c := m.nextToolIdx()
	if a != 1 || b != 2 || c != 3 {
		t.Errorf("indices = %d,%d,%d; want 1,2,3", a, b, c)
	}
}

func TestModel_MonotonicThinkingIndex(t *testing.T) {
	m := &Model{}
	a := m.nextThinkingIdx()
	b := m.nextThinkingIdx()
	if a != 1 || b != 2 {
		t.Errorf("indices = %d,%d; want 1,2", a, b)
	}
}
```

**Step 2: Confirm failure.**
```bash
go test ./internal/tui/... -run 'TestModel_(RecordTool|RecordThinking|Monotonic)' -count=1
```
Expected: compile errors — `toolInvocation`, `thinkingInvocation`, `maxRecentTools`, `maxRecentThinking`, `recordTool`, `recordThinking`, `nextToolIdx`, `nextThinkingIdx` undefined.

**Step 3: Implement in `internal/tui/app.go`:**

Extend `pendingTurn`:
```go
type pendingTurn struct {
	events    <-chan Event
	raw       []rune
	committed int
	thinkRaw  []rune
	done      bool
	skill     *skillCardState
	thinking  *thinkingCardState
	tools     []*toolCardState
}
```

Add new types near `skillCardState`:
```go
type toolCardState struct {
	ID        string
	Name      string
	Input     json.RawMessage
	Output    string
	Lines     int
	EditLines int
	Bytes     int
	IsError   bool
	Cancelled bool
	StartedAt time.Time
	EndedAt   time.Time
	Index     int
}

type thinkingCardState struct {
	Text      string
	StartedAt time.Time
	EndedAt   time.Time
	Index     int
}

type toolInvocation struct {
	Index   int
	Header  string
	Input   string
	Output  string
	IsError bool
}

type thinkingInvocation struct {
	Index  int
	Header string
	Body   string
}
```

Add `encoding/json` to the imports.

Add ring + counters to `Model`:
```go
recentTools       []toolInvocation
recentThinking    []thinkingInvocation
nextToolIndex     int
nextThinkingIndex int
```

Add constants + helpers:
```go
const (
	maxRecentTools    = 32
	maxRecentThinking = 32
)

func (m *Model) recordTool(inv toolInvocation) {
	m.recentTools = append(m.recentTools, inv)
	if len(m.recentTools) > maxRecentTools {
		m.recentTools = m.recentTools[len(m.recentTools)-maxRecentTools:]
	}
}

func (m *Model) recordThinking(inv thinkingInvocation) {
	m.recentThinking = append(m.recentThinking, inv)
	if len(m.recentThinking) > maxRecentThinking {
		m.recentThinking = m.recentThinking[len(m.recentThinking)-maxRecentThinking:]
	}
}

func (m *Model) nextToolIdx() int {
	m.nextToolIndex++
	return m.nextToolIndex
}

func (m *Model) nextThinkingIdx() int {
	m.nextThinkingIndex++
	return m.nextThinkingIndex
}
```

**Step 4: Confirm pass.**
```bash
go test ./internal/tui/... -run 'TestModel_(RecordTool|RecordThinking|Monotonic)' -count=1
```
Expected: PASS (4 tests).

**Step 5: Commit.**
```bash
git add internal/tui/app.go internal/tui/cards_test.go
git commit -m "feat(tui): add tool/thinking card state types and ring buffers"
```

---

### Task 2: Add theme styles + cached dark-bg detection

**Files:**
- Modify: `internal/tui/theme.go:8-42` (add fields to `Theme`)
- Modify: `internal/tui/theme.go:44-60` (cache `dark` at construction)
- Modify: `internal/tui/theme.go:62-108` (extend `Apply` with new styles)
- Create: `internal/tui/theme_cards_test.go`

**Step 1: Write failing tests** in `internal/tui/theme_cards_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestTheme_ToolCardStylesPresent(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	s := th.ToolCard.Render("x")
	if !strings.Contains(s, "x") {
		t.Errorf("ToolCard didn't render body: %q", s)
	}
	if th.ToolCardHeader.GetForeground() == nil {
		t.Error("ToolCardHeader foreground unset")
	}
}

func TestTheme_ThinkingCardBodyHasBackground(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	bg := th.ThinkingCardBody.GetBackground()
	if bg == nil {
		t.Fatal("ThinkingCardBody background unset")
	}
}

func TestTheme_SubtleBgDependsOnDark(t *testing.T) {
	th := &Theme{dark: true}
	if got := th.subtleBg(); string(got) != "235" {
		t.Errorf("dark bg = %q, want 235", got)
	}
	th.dark = false
	if got := th.subtleBg(); string(got) != "254" {
		t.Errorf("light bg = %q, want 254", got)
	}
}
```

**Step 2: Confirm failure.**
```bash
go test ./internal/tui/... -run TestTheme_ -count=1
```
Expected: compile errors — `ToolCard`, `ToolCardHeader`, `ThinkingCardBody`, `subtleBg`, `dark` undefined on `Theme`.

**Step 3: Implement in `internal/tui/theme.go`.**

Add to `Theme` struct (keep existing fields, append):
```go
ToolCard        lipgloss.Style
ToolCardError   lipgloss.Style
ToolCardHeader  lipgloss.Style
ToolCardMeta    lipgloss.Style
ToolCardPeek    lipgloss.Style
ToolCardPeekErr lipgloss.Style

ThinkingCard     lipgloss.Style
ThinkingCardBody lipgloss.Style
ThinkingCardMeta lipgloss.Style

dark bool
```

In `NewTheme`, cache once:
```go
th.dark = lipgloss.HasDarkBackground()
```
(place right before `th.Apply(80)`)

Add `subtleBg()` method:
```go
func (t *Theme) subtleBg() lipgloss.Color {
	if t.dark {
		return lipgloss.Color("235")
	}
	return lipgloss.Color("254")
}
```

In `Apply(width)`, add (after existing style assignments):
```go
t.ToolCard = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(t.Accent).
	Padding(0, 1).MarginTop(1)
t.ToolCardError = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(t.ErrorFg).
	Padding(0, 1).MarginTop(1)
t.ToolCardHeader = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
t.ToolCardMeta = lipgloss.NewStyle().Foreground(t.Muted)
t.ToolCardPeek = lipgloss.NewStyle().Foreground(t.Muted).Italic(true)
t.ToolCardPeekErr = lipgloss.NewStyle().Foreground(t.ErrorFg).Italic(true)

t.ThinkingCard = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(t.Accent).
	Padding(0, 1).MarginTop(1)
inner := width - 4
if inner < 1 {
	inner = 1
}
t.ThinkingCardBody = lipgloss.NewStyle().
	Foreground(t.Muted).Italic(true).
	Background(t.subtleBg()).
	Width(inner)
t.ThinkingCardMeta = lipgloss.NewStyle().Foreground(t.Muted)
```

**Step 4: Confirm pass.**
```bash
go test ./internal/tui/... -run TestTheme_ -count=1
```
Expected: PASS (3 tests).

**Step 5: Commit.**
```bash
git add internal/tui/theme.go internal/tui/theme_cards_test.go
git commit -m "feat(tui): add tool/thinking card theme styles + cached dark-bg detection"
```

---

### Task 3: Add `ThinkingSettings.StreamMode` + settings-modal control

**Files:**
- Modify: `internal/tui/settings.go:11-50` (add `ThinkingSettings`, register on `Settings`)
- Modify: `internal/tui/settings.go:52-80` (extend `DefaultSettings`)
- Modify: `internal/tui/settings.go` (load-time normalize)
- Modify: `internal/tui/settings_modal.go` — add select row under the existing statusline/theme tab (same tab; do not add a new tab)
- Create: `internal/tui/settings_thinking_test.go`

**Step 1: Write failing tests** in `internal/tui/settings_thinking_test.go`:

```go
package tui

import "testing"

func TestDefaultSettings_ThinkingMode(t *testing.T) {
	s := DefaultSettings()
	if s.Thinking.StreamMode != "full" {
		t.Errorf("default stream mode = %q, want full", s.Thinking.StreamMode)
	}
}

func TestNormalizeThinkingMode(t *testing.T) {
	cases := map[string]string{
		"":           "full",
		"full":       "full",
		"header":     "header",
		"nonsense":   "full",
		"HEADER":     "full", // strict match only
	}
	for in, want := range cases {
		s := Settings{Thinking: ThinkingSettings{StreamMode: in}}
		s = normalizeSettings(s)
		if s.Thinking.StreamMode != want {
			t.Errorf("normalize(%q) = %q, want %q", in, s.Thinking.StreamMode, want)
		}
	}
}
```

**Step 2: Confirm failure.**
```bash
go test ./internal/tui/... -run 'TestDefault|TestNormalizeThinking' -count=1
```
Expected: compile errors — `ThinkingSettings`, `normalizeSettings`, `Settings.Thinking` undefined.

**Step 3: Implement in `internal/tui/settings.go`.**

Add type + field:
```go
type ThinkingSettings struct {
	StreamMode string `toml:"stream_mode"` // "full" | "header"
}
```
On `Settings`:
```go
Thinking ThinkingSettings `toml:"thinking"`
```

Extend `DefaultSettings()` returning `Thinking: ThinkingSettings{StreamMode: "full"}`.

Add:
```go
func normalizeSettings(s Settings) Settings {
	if s.Thinking.StreamMode != "full" && s.Thinking.StreamMode != "header" {
		s.Thinking.StreamMode = "full"
	}
	return s
}
```

Call `normalizeSettings` at the tail of `LoadSettings` before returning.

**Settings modal UI.** Add a select row to the existing primary tab (statusline) in `internal/tui/settings_modal.go`. Do *not* add a new tab. Use the same `huh` field pattern already present for other selects. Label: `"Thinking stream"`, options `"full" / "header"`, bound to `s.Thinking.StreamMode`. If you cannot find a clear injection point in under 30 seconds of reading, escalate.

**Step 4: Confirm pass.**
```bash
go test ./internal/tui/... -run 'TestDefault|TestNormalizeThinking' -count=1
```
Expected: PASS (2 tests).

**Step 5: Commit.**
```bash
git add internal/tui/settings.go internal/tui/settings_modal.go internal/tui/settings_thinking_test.go
git commit -m "feat(tui): add thinking.stream_mode setting with full/header modes"
```

---

### Task 4: Implement `renderToolCard` + `toolSummary`

**Files:**
- Modify: `internal/tui/render.go` (add `renderToolCard`, `toolSummary`, helper `toolInputLine`)
- Create: `internal/tui/render_tool_test.go`

**Step 1: Write failing tests.** Spec §"Tool cards" + §"Rendering API" are authoritative for expected output; tests assert only the salient substrings to stay robust to exact styling:

```go
package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRenderToolCard_Running(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &toolCardState{
		ID:        "1",
		Name:      "Bash",
		Input:     json.RawMessage(`{"command":"go test ./..."}`),
		StartedAt: time.Now(),
		Index:     3,
	}
	out := renderToolCard(th, tc, time.Now(), 0, 80)
	if !strings.Contains(out, "Bash") {
		t.Errorf("missing name: %q", out)
	}
	if !strings.Contains(out, "running…") {
		t.Errorf("missing running slug: %q", out)
	}
	if strings.Contains(out, "/show-tool") {
		t.Errorf("hint should NOT appear while running: %q", out)
	}
	if !strings.ContainsAny(out, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Errorf("missing spinner glyph: %q", out)
	}
}

func TestRenderToolCard_DoneSuccess(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	start := time.Now().Add(-1200 * time.Millisecond)
	tc := &toolCardState{
		Name:      "Bash",
		Input:     json.RawMessage(`{"command":"go test"}`),
		Output:    "PASS\nok  pkg/x  0.1s\n",
		Lines:     2,
		StartedAt: start,
		EndedAt:   time.Now(),
		Index:     5,
	}
	out := renderToolCard(th, tc, time.Now(), 0, 80)
	for _, want := range []string{"●", "Bash", "2 lines", "[/show-tool 5]", "PASS"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestRenderToolCard_DoneError(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &toolCardState{
		Name:      "Bash",
		Input:     json.RawMessage(`{"command":"go test"}`),
		Output:    "FAIL: TestFoo\n--- FAIL",
		Lines:     2,
		IsError:   true,
		StartedAt: time.Now().Add(-400 * time.Millisecond),
		EndedAt:   time.Now(),
		Index:     9,
	}
	out := renderToolCard(th, tc, time.Now(), 0, 80)
	for _, want := range []string{"✗", "Bash", "FAIL", "[/show-tool 9]"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestRenderToolCard_Cancelled(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &toolCardState{
		Name:      "Bash",
		Input:     json.RawMessage(`{"command":"sleep 9"}`),
		Cancelled: true,
		StartedAt: time.Now().Add(-500 * time.Millisecond),
		EndedAt:   time.Now(),
		Index:     11,
	}
	out := renderToolCard(th, tc, time.Now(), 0, 80)
	for _, want := range []string{"◌", "Bash", "cancelled", "[/show-tool 11]"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestToolSummary_PerTool(t *testing.T) {
	cases := []struct {
		tc   *toolCardState
		want string
	}{
		{&toolCardState{Name: "Bash", Lines: 47,
			StartedAt: time.Now().Add(-1200 * time.Millisecond),
			EndedAt:   time.Now()}, "47 lines"},
		{&toolCardState{Name: "Edit", EditLines: 10,
			Input: json.RawMessage(`{"file_path":"foo/bar.go"}`),
			EndedAt: time.Now()}, "10 lines edited"},
		{&toolCardState{Name: "Write", Lines: 42,
			Input: json.RawMessage(`{"file_path":"foo/bar.go"}`),
			EndedAt: time.Now()}, "42 lines written"},
		{&toolCardState{Name: "Task", Bytes: 1024,
			EndedAt: time.Now()}, "~256 tok"},
	}
	for _, c := range cases {
		got := toolSummary(c.tc, time.Now())
		if !strings.Contains(got, c.want) {
			t.Errorf("toolSummary(%s) = %q, want contains %q", c.tc.Name, got, c.want)
		}
	}

	running := &toolCardState{Name: "Bash"}
	if got := toolSummary(running, time.Now()); got != "running…" {
		t.Errorf("running summary = %q, want running…", got)
	}

	cancelled := &toolCardState{Name: "Bash", Cancelled: true,
		StartedAt: time.Now().Add(-300 * time.Millisecond),
		EndedAt:   time.Now()}
	if got := toolSummary(cancelled, time.Now()); !strings.Contains(got, "cancelled") {
		t.Errorf("cancelled summary = %q, want contains cancelled", got)
	}
}

func TestRenderToolCard_NarrowTerminalHintWraps(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &toolCardState{
		Name: "Bash", Lines: 2,
		Input:  json.RawMessage(`{"command":"go test ./internal/tui/..."}`),
		Output: "PASS\n",
		StartedAt: time.Now().Add(-1 * time.Second),
		EndedAt:   time.Now(),
		Index:     99,
	}
	narrow := renderToolCard(th, tc, time.Now(), 0, 30)
	// When narrow, hint must appear but on its own line.
	if !strings.Contains(narrow, "[/show-tool 99]") {
		t.Errorf("hint absent: %q", narrow)
	}
}
```

**Step 2: Confirm failure.** Running tests — `renderToolCard`, `toolSummary` undefined.

**Step 3: Implement in `internal/tui/render.go`.** Add functions per spec §"Rendering API". Key logic:

- `toolSummary(tc, now)`:
  - `tc.EndedAt.IsZero() && !tc.Cancelled` → `"running…"`
  - `tc.Cancelled` → `fmt.Sprintf("cancelled · %s", formatElapsed(tc.EndedAt.Sub(tc.StartedAt)))`
  - Else per-tool table (spec §"Per-tool summary slug").
- `renderToolCard(t, tc, now, frame, innerWidth)`:
  - Pick border style (`t.ToolCard` / `t.ToolCardError`; cancelled uses a muted variant — create on the fly via `t.ToolCard.BorderForeground(t.Muted)` to avoid adding another permanent theme field).
  - Glyph: `spinnerFrames[frame%len]` if running; `●` success; `✗` error; `◌` cancelled.
  - Compose `header := glyph + " " + tc.Name + " · " + summary`.
  - `meta := <input preview line>` using a new helper `toolInputLine(name, input, width-6)` that wraps existing `toolInputPreview` with width-aware truncation (keep `toolInputPreview` as-is; shim trims to `width-6`).
  - Peek: first line of `tc.Output`, truncated to `innerWidth - 2`. Omit for `Edit`, `Write`, running, cancelled. Error peek uses `ToolCardPeekErr`.
  - Hint: `fmt.Sprintf("[/show-tool %d]", tc.Index)` only when `!tc.EndedAt.IsZero() || tc.Cancelled` (any settled state). Append inline to header if `lipgloss.Width(header)+lipgloss.Width(hint)+2 <= innerWidth`; else on a new right-aligned line.
  - Return `border.Render(<composed body>)`.

`estimateTUITokens` already exists and handles the `~N tok` case for Task.

**Step 4: Confirm pass.**
```bash
go test ./internal/tui/... -run 'TestRenderToolCard|TestToolSummary' -count=1
```
Expected: PASS (6 tests).

**Step 5: Commit.**
```bash
git add internal/tui/render.go internal/tui/render_tool_test.go
git commit -m "feat(tui): implement renderToolCard + toolSummary for all four states"
```

---

### Task 5: Refactor `renderThinkingCard` — full/header/settled

**Files:**
- Modify: `internal/tui/update.go:696` (existing `renderThinkingCard(t, text, streaming)` is defined here, not in `render.go` — verify with `grep -n 'func renderThinkingCard' internal/tui/` before editing)
- Move the function to `internal/tui/render.go` as part of this task (co-location with other render helpers) and update the new signature per spec §"Rendering API"
- Delete the old definition from `update.go:696`
- Create: `internal/tui/render_thinking_test.go`
- Modify: `internal/tui/update.go:136-139` caller — wrap Task 5 + Task 6 into one landing so the build never breaks

**Step 1: Write failing tests.**

```go
package tui

import (
	"strings"
	"testing"
	"time"
)

func TestRenderThinkingCard_FullLive(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &thinkingCardState{Text: "Reading the plan", StartedAt: time.Now()}
	out := renderThinkingCard(th, tc, time.Now(), 0, "full", 80)
	if !strings.ContainsAny(out, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Errorf("missing spinner: %q", out)
	}
	if !strings.Contains(out, "Reading the plan") {
		t.Errorf("missing body: %q", out)
	}
}

func TestRenderThinkingCard_HeaderLive(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &thinkingCardState{Text: strings.Repeat("x", 250), StartedAt: time.Now()}
	out := renderThinkingCard(th, tc, time.Now(), 0, "header", 80)
	if !strings.Contains(out, "250 chars") {
		t.Errorf("missing char count: %q", out)
	}
	if strings.Contains(out, "xxxxx") {
		t.Errorf("body should be hidden in header mode: %q", out)
	}
}

func TestRenderThinkingCard_Settled(t *testing.T) {
	th := NewTheme(DefaultSettings().Theme)
	tc := &thinkingCardState{
		Text:      "thought content",
		StartedAt: time.Now().Add(-4200 * time.Millisecond),
		EndedAt:   time.Now(),
		Index:     1,
	}
	// Mode argument is ignored once settled.
	full := renderThinkingCard(th, tc, time.Now(), 0, "full", 80)
	hdr := renderThinkingCard(th, tc, time.Now(), 0, "header", 80)
	for _, out := range []string{full, hdr} {
		if !strings.Contains(out, "💭") {
			t.Errorf("missing settled glyph: %q", out)
		}
		if !strings.Contains(out, "thought for 4.2s") {
			t.Errorf("missing elapsed: %q", out)
		}
		if !strings.Contains(out, "[/show-thinking 1]") {
			t.Errorf("missing hint: %q", out)
		}
	}
}
```

**Step 2: Confirm failure.** Old signature mismatch — compile errors.

**Step 3: Implement.** Delete the existing `renderThinkingCard` definition at `update.go:696`. Add a new one in `internal/tui/render.go` with signature per spec §"Rendering API":

```go
func renderThinkingCard(t *Theme, tc *thinkingCardState, now time.Time,
	spinnerFrame int, mode string, innerWidth int) string
```

Live render branches on `mode`. Settled uses the single `💭 thought for … · N chars [/show-thinking N]` form with the same hint-wrap rule as tool cards.

The old call site at `update.go:136-139` (`renderThinkingCard(m.theme, string(m.pending.thinkRaw), true)`) is rewritten in Task 6. Because the signature changes in this task and the caller adapts in Task 6, the two tasks land together in a single commit. Build will be broken between them — do not commit halfway.

Decision: **Task 5 + Task 6 land as one commit.** Test files are still separate artifacts.

**Step 4: Confirm pass.** (After Task 6.)

**Step 5:** no separate commit — landed with Task 6.

---

### Task 6: Route tool + thinking events into pending state; update `View()`

**Files:**
- Modify: `internal/tui/update.go:118-162` (View composition)
- Modify: `internal/tui/update.go:490-542` (`ThinkingDelta`, `TextDelta`, `ToolCall`, `ToolResult` handlers)
- Create: `internal/tui/update_events_test.go`

**Step 0: Pre-flight event field check.** Before writing any code, confirm the agent event field names:
```bash
grep -n 'type ToolCall\|type ToolResult' internal/agent/events.go
```
Expected: both types have a `ID` field (plan's test code and handlers rely on it). If the field is named something else (e.g. `CallID`), update all test code and handler references accordingly. At spec-write time we verified `ID` is correct — this step is insurance.

**Step 1: Write failing tests.** Tests drive a `Model` through events and assert state mutation (no UI rendering assertions here — those are in Tasks 4/5).

```go
package tui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stefanfaur/sam/internal/agent"
)

func synthPending() *pendingTurn {
	return &pendingTurn{events: make(chan Event)}
}

func TestHandleToolCall_AppendsCardAndSettlesThinking(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	m.pending.thinking = &thinkingCardState{StartedAt: time.Now().Add(-time.Second)}
	ev := agent.ToolCall{ID: "t1", Name: "Bash", Input: json.RawMessage(`{}`)}
	m.applyAgentEvent(ev)
	if len(m.pending.tools) != 1 || m.pending.tools[0].ID != "t1" {
		t.Fatalf("tool not appended: %+v", m.pending.tools)
	}
	if m.pending.thinking.EndedAt.IsZero() {
		t.Error("thinking should settle on ToolCall arrival")
	}
	if m.pending.tools[0].Index != 1 {
		t.Errorf("tool Index = %d, want 1", m.pending.tools[0].Index)
	}
}

func TestHandleToolResult_MatchesByID(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	m.pending.tools = []*toolCardState{
		{ID: "t1", Name: "Bash", StartedAt: time.Now()},
	}
	ev := agent.ToolResult{ID: "t1", Name: "Bash", Output: "line1\nline2\n", IsError: false}
	m.applyAgentEvent(ev)
	if m.pending.tools[0].Output == "" {
		t.Error("output not filled")
	}
	if m.pending.tools[0].Lines != 2 {
		t.Errorf("Lines = %d, want 2", m.pending.tools[0].Lines)
	}
	if m.pending.tools[0].EndedAt.IsZero() {
		t.Error("EndedAt not set")
	}
}

func TestHandleToolResult_UnknownIDDefensive(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	ev := agent.ToolResult{ID: "ghost", Name: "Bash", Output: "x\n"}
	m.applyAgentEvent(ev) // must not panic
	if len(m.pending.tools) != 1 {
		t.Fatalf("defensive card not created: %+v", m.pending.tools)
	}
	if !m.pending.tools[0].EndedAt.After(time.Time{}) {
		t.Error("defensive card should be settled")
	}
}

func TestHandleThinkingDelta_LazyAlloc(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	m.applyAgentEvent(agent.ThinkingDelta{Text: "hello"})
	if m.pending.thinking == nil || m.pending.thinking.Text != "hello" {
		t.Fatalf("thinking not allocated: %+v", m.pending.thinking)
	}
	if m.pending.thinking.Index != 1 {
		t.Errorf("Index = %d, want 1", m.pending.thinking.Index)
	}
}

func TestHandleTextDelta_SettlesThinking(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme), scanner: &blockScanner{}}
	m.pending.thinking = &thinkingCardState{StartedAt: time.Now().Add(-time.Second)}
	m.applyAgentEvent(agent.TextDelta{Text: "ok"})
	if m.pending.thinking.EndedAt.IsZero() {
		t.Error("thinking should settle on first text delta")
	}
}
```

A thin `applyAgentEvent(ev Event)` test hook on `Model` avoids wiring the full Bubbletea `Update` signature. It just dispatches the same switch as the real handler but without returning `tea.Cmd`.

**Step 2: Confirm failure.** Compile errors — `applyAgentEvent` undefined.

**Step 3: Implement.**

In `internal/tui/update.go`, replace the bodies of the four existing event cases (`ThinkingDelta`, `TextDelta`, `ToolCall`, `ToolResult`) with calls to new helpers that mutate `pending`. Also extract the state-mutation portion to `applyAgentEvent(ev Event)` so tests can drive it without `tea.Cmd` plumbing. The returned `tea.Cmd` from the switch case wraps `applyAgentEvent` + `waitAgent(m.pending.events)`.

Key behaviors (see spec §Lifecycle):
- `agent.ThinkingDelta`: lazy-alloc `pending.thinking` with `Index = m.nextThinkingIdx()`, `StartedAt = time.Now()`. Append `ev.Text` to `.Text`.
- `agent.TextDelta`: if `pending.thinking != nil && thinking.EndedAt.IsZero()` → set `EndedAt = time.Now()`. Existing text-handling stays.
- `agent.ToolCall`: settle thinking same way. Append `toolCardState` with `Index = m.nextToolIdx()`, `ID`, `Name`, `Input`, `StartedAt = time.Now()`.
- `agent.ToolResult`: `findTool(id)` returns `*toolCardState` or nil; if nil create defensive zero-StartedAt card and append. Fill `Output`, set `Lines = countLines(Output)`, set `EditLines` from input (if `ev.Name == "Edit"`) via `strings.Count(new_string, "\n")+1`, set `Bytes = len(Output)` for Task, set `IsError`, set `EndedAt = time.Now()`.

Also remove the current `tea.Printf("%s", renderToolCall/Result)` calls — those are now deferred to flush in Task 7. The `ToolCall` case still flushes any pending committed text before the card is registered (keep that behavior; it preserves correct ordering of assistant text vs. tool card).

Delete `m.pending.thinkRaw = nil` from the `ToolCall` case (it conflicts with settlement-driven model).

**View() update.** The current View() at `update.go:118-162` orders: skill card → thinking (`thinkRaw`) → live tail. The spec §Lifecycle step 2 requires: thinking → tools → live tail → skill card. You must:

1. **Delete** the existing skill-card block at `update.go:129-132`.
2. **Delete** the existing thinking block at `update.go:136-139` (the `thinkRaw`-based render).
3. **Insert** the new composition *after* `parts := []string{""}` (line 125):

```go
if m.pending != nil {
	now := time.Now()
	inner := m.width - 4
	if inner < 1 {
		inner = 80
	}
	// Thinking card first — streams before the response.
	if m.pending.thinking != nil && m.pending.thinking.EndedAt.IsZero() {
		parts = append(parts, renderThinkingCard(m.theme, m.pending.thinking, now,
			m.spinner.frame, m.settings.Thinking.StreamMode, inner))
	}
	// Tool cards in start-time order (both running and settled; settled cards
	// show even before TurnDone so the user can watch them land).
	for _, tc := range m.pending.tools {
		parts = append(parts, renderToolCard(m.theme, tc, now, m.spinner.frame, inner))
	}
}
```

4. **Keep** the live-tail block at `update.go:142-145` as-is.

5. **Insert** a new skill-card block *after* the live tail (replacing what was deleted in step 1):

```go
if m.pending != nil && m.pending.skill != nil && !m.pending.skill.Flushed {
	elapsed := time.Since(m.pending.skill.StartedAt)
	parts = append(parts, renderSkillCard(m.theme, m.pending.skill, elapsed, m.spinner.frame, true))
}
```

Final order inside `parts`: leading blank → thinking → tools → live tail → skill card → input box → status bar.

**Note on `thinkRaw`.** The existing `pendingTurn.thinkRaw` field becomes unused after this refactor (thinking text now lives on `pending.thinking.Text`). Keep the field for now to avoid churn in unrelated code paths; Task 10 audits and deletes it.

**Step 4: Confirm pass.**
```bash
go test ./internal/tui/... -run 'TestHandleTool|TestHandleThinking|TestHandleText' -count=1
go test ./internal/tui/... -run 'TestRenderThinkingCard' -count=1
go build ./...
```
Expected: both PASS, build green.

**Step 5: Commit.**
```bash
git add internal/tui/render.go internal/tui/render_thinking_test.go \
       internal/tui/update.go internal/tui/update_events_test.go
git commit -m "feat(tui): route tool/thinking events into pending state; live-render cards"
```

---

### Task 7: Flush settled cards on `TurnDone` / `ErrorEvent`; record rings

**Files:**
- Modify: `internal/tui/update.go:559-598` (`ErrorEvent`, `TurnDone`)
- Modify: `internal/tui/update_events_test.go` (add flush-order + cancellation tests)

**Step 1: Write failing tests.**

```go
func TestFlushOrder_ThinkingThenToolsThenText(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	// Synthesize a complete turn.
	m.applyAgentEvent(agent.ThinkingDelta{Text: "planning"})
	m.applyAgentEvent(agent.ToolCall{ID: "t1", Name: "Bash", Input: json.RawMessage(`{}`)})
	m.applyAgentEvent(agent.ToolResult{ID: "t1", Name: "Bash", Output: "ok\n"})
	m.applyAgentEvent(agent.ToolCall{ID: "t2", Name: "Read", Input: json.RawMessage(`{}`)})
	m.applyAgentEvent(agent.ToolResult{ID: "t2", Name: "Read", Output: "a\nb\n"})
	out := m.flushTurnSettled() // returns ordered slice of rendered strings
	if len(out) < 3 {
		t.Fatalf("expected >=3 settled outputs, got %d: %v", len(out), out)
	}
	// Thinking first.
	if !strings.Contains(out[0], "💭") {
		t.Errorf("out[0] not thinking: %q", out[0])
	}
	// Tools in start order.
	if !strings.Contains(out[1], "Bash") || !strings.Contains(out[2], "Read") {
		t.Errorf("tool order wrong: %q %q", out[1], out[2])
	}
	if len(m.recentTools) != 2 || len(m.recentThinking) != 1 {
		t.Errorf("rings not populated: tools=%d thinking=%d",
			len(m.recentTools), len(m.recentThinking))
	}
}

func TestFlush_CancelsRunningTool(t *testing.T) {
	m := &Model{pending: synthPending(), theme: NewTheme(DefaultSettings().Theme)}
	m.applyAgentEvent(agent.ToolCall{ID: "t1", Name: "Bash", Input: json.RawMessage(`{}`)})
	out := m.flushTurnSettled()
	if len(out) != 1 || !strings.Contains(out[0], "cancelled") {
		t.Errorf("cancelled card not flushed: %v", out)
	}
	if !m.pending.tools[0].Cancelled {
		t.Error("Cancelled flag not set")
	}
}
```

**Step 2: Confirm failure.** `flushTurnSettled` undefined.

**Step 3: Implement.** Add:

```go
// flushTurnSettled marks any open cards as cancelled/settled, renders their
// final static forms, records them to ring buffers, and returns the rendered
// strings in chronological order (thinking → tools). Caller is responsible
// for emitting assistant text and skill card separately, which preserves the
// existing skill-card flush path.
func (m *Model) flushTurnSettled() []string {
	now := time.Now()
	inner := m.width - 4
	if inner < 1 {
		inner = 80
	}
	var out []string
	if m.pending.thinking != nil {
		if m.pending.thinking.EndedAt.IsZero() {
			m.pending.thinking.EndedAt = now
		}
		out = append(out, renderThinkingCard(m.theme, m.pending.thinking, now, 0, "full", inner))
		m.recordThinking(thinkingInvocation{
			Index:  m.pending.thinking.Index,
			Header: fmt.Sprintf("thought for %s · %d chars",
				formatElapsed(m.pending.thinking.EndedAt.Sub(m.pending.thinking.StartedAt)),
				len(m.pending.thinking.Text)),
			Body:   m.pending.thinking.Text,
		})
	}
	for _, tc := range m.pending.tools {
		if tc.EndedAt.IsZero() {
			tc.Cancelled = true
			tc.EndedAt = now
		}
		out = append(out, renderToolCard(m.theme, tc, now, 0, inner))
		m.recordTool(toolInvocation{
			Index:   tc.Index,
			Header:  fmt.Sprintf("%s · %s", tc.Name, toolSummary(tc, now)),
			Input:   toolInputPreview(tc.Name, tc.Input),
			Output:  tc.Output,
			IsError: tc.IsError,
		})
	}
	return out
}
```

Integrate into handlers. Rewrite `TurnDone` case:

```go
case agent.TurnDone:
	m.pending.done = true
	settled := m.flushTurnSettled()
	var cardCmd tea.Cmd
	if m.pending.skill != nil && !m.pending.skill.Flushed {
		m.pending.skill.Flushed = true
		elapsed := time.Since(m.pending.skill.StartedAt)
		card := renderSkillCard(m.theme, m.pending.skill, elapsed, m.spinner.frame, false)
		cardCmd = tea.Printf("%s", card)
	}
	var flush tea.Cmd
	if m.pending.committed < len(m.pending.raw) {
		tail := m.pending.raw[m.pending.committed:]
		rendered := safeGlamourRender(m.theme.Glamour(), string(tail))
		m.pending.committed = len(m.pending.raw)
		flush = tea.Printf("%s", rendered)
	}
	m.pending = nil
	m.scanner = &blockScanner{}
	m.status.state = "idle"
	m.spinner.on = false
	m.input.Focus()
	cmds := []tea.Cmd{}
	for _, s := range settled {
		cmds = append(cmds, tea.Printf("%s", s))
	}
	if cardCmd != nil {
		cmds = append(cmds, cardCmd)
	}
	if flush != nil {
		cmds = append(cmds, flush)
	}
	cmds = append(cmds, func() tea.Msg { return turnClosedMsg{} })
	return m, tea.Sequence(cmds...)
```

Settled order: `thinking → tools → skill card → assistant text → turnClosedMsg`. This matches the spec (the skill card was historically appended after tools; that pre-existing behavior stays).

Rewrite `ErrorEvent` to also flush (see spec §"Cancelled / still-running at flush"):

```go
case agent.ErrorEvent:
	errorStr := renderError(m.theme, ev.Err)
	m.status.state = "error"
	m.spinner.on = false
	var settled []string
	if m.pending != nil {
		settled = m.flushTurnSettled()
	}
	cmds := []tea.Cmd{}
	for _, s := range settled {
		cmds = append(cmds, tea.Printf("%s", s))
	}
	cmds = append(cmds, tea.Printf("%s", errorStr), waitAgent(m.pending.events))
	return m, tea.Sequence(cmds...)
```

**Step 4: Confirm pass.**
```bash
go test ./internal/tui/... -count=1 -race
```
Expected: all PASS.

**Step 5: Commit.**
```bash
git add internal/tui/update.go internal/tui/update_events_test.go
git commit -m "feat(tui): flush settled tool/thinking cards on turn end; record rings"
```

---

### Task 8: `/show-tool` + `/show-thinking` commands + handlers

**Files:**
- Modify: `internal/tui/commands.go:12-25` (add consts)
- Modify: `internal/tui/commands.go:28-42` (add suggestions)
- Modify: `internal/tui/commands.go:46-49` (BuiltinNames)
- Modify: `internal/tui/commands.go:65-84` (parser)
- Modify: `internal/tui/commands.go:131-148` (help text)
- Modify: `internal/tui/update.go` (handlers)
- Create: `internal/tui/commands_tools_test.go`

**Step 1: Write failing tests.**

```go
package tui

import (
	"testing"
)

func TestParseCommand_ShowTool(t *testing.T) {
	cmd, arg, _ := parseCommand("/show-tool 7", nil)
	if cmd != CmdShowTool || arg != "7" {
		t.Errorf("got (%q,%q)", cmd, arg)
	}
	cmd, arg, _ = parseCommand("/SHOW-TOOL", nil)
	if cmd != CmdShowTool || arg != "" {
		t.Errorf("case-insensitive no-arg: (%q,%q)", cmd, arg)
	}
}

func TestParseCommand_ShowThinking(t *testing.T) {
	cmd, arg, _ := parseCommand("/show-thinking 3", nil)
	if cmd != CmdShowThinking || arg != "3" {
		t.Errorf("got (%q,%q)", cmd, arg)
	}
}

func TestCommandSuggestions_IncludesShowToolAndThinking(t *testing.T) {
	sugg := suggestionsFor(nil)
	var seenT, seenTh bool
	for _, s := range sugg {
		if strings.HasPrefix(s.Name, "/show-tool") {
			seenT = true
		}
		if strings.HasPrefix(s.Name, "/show-thinking") {
			seenTh = true
		}
	}
	if !seenT || !seenTh {
		t.Errorf("missing suggestions: tool=%v thinking=%v", seenT, seenTh)
	}
}

func TestShowTool_MonotonicResolve(t *testing.T) {
	m := &Model{}
	m.recordTool(toolInvocation{Index: 5, Header: "Bash · 1 line · 1s", Input: "go test", Output: "ok\n"})
	out := m.resolveToolDump(5)
	if !strings.Contains(out, "Bash") || !strings.Contains(out, "ok") {
		t.Errorf("dump missing content: %q", out)
	}
	if missing := m.resolveToolDump(999); !strings.Contains(missing, "unknown") {
		t.Errorf("unknown index should produce error line: %q", missing)
	}
}
```

**Step 2: Confirm failure.**

**Step 3: Implement.**

In `internal/tui/commands.go`:
- Add consts:
```go
CmdShowTool     Command = "show-tool"
CmdShowThinking Command = "show-thinking"
```
- Append to `commandSuggestions`:
```go
{"/show-tool", "expand a previously-collapsed tool call (arg: index; default last)"},
{"/show-thinking", "expand a previously-collapsed thinking block (arg: index; default last)"},
```
- Append to `BuiltinNames`: `"show-tool"`, `"show-thinking"`.
- Add parser cases in the switch:
```go
case "show-tool":
	return CmdShowTool, arg, nil
case "show-thinking":
	return CmdShowThinking, arg, nil
```
- Extend `helpTextBuiltin` with the two new commands (follow existing formatting).

In `internal/tui/update.go`, add resolvers + handlers:

```go
func (m *Model) resolveToolDump(idx int) string {
	var inv *toolInvocation
	if idx == 0 && len(m.recentTools) > 0 {
		inv = &m.recentTools[len(m.recentTools)-1]
	} else {
		for i := range m.recentTools {
			if m.recentTools[i].Index == idx {
				inv = &m.recentTools[i]
				break
			}
		}
	}
	if inv == nil {
		return m.theme.ToolError.Render(fmt.Sprintf("unknown tool index %d", idx))
	}
	header := m.theme.ToolCardHeader.Render("● " + inv.Header)
	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteByte('\n')
	if inv.Input != "" {
		sb.WriteString(m.theme.ToolCardMeta.Render(inv.Input))
		sb.WriteString("\n---\n")
	}
	if inv.IsError {
		sb.WriteString(m.theme.ToolError.Render(inv.Output))
	} else {
		sb.WriteString(m.theme.ToolResult.Render(inv.Output))
	}
	return sb.String()
}

func (m *Model) resolveThinkingDump(idx int) string {
	var inv *thinkingInvocation
	if idx == 0 && len(m.recentThinking) > 0 {
		inv = &m.recentThinking[len(m.recentThinking)-1]
	} else {
		for i := range m.recentThinking {
			if m.recentThinking[i].Index == idx {
				inv = &m.recentThinking[i]
				break
			}
		}
	}
	if inv == nil {
		return m.theme.ToolError.Render(fmt.Sprintf("unknown thinking index %d", idx))
	}
	return m.theme.ThinkingHeader.Render("💭 "+inv.Header) + "\n---\n" +
		m.theme.Thinking.Render(inv.Body)
}
```

Wire into the command dispatcher (same place `/show-skill` is handled). Use existing helper `parseNonNegInt` (already in the skills path) to parse the numeric arg; empty arg → 0 (meaning "latest").

**Step 4: Confirm pass.**
```bash
go test ./internal/tui/... -count=1
```
Expected: all PASS.

**Step 5: Commit.**
```bash
git add internal/tui/commands.go internal/tui/update.go internal/tui/commands_tools_test.go
git commit -m "feat(tui): add /show-tool and /show-thinking slash commands"
```

---

### Task 9: Integration test — full turn lifecycle

**File:**
- Create: `internal/tui/integration_cards_test.go`

**Step 1: Write test.** Drives a `Model` through a synthetic event sequence (matches the realistic order from a provider turn): `ThinkingDelta` → `ThinkingDelta` → `TextDelta` (settles thinking) → `ToolCall` → `ToolResult` → `TextDelta` → `TurnDone`. Asserts:

1. View() during thinking shows the thinking card with body.
2. After `TextDelta`, thinking `EndedAt` is set.
3. After `ToolCall`, tool card exists in `pending.tools`; View() includes both cards.
4. After `ToolResult`, tool card has `Output`, `Lines`, `EndedAt`.
5. `flushTurnSettled()` returns thinking settled + tool settled in that order.
6. Ring buffers contain one entry each with `Index == 1`.

Use the `applyAgentEvent` hook from Task 6. No Bubbletea event loop required.

**Step 2-5:** run, implement fixes if any come up, commit.

```bash
go test ./internal/tui/... -run TestIntegration -count=1 -race
git add internal/tui/integration_cards_test.go
git commit -m "test(tui): integration test for tool/thinking card lifecycle"
```

---

### Task 10: Clean up superseded rendering + final verification

**Files:**
- Modify: `internal/tui/render.go` — verify `renderToolCall`, `renderToolResult` have no callers (superseded by `renderToolCard` + flush path); delete if unused.
- Modify: `internal/tui/theme.go` — verify `ToolHeader`, `ToolInput` styles have no callers; delete only if truly unused. **Keep `Thinking` and `ThinkingHeader`** — `resolveThinkingDump` in `update.go` (added Task 8) depends on both for the `/show-thinking` expansion output. Even though the spec's "Style retention map" marks `Thinking` for deletion, the plan's implementation of the dump function keeps it live — do not delete.
- Modify: `internal/tui/app.go` — verify `pendingTurn.thinkRaw` has no remaining callers; delete the field if the post-Task 6 View() no longer reads it.
- Modify: `README.md` — add short paragraph under the TUI section describing the new cards + `/show-tool` / `/show-thinking` commands (keep under 10 lines total; match existing brevity).

**Step 1: Audit unused symbols.**
```bash
go build ./...
```
Then for each candidate for deletion:
```bash
grep -r "renderToolCall\|renderToolResult\|ToolHeader\|ToolInput" internal/ cmd/
```
Delete only those with zero matches outside their definition file.

**Step 2: Full-suite verification.**
```bash
go vet ./...
go test ./... -count=1 -race
go build ./...
```
Expected: all pass; binary builds.

**Step 3: Manual smoke test (documented, not automated).** Launch the binary against a real provider or the fake provider from `internal/llm/fake`:
1. Ask something that triggers extended thinking — see thinking card stream then settle to `💭` one-liner.
2. Issue a tool-heavy prompt (e.g. "read file X then edit Y") — watch each tool card animate, settle with correct summary.
3. Toggle `tui.thinking.stream_mode` to `header` in settings, re-ask — confirm header-only streaming.
4. Run `/show-thinking 1` and `/show-tool 1` — full dumps print.
5. Ctrl+C mid-tool-call — confirm cancelled card appears with `◌` glyph and muted border.

**Step 4: Commit cleanup + README.**
```bash
git add internal/tui/render.go internal/tui/theme.go README.md
git commit -m "chore(tui): remove superseded tool render helpers; document new cards"
```

---

## Success Criteria

- All 10 tasks committed; `git log` shows a clean feat/test sequence.
- `go vet ./...` clean.
- `go test ./... -count=1 -race` — all packages pass.
- `go build ./...` succeeds.
- Manual smoke test (Task 10 Step 3) all five steps behave as described.
- Spec `thoughts/shared/plans/tui/2026-04-23-tool-thinking-cards-design.md` covers every user-facing behavior visible in the app after this plan.

---

## Open carry-overs for future PRs (explicitly deferred)

- Rendered-string caching per `toolCardState` for >3 parallel tool scenarios.
- Per-tool color coding beyond success/error/cancelled.
- Ring-buffer persistence across restarts.
- Interactive focus+Enter expansion (current expansion is via slash command only).
