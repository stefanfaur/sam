# Scrollback-First TUI with Streaming Markdown — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use executing-plans to implement this plan task-by-task.

**Goal:** Eliminate flicker during streaming assistant text and enable real terminal scrollback by refactoring the TUI rendering pipeline from viewport-based history to incremental `tea.Printf` commits.

**Architecture:** Replace the ticker-driven viewport system with a two-region design: immutable scrollback (via `tea.Printf`) above the live frame, and a lightweight live frame below containing only the active tail, thinking card, approval panel, status bar, and input. A `blockScanner` analyzes markdown structure to identify safe split boundaries, committing complete blocks as they finish streaming rather than re-rendering the full buffer on each tick.

**Tech Stack:** Go 1.26.2, Bubbletea 1.3.10, Glamour 1.0.0, Lipgloss 1.1.1

---

## Task 1: Create blockScanner and normalization helpers

**Files:**
- Create: `internal/tui/stream.go`
- Test: `internal/tui/stream_test.go`

**Step 1: Write the test file for blockScanner**

Create test cases covering:
- Two paragraphs separated by blank line → split at blank
- Code fence lifecycle → split after closing fence's blank line
- Fence state persistence across calls → `Advance` updates state
- CRLF normalization → normalized before scanning
- BOM stripping → only on first delta
- Setext heading → must not split between heading and underline
- HTML blocks → no split until closing tag or blank line
- Tables → split on trailing blank only
- Empty tail → returns 0
- No blank line in tail → returns 0

```go
package tui

import (
	"testing"
)

func TestBlockScannerSafeSplit(t *testing.T) {
	tests := []struct {
		name       string
		input      []rune
		wantIdx    int
		wantFenced bool // scanner state after SafeSplit
	}{
		{
			name:    "two paragraphs with blank line",
			input:   []rune("line 1\nline 2\n\nline 3"),
			wantIdx: 8, // points to start of "line 3", i.e., after the \n\n
		},
		{
			name:    "no blank line",
			input:   []rune("line 1\nline 2"),
			wantIdx: 0,
		},
		{
			name:    "empty tail",
			input:   []rune(""),
			wantIdx: 0,
		},
		{
			name:       "fence open then blank",
			input:      []rune("```\ncode\n```\n\n"),
			wantIdx:    15, // after trailing \n\n
			wantFenced: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := &blockScanner{}
			idx := scanner.SafeSplit(tt.input)
			if idx \!= tt.wantIdx {
				t.Errorf("SafeSplit(%q) = %d, want %d", string(tt.input), idx, tt.wantIdx)
			}
			if scanner.fenceOpen \!= tt.wantFenced {
				t.Errorf("after SafeSplit, fenceOpen = %v, want %v", scanner.fenceOpen, tt.wantFenced)
			}
		})
	}
}

func TestBlockScannerAdvance(t *testing.T) {
	tests := []struct {
		name      string
		prefix    []rune
		nextTail  []rune
		wantIdx   int
		wantFence bool
	}{
		{
			name:      "advance past first paragraph",
			prefix:    []rune("para\n\n"),
			nextTail:  []rune("next\n\n"),
			wantIdx:   6,
			wantFence: false,
		},
		{
			name:      "advance past fence open",
			prefix:    []rune("```\ncode\n"),
			nextTail:  []rune("```\n\n"),
			wantIdx:   5,
			wantFence: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := &blockScanner{}
			scanner.Advance(tt.prefix)
			idx := scanner.SafeSplit(tt.nextTail)
			if idx \!= tt.wantIdx {
				t.Errorf("SafeSplit after Advance = %d, want %d", idx, tt.wantIdx)
			}
		})
	}
}

func TestNormalizeLinendings(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"CRLF", "a\r\nb", "a\nb"},
		{"bare CR", "a\rb", "a\nb"},
		{"mixed", "a\r\nb\rc", "a\nb\nc"},
		{"LF only", "a\nb", "a\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLineEndings(tt.in)
			if got \!= tt.want {
				t.Errorf("normalizeLineEndings(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripBOM(t *testing.T) {
	tests := []struct {
		name string
		in   []rune
		want []rune
	}{
		{"no BOM", []rune("hello"), []rune("hello")},
		{"with BOM", []rune("\uFEFFhello"), []rune("hello")},
		{"empty", []rune(""), []rune("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripBOM(tt.in)
			if len(got) \!= len(tt.want) || string(got) \!= string(tt.want) {
				t.Errorf("stripBOM(%q) = %q, want %q", string(tt.in), string(got), string(tt.want))
			}
		})
	}
}
```

**Step 2: Implement blockScanner in stream.go**

```go
package tui

import (
	"strings"
	"unicode"
)

type blockScanner struct {
	fenceOpen   bool
	fenceMark   string // "```" or "~~~"
	fenceIndent int    // leading spaces on fence-open line (0..3)
	inHTMLBlock bool
}

// SafeSplit returns the largest rune index into tail such that tail[:idx]
// ends on a closed-block boundary. Returns 0 if no safe split exists.
// Does NOT mutate scanner state.
func (s *blockScanner) SafeSplit(tail []rune) int {
	if len(tail) == 0 {
		return 0
	}

	lines := splitLines(tail)
	committed := 0

	for i, line := range lines {
		lineStart := committed
		committed += len(line) + 1 // +1 for the \n separator
		if committed > len(tail) {
			committed = len(tail)
		}

		isSplit := false

		// Check for fence markers
		if openIdx, mark, indent := parseFenceOpen(line); openIdx >= 0 && \!s.fenceOpen && \!s.inHTMLBlock {
			s.fenceOpen = true
			s.fenceMark = mark
			s.fenceIndent = indent
			continue
		}

		// Check for fence close
		if s.fenceOpen && isFenceClose(line, s.fenceMark, s.fenceIndent) {
			s.fenceOpen = false
			s.fenceMark = ""
			s.fenceIndent = 0
			// Split candidate is after the trailing blank line following fence close
			if i+1 < len(lines) && len(strings.TrimSpace(lines[i+1])) == 0 {
				isSplit = true
			}
			continue
		}

		// Check for HTML block
		if \!s.inHTMLBlock && hasHTMLBlockStart(line) {
			s.inHTMLBlock = true
			continue
		}
		if s.inHTMLBlock && hasHTMLBlockEnd(line) {
			s.inHTMLBlock = false
			// Split candidate after blank line following HTML block
			if i+1 < len(lines) && len(strings.TrimSpace(lines[i+1])) == 0 {
				isSplit = true
			}
			continue
		}

		// Blank line can be a split candidate
		if len(strings.TrimSpace(line)) == 0 && \!s.fenceOpen && \!s.inHTMLBlock {
			// Check for setext underline rejection
			if i+1 < len(lines) && isSetextUnderline(lines[i+1]) {
				continue
			}
			isSplit = true
		}

		if isSplit {
			return lineStart + len(line) + 1
		}
	}

	return 0
}

// Advance updates scanner state to reflect having committed prefix.
func (s *blockScanner) Advance(prefix []rune) {
	lines := splitLines(prefix)
	for _, line := range lines {
		if openIdx, mark, indent := parseFenceOpen(line); openIdx >= 0 && \!s.fenceOpen && \!s.inHTMLBlock {
			s.fenceOpen = true
			s.fenceMark = mark
			s.fenceIndent = indent
		} else if s.fenceOpen && isFenceClose(line, s.fenceMark, s.fenceIndent) {
			s.fenceOpen = false
		} else if \!s.inHTMLBlock && hasHTMLBlockStart(line) {
			s.inHTMLBlock = true
		} else if s.inHTMLBlock && hasHTMLBlockEnd(line) {
			s.inHTMLBlock = false
		}
	}
}

// Helper functions

func splitLines(runes []rune) [][]rune {
	var lines [][]rune
	var current []rune
	for _, r := range runes {
		if r == '\n' {
			lines = append(lines, current)
			current = nil
		} else {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		lines = append(lines, current)
	}
	return lines
}

// parseFenceOpen checks if line opens a code fence. Returns (index, marker, indent) or (-1, "", 0).
func parseFenceOpen(line []rune) (int, string, int) {
	s := string(line)
	// Count leading spaces (0-3)
	indent := 0
	for i, ch := range s {
		if ch \!= ' ' {
			break
		}
		indent++
	}
	if indent > 3 {
		return -1, "", 0
	}

	rest := s[indent:]
	if strings.HasPrefix(rest, "```") {
		return 0, "```", indent
	}
	if strings.HasPrefix(rest, "~~~") {
		return 0, "~~~", indent
	}
	return -1, "", 0
}

// isFenceClose checks if line closes the current fence.
func isFenceClose(line []rune, fenceMark string, fenceIndent int) bool {
	s := string(line)
	indent := 0
	for i, ch := range s {
		if ch \!= ' ' {
			break
		}
		indent++
	}
	if indent > fenceIndent+3 {
		return false
	}

	rest := s[indent:]
	if \!strings.HasPrefix(rest, fenceMark) {
		return false
	}

	// Remaining must be only spaces
	tail := rest[len(fenceMark):]
	return len(strings.TrimSpace(tail)) == 0
}

// hasHTMLBlockStart checks for CommonMark type-1 HTML block start.
func hasHTMLBlockStart(line []rune) bool {
	s := strings.ToLower(strings.TrimSpace(string(line)))
	starts := []string{"<script", "<pre", "<style", "<textarea"}
	for _, start := range starts {
		if strings.HasPrefix(s, start) {
			return true
		}
	}
	return false
}

// hasHTMLBlockEnd checks if line contains the matching HTML end tag.
func hasHTMLBlockEnd(line []rune) bool {
	s := strings.ToLower(string(line))
	ends := []string{"</script>", "</pre>", "</style>", "</textarea>"}
	for _, end := range ends {
		if strings.Contains(s, end) {
			return true
		}
	}
	return false
}

// isSetextUnderline checks if line is a setext heading underline.
func isSetextUnderline(line []rune) bool {
	s := string(line)
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 {
		return false
	}
	// All = or all -
	allEq := true
	allDash := true
	for _, ch := range trimmed {
		if ch \!= '=' && ch \!= '-' {
			return false
		}
		if ch \!= '=' {
			allEq = false
		}
		if ch \!= '-' {
			allDash = false
		}
	}
	return allEq || allDash
}

// normalizeLineEndings converts CRLF and bare CR to LF.
func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// stripBOM removes leading BOM (U+FEFF) if present.
func stripBOM(runes []rune) []rune {
	if len(runes) > 0 && runes[0] == '\uFEFF' {
		return runes[1:]
	}
	return runes
}

// safeGlamourRender wraps glamour rendering with panic recovery.
func safeGlamourRender(glam *glamour.TermRenderer, text string) string {
	defer func() {
		if recover() \!= nil {
			// Return raw text on panic
		}
	}()
	if glam == nil {
		return text
	}
	rendered, err := glam.Render(text)
	if err \!= nil {
		return text
	}
	return strings.TrimRight(rendered, "\n")
}
```

**Step 3: Run tests to verify blockScanner works**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go test ./internal/tui -run TestBlockScanner -v
```

Expected: All tests PASS.

**Step 4: Verify no import errors**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go build ./internal/tui
```

Expected: Builds without error.

**Step 5: Commit**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
git add internal/tui/stream.go internal/tui/stream_test.go
git commit -m "feat(tui): add blockScanner and normalization helpers for streaming markdown"
```

---

## Task 2: Refactor Model struct and pendingTurn for scrollback-first design

**Files:**
- Modify: `internal/tui/app.go`

**Step 1: Read current app.go to identify all fields**

Already done. Current fields to remove:
- `history []renderedBlock`
- `viewport viewport.Model`
- `lastVP string`
- From `pendingTurn`: `shown`, `thinkShown`, `ticking`, `tickCount`, `rendered`, `thinkIdx`, `stablePrefix`, `stableRendered`, `toolRow`

**Step 2: Remove viewport and history fields from Model**

Replace:
```go
type Model struct {
	agent     *agent.Agent
	viewport  viewport.Model  // DELETE
	input     textarea.Model
	status    statusbarModel
	approval  *Approval
	modal     modal
	debug     debugModel
	history   []renderedBlock  // DELETE
	pending   *pendingTurn
	width     int
	height    int
	glam      *glamour.TermRenderer
	ring      *logging.Ring
	factory   ProviderFactory
	suggest   suggestState
	lastCtrlC time.Time
	lastVP    string  // DELETE
}
```

With:
```go
type Model struct {
	agent     *agent.Agent
	input     textarea.Model
	status    statusbarModel
	approval  *Approval
	modal     modal
	debug     debugModel
	pending   *pendingTurn
	width     int
	height    int
	glam      *glamour.TermRenderer
	ring      *logging.Ring
	factory   ProviderFactory
	suggest   suggestState
	lastCtrlC time.Time
	scanner   *blockScanner  // NEW
}
```

**Step 3: Simplify pendingTurn struct**

Replace:
```go
type pendingTurn struct {
	events     <-chan Event
	raw        []rune
	shown      int
	thinkRaw   []rune
	thinkShown int
	toolRow    map[string]int
	rendered   bool
	thinkIdx   int
	done       bool
	ticking    bool
	tickCount  int
	stablePrefix   string
	stableRendered string
}
```

With:
```go
type pendingTurn struct {
	events   <-chan Event
	raw      []rune  // full assistant text received so far
	committed int    // rune index in raw that has been flushed to scrollback
	thinkRaw []rune  // live thinking text (rendered in View(), never committed)
	done     bool    // TurnDone received; flush remaining tail
}
```

**Step 4: Remove viewport initialization from New()**

In `func New(...)`, remove:
```go
viewport: viewport.New(80, 20),
```

And initialize scanner:
```go
scanner: &blockScanner{},
```

Also remove the line:
```go
debug:   debugModel{viewport: viewport.New(80, 20), ring: ring},
```

Keep the debug viewport (it's still used for the Ctrl-L overlay).

**Step 5: Verify syntax**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go build ./internal/tui
```

Expected: Compilation error because `rebuildViewport` and other methods still reference removed fields. We'll fix those in subsequent tasks.

**Step 6: Commit**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
git add internal/tui/app.go
git commit -m "refactor(tui): remove viewport and history from Model struct"
```

---

## Task 3: Rewrite streaming handlers to use tea.Printf instead of ticker

**Files:**
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/messages.go`

**Step 1: Remove tickMsg and related code from messages.go**

Delete the line:
```go
type tickMsg struct{}
```

This is the only thing in messages.go related to ticking.

**Step 2: Remove tick-related handlers from update.go**

Delete these functions entirely:
- `ensureTick()`
- `handleTick()`
- `revealRate()`
- `redrawAssistantBufferPlain()`
- `redrawAssistantBufferGlam()`
- `writeAssistantBlock()`
- `redrawAssistantBufferFinal()`
- `redrawThinkingBuffer()`

Also delete these constants:
```go
const (
	typewriterTick = 20 * time.Millisecond
	glamourEveryNTicks = 4
)
```

And delete the global:
```go
var lastRender time.Time
```

**Step 3: Remove tick case from Update() switch**

In `func (m *Model) Update(...)`, remove:
```go
case tickMsg:
	return m.handleTick()
```

**Step 4: Remove rendering constants**

Delete:
```go
const (
	inputHeight  = 1 + 2 + 1
	statusHeight = 1
	renderFPSNS = 16_000_000
)
```

And add simpler ones:
```go
const (
	inputHeight  = 1 + 2 + 1  // 1 row text + 2 border rows + 1 blank separator
	statusHeight = 1
)
```

**Step 5: Rewrite resize() to not rebuild viewport**

Replace the `resize()` method with:
```go
func (m *Model) resize(w, h int) {
	m.width = w
	m.height = h
	m.status.width = w
	m.debug.viewport = viewport.New(w, h-inputHeight-statusHeight-1)
	m.input.SetWidth(w - 4) // account for border + padding
	if gl, err := glamourForWidth(w); err == nil {
		m.glam = gl
	}
}
```

**Step 6: Rewrite handleTurnDone to flush remaining tail**

In `handleAgentEvent()`, find the `case agent.TurnDone:` branch and replace it with:
```go
case agent.TurnDone:
	if m.pending == nil {
		m.status.state = "idle"
		m.input.Focus()
		return m, func() tea.Msg { return turnClosedMsg{} }
	}
	var flush tea.Cmd
	if m.pending.committed < len(m.pending.raw) {
		tail := m.pending.raw[m.pending.committed:]
		rendered := safeGlamourRender(m.glam, string(tail))
		flush = tea.Printf("%s", rendered)
	}
	m.pending = nil
	m.scanner = &blockScanner{}
	m.status.state = "idle"
	m.input.Focus()
	// Use Sequence (not Batch): flush must land in scrollback before the live frame redraws.
	return m, tea.Sequence(flush, func() tea.Msg { return turnClosedMsg{} })
```

**Step 7: Rewrite handleAgentEvent TextDelta handler**

Replace the `case agent.TextDelta:` handler with:
```go
case agent.TextDelta:
	normalized := normalizeLineEndings(ev.Text)
	m.pending.raw = append(m.pending.raw, []rune(normalized)...)
	m.status.state = "responding"

	tail := m.pending.raw[m.pending.committed:]
	idx := m.scanner.SafeSplit(tail)
	if idx == 0 {
		return m, waitAgent(m.pending.events)
	}
	prefix := tail[:idx]
	rendered := safeGlamourRender(m.glam, string(prefix))
	m.scanner.Advance(prefix)
	m.pending.committed += idx
	// Use Sequence, not Batch: printf must land in scrollback before next View() redraw.
	return m, tea.Sequence(tea.Printf("%s", rendered), waitAgent(m.pending.events))
```

**Step 8: Update ThinkingDelta handler**

Replace `case agent.ThinkingDelta:` with:
```go
case agent.ThinkingDelta:
	m.pending.thinkRaw = append(m.pending.thinkRaw, []rune(ev.Text)...)
	m.status.state = "thinking…"
	return m, waitAgent(m.pending.events)
```

(Remove the `m.ensureTick()` call.)

**Step 9: Rewrite ToolCall handler**

Replace `case agent.ToolCall:` with:
```go
case agent.ToolCall:
	m.status.state = "tool:" + ev.Name
	// Flush pending text before tool call
	var flushCmd tea.Cmd
	if m.pending.committed < len(m.pending.raw) {
		tail := m.pending.raw[m.pending.committed:]
		rendered := safeGlamourRender(m.glam, string(tail))
		flushCmd = tea.Printf("%s\n\n", rendered)
		m.pending.committed = len(m.pending.raw)
	}
	// Clear thinking card without committing
	m.pending.thinkRaw = nil
	toolCallStr := renderToolCall(ev.Name, ev.Input)
	toolCmd := tea.Printf("%s\n", toolCallStr)
	var cmds []tea.Cmd
	if flushCmd \!= nil {
		cmds = append(cmds, flushCmd)
	}
	cmds = append(cmds, toolCmd, waitAgent(m.pending.events))
	return m, tea.Sequence(cmds...)
```

**Step 10: Rewrite ToolResult handler**

Replace `case agent.ToolResult:` with:
```go
case agent.ToolResult:
	result := renderToolResult(ev.Name, ev.Output, ev.IsError)
	m.status.iter++
	return m, tea.Sequence(
		tea.Printf("%s\n", result),
		waitAgent(m.pending.events),
	)
```

**Step 11: Rewrite ErrorEvent handler**

Replace `case agent.ErrorEvent:` with:
```go
case agent.ErrorEvent:
	errorStr := renderError(ev.Err)
	m.status.state = "error"
	return m, tea.Sequence(
		tea.Printf("%s\n", errorStr),
		waitAgent(m.pending.events),
	)
```

**Step 12: Update startTurn() to print user message**

Modify `startTurn()`:
```go
func (m *Model) startTurn(text string) (tea.Model, tea.Cmd) {
	if cmd, arg := parseCommand(text); cmd \!= CmdNone {
		return m.dispatchCommand(cmd, arg)
	}

	userMsg := m.renderUserMsg(text)
	m.status.state = "thinking"
	m.status.iter = 0
	m.input.Blur()

	events := m.agent.Submit(context.Background(), text)
	m.pending = &pendingTurn{events: events}
	m.scanner = &blockScanner{}

	// Print user message to scrollback
	return m, tea.Sequence(
		tea.Printf("%s\n\n", userMsg),
		waitAgent(events),
	)
}
```

**Step 13: Update addInfo() to use tea.Printf**

Replace:
```go
func (m *Model) addInfo(text string) {
	m.history = append(m.history, renderedBlock{kind: "info", content: infoStyle.Render(text)})
	m.rebuildViewport()
}
```

With:
```go
func (m *Model) addInfo(text string) tea.Cmd {
	info := infoStyle.Render(text)
	return tea.Printf("%s\n", info)
}
```

And update all call sites in `dispatchCommand()` to chain the returned commands.

**Step 14: Verify compilation**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go build ./internal/tui
```

Expected: Errors about undefined `rebuildViewport()` and `renderThinkingCard()`. We'll fix those in the next task.

**Step 15: Commit**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
git add internal/tui/update.go internal/tui/messages.go
git commit -m "refactor(tui): remove ticker and rewrite streaming handlers to use tea.Printf"
```

---

## Task 4: Rewrite View() and implement new rendering

**Files:**
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/render.go`

**Step 1: Remove rebuildViewport() entirely**

Delete the entire `rebuildViewport()` function from update.go.

**Step 2: Rewrite View() to render only the live frame**

Replace the existing `View()` method with:
```go
func (m *Model) View() string {
	if m.debug.visible {
		return m.debug.View(m.width, m.height)
	}

	var parts []string

	// Live tail (uncommitted text)
	if m.pending \!= nil && m.pending.committed < len(m.pending.raw) {
		tail := m.pending.raw[m.pending.committed:]
		parts = append(parts, string(tail))
	}

	// Thinking card
	if m.pending \!= nil && len(m.pending.thinkRaw) > 0 {
		streaming := true // Always streaming if visible
		card := renderThinkingCard(string(m.pending.thinkRaw), streaming)
		parts = append(parts, card)
	}

	// Status bar
	parts = append(parts, m.status.View())
	parts = append(parts, "")

	// Bottom region (input, approval, modal)
	bottom := m.renderInputBox()
	if m.modal \!= nil {
		bottom = m.modal.View()
	} else if m.approval \!= nil {
		bottom = m.approval.View()
	} else if m.suggest.active {
		bottom = m.renderSuggestions() + "\n" + m.renderInputBox()
	}
	parts = append(parts, bottom)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
```

**Step 3: Add renderThinkingCard back to render.go**

Add this to `internal/tui/render.go`:
```go
func renderThinkingCard(text string, streaming bool) string {
	header := thinkingHeaderStyle.Render("✦ thinking")
	body := strings.TrimRight(text, "\n")
	return thinkingStyle.Render(header + "\n" + body)
}
```

**Step 4: Remove unused styling structs if any**

Check if `renderedBlock` type is used anywhere else. It's only used in the removed history logic, so it can stay for now (or be removed if no other code references it).

**Step 5: Update turnClosedMsg handler**

In `Update()` switch, update the `turnClosedMsg` handler:
```go
case turnClosedMsg:
	m.pending = nil
	m.scanner = &blockScanner{}
	m.status.state = "idle"
	m.status.iter = 0
	m.input.Focus()
	return m, nil
```

**Step 6: Update Ctrl-C handler for pending turns**

In `handleKey()`, update the `tea.KeyCtrlC` case to reset scanner:
```go
case tea.KeyCtrlC:
	now := time.Now()
	if m.pending \!= nil {
		m.agent.CancelCurrent()
		m.pending = nil
		m.scanner = &blockScanner{}
		m.status.state = "cancelled"
		m.lastCtrlC = now
		return m, nil
	}
	if now.Sub(m.lastCtrlC) < 500*time.Millisecond {
		return m, tea.Quit
	}
	m.lastCtrlC = now
	return m, nil
```

**Step 7: Verify compilation**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go build ./internal/tui
```

Expected: Build succeeds.

**Step 8: Run tests**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go test ./internal/tui -v
```

Expected: No new failures (existing tests may need updates, handled in integration testing).

**Step 9: Commit**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
git add internal/tui/update.go internal/tui/render.go
git commit -m "refactor(tui): rewrite View() to render only live frame, remove viewport rendering"
```

---

## Task 5: Update dispatchCommand() to return tea.Cmd and integrate addInfo()

**Files:**
- Modify: `internal/tui/update.go`

**Step 1: Update dispatchCommand signature**

Change from:
```go
func (m *Model) dispatchCommand(cmd Command, arg string) (tea.Model, tea.Cmd)
```

The return type is already correct. Just need to update the implementation to return proper commands.

**Step 2: Fix all addInfo() calls to use returned Cmd**

Replace the entire `dispatchCommand()` function:
```go
func (m *Model) dispatchCommand(cmd Command, arg string) (tea.Model, tea.Cmd) {
	switch cmd {
	case CmdQuit:
		return m, tea.Quit

	case CmdClear:
		m.pending = nil
		m.scanner = &blockScanner{}
		return m, tea.ClearScreen()

	case CmdReset:
		m.agent.Reset()
		return m, tea.ClearScreen()

	case CmdModel:
		if arg \!= "" {
			m.agent.SetModel(arg)
			m.status.model = arg
			m.input.Reset()
			m.suggest.active = false
			return m, m.addInfo("model set to " + arg)
		}
		m.modal = newModelForm(m.status.provider, m.status.model)
		m.input.Blur()
		return m, m.modal.Init()

	case CmdProvider:
		if arg \!= "" {
			if m.factory == nil {
				m.input.Reset()
				m.suggest.active = false
				return m, m.addInfo("provider switch not available (no factory)")
			}
			prov, err := m.factory(arg, m.status.model)
			if err \!= nil {
				m.input.Reset()
				m.suggest.active = false
				return m, m.addInfo("provider build failed: " + err.Error())
			}
			m.agent.SetProvider(prov)
			m.status.provider = arg
			m.input.Reset()
			m.suggest.active = false
			return m, m.addInfo("provider set to " + arg)
		}
		m.modal = newProviderForm(m.status.provider, m.factory)
		m.input.Blur()
		return m, m.modal.Init()

	case CmdAuth:
		m.modal = newAuthForm(m.status.provider, m.factory)
		m.input.Blur()
		return m, m.modal.Init()

	case CmdCwd:
		m.input.Reset()
		m.suggest.active = false
		return m, m.addInfo("cwd: " + m.agent.LaunchDir())

	case CmdHelp:
		m.input.Reset()
		m.suggest.active = false
		return m, m.addInfo(helpText)

	case CmdUnknown:
		m.input.Reset()
		m.suggest.active = false
		return m, m.addInfo("unknown command: /" + arg)
	}

	m.input.Reset()
	m.suggest.active = false
	return m, nil
}
```

**Step 3: Verify compilation**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go build ./internal/tui
```

Expected: Build succeeds.

**Step 4: Commit**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
git add internal/tui/update.go
git commit -m "refactor(tui): update dispatchCommand to return proper tea.Cmd from addInfo()"
```

---

## Task 6: Wire up Ctrl-L debug toggle with alt-screen enter/exit

**Files:**
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/debug.go`

**Step 1: Update Ctrl-L handler to wrap with alt-screen**

In `handleKey()`, replace the `tea.KeyCtrlL` case with:
```go
case tea.KeyCtrlL:
	m.debug.visible = \!m.debug.visible
	if m.debug.visible {
		return m, tea.EnterAltScreen
	}
	return m, tea.ExitAltScreen
```

**Step 2: Verify debug view still works**

Check `internal/tui/debug.go` to ensure it uses a viewport correctly (it should, since debug has its own viewport).

**Step 3: Verify compilation**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go build ./internal/tui
```

Expected: Build succeeds.

**Step 4: Commit**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
git add internal/tui/update.go internal/tui/debug.go
git commit -m "feat(tui): wire Ctrl-L debug toggle with alt-screen enter/exit"
```

---

## Task 7: Integration testing and stress tests

**Files:**
- Test: Manual testing in interactive mode

**Step 1: Build and test basic streaming**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go build -o sam ./cmd/sam
SAM_PROVIDER=fake sam -p "write a short poem about Go"
```

Expected: 
- No flicker during streaming
- Output appears in terminal scrollback and is scrollable
- No errors in debug log

**Step 2: Test long response with code blocks**

```bash
SAM_PROVIDER=fake sam -p "write a hello world program in Go"
```

Expected:
- Code block finalized without extra rendered lines
- No duplicate rendering of the same content

**Step 3: Test terminal resize during streaming**

```bash
SAM_PROVIDER=fake sam
# Type a prompt, wait for streaming to start
# Resize terminal (drag window edge or change terminal size)
```

Expected:
- Live tail re-flows at new width
- Input box and status bar reposition
- No crashes or display corruption

**Step 4: Test Ctrl-C during streaming**

```bash
SAM_PROVIDER=fake sam
# Type a prompt, wait for streaming to start
# Press Ctrl-C once
```

Expected:
- Stream cancels
- No orphan text in the live frame
- Status shows "cancelled"

**Step 5: Test /clear command**

At idle:
```bash
/clear
```

Expected:
- Screen clears
- Scrollback remains (can scroll up to see prior history)

During streaming:
```bash
# Type prompt, wait for streaming, then type /clear
```

Expected:
- Screen clears
- Stream cancels
- Scrollback remains

**Step 6: Test /reset command**

```bash
/reset
```

Expected:
- Agent state clears (conversation history forgotten)
- Screen clears
- Scrollback may or may not remain (per design decision)

**Step 7: Test approval flow mid-stream**

(Requires a tool that needs approval in the fake provider or a configured real provider.)

Expected:
- Approval panel appears
- Scrollback and streaming tail remain visible above it
- Approve/deny button works
- Stream resumes cleanly

**Step 8: Test Ctrl-L debug toggle**

During streaming:
```bash
Ctrl-L
```

Expected:
- Alt-screen overlay appears with log ring
- Toggle Ctrl-L again to exit
- Live frame redraws intact below the overlay

**Step 9: Test terminal scrollback**

```bash
SAM_PROVIDER=fake sam
# Run several turns to generate scrollback
# Use terminal's scroll wheel or Shift+PageUp to scroll up
```

Expected:
- Scrollbar appears
- Can scroll to see prior turns
- Tea input/status still visible at bottom of live frame

**Step 10: Check no regressions in existing flows**

- Suggestion dropdown still works (type `/` then arrow keys)
- Input box multi-line still works (Shift+Enter)
- Modal forms still work (`/model`, `/provider`)

**Step 11: Commit results**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
git add -A  # any code fixes from testing
git commit -m "test(tui): integration and stress testing for scrollback-first design"
```

---

## Task 8: Code review and polish pass

**Files:**
- Review: All modified files

**Step 1: Code review checklist**

- [ ] All `tea.Sequence` calls use correct order (Printf before View redraws)
- [ ] No lingering references to removed `history`, `viewport`, `lastVP` fields
- [ ] `blockScanner` state is correctly reset on `/clear` and `/reset`
- [ ] `addInfo()` returns `tea.Cmd`; all callers chain correctly
- [ ] No unused imports or constants
- [ ] Error messages are user-friendly
- [ ] Debug overlay still functional

**Step 2: Static analysis**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go vet ./internal/tui
```

Expected: No warnings.

**Step 3: Format code**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
gofmt -w internal/tui/
```

**Step 4: Run full test suite**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
go test ./internal/tui -v
go test ./internal/agent -v
go test ./internal/tools -v
```

Expected: All tests pass.

**Step 5: Update README if needed**

Check if `thoughts/shared/plans/sam/2026-04-22-foundation.md` references the TUI architecture. Update if references are stale.

**Step 6: Commit polish**

```bash
cd /Users/stefanfaur/Desktop/work/ai-tools/sam
git add internal/tui/
git commit -m "polish(tui): code review, formatting, and test fixes"
```

---

## File Changes Summary

| File | Change Type | Rationale |
|---|---|---|
| `internal/tui/stream.go` | Create | Block scanner for safe markdown splitting |
| `internal/tui/stream_test.go` | Create | Unit tests for blockScanner |
| `internal/tui/app.go` | Modify | Remove viewport/history; add scanner |
| `internal/tui/update.go` | Modify | Rewrite handlers; remove ticker; use tea.Printf |
| `internal/tui/messages.go` | Modify | Remove tickMsg |
| `internal/tui/render.go` | Modify | Add renderThinkingCard |
| `internal/tui/debug.go` | Modify | Wire alt-screen toggle |

---

## Expected Outcomes

✓ No visible flicker during streaming  
✓ Prior turns appear in terminal scrollback  
✓ Glamour styling quality preserved for finalized blocks  
✓ All existing flows (approval, thinking, tools) continue to work  
✓ No regression to UX or performance  

---

## Risk Mitigations

| Risk | Mitigation |
|---|---|
| Malformed markdown (unclosed fence) blocks commits until TurnDone | Acceptable — full flush at end via safeGlamourRender with panic recovery |
| tea.Printf ordering | Use tea.Sequence, not Batch |
| Resize narrowing old output | Industry-standard behavior; no mitigation |
| Losing typewriter "feel" | Streaming still visible via tail; blocks land in bursts |
| Thinking card vanishing | By design; documented behavior |

