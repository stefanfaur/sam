package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stefanfaur/sam/internal/agent"
	"github.com/stefanfaur/sam/internal/skills"
)

const (
	// input: 1 row text + 2 border rows + 1 blank separator
	inputHeight  = 1 + 2 + 1
	statusHeight = 1
)

func (m *Model) Init() tea.Cmd {
	// Print a small banner up front so the TUI has breathing room above the
	// input box when launched (otherwise the status bar/input sits flush
	// against the shell prompt).
	banner := lipgloss.NewStyle().
		Foreground(m.theme.Accent).
		Bold(true).
		Render("✦ sam")
	tag := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Italic(true).
		Render(" — type /help for commands")
	return tea.Sequence(
		tea.Printf("\n%s%s\n", banner, tag),
		m.input.Focus(),
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case agentEventMsg:
		return m.handleAgentEvent(msg)

	case submitMsg:
		return m.startTurn(msg.text)

	case turnClosedMsg:
		m.pending = nil
		m.scanner = &blockScanner{}
		m.status.state = "idle"
		m.status.iter = 0
		m.spinner.on = false
		m.input.Focus()
		return m, nil

	case spinnerTickMsg:
		if m.pending == nil {
			m.spinner.on = false
			return m, nil
		}
		m.spinner.advance()
		return m, tea.Tick(spinnerTickInterval, func(time.Time) tea.Msg { return spinnerTickMsg{} })

	case quitMsg:
		return m, tea.Quit
	}

	// route to modal / approval if active
	if m.modal != nil {
		cmd := m.modal.Update(msg)
		if m.modal.Done() {
			applyCmd := m.modal.Apply(m)
			m.modal = nil
			m.input.Focus()
			return m, tea.Batch(cmd, applyCmd)
		}
		return m, cmd
	}
	if m.approval != nil {
		cmd := m.approval.Update(msg)
		if m.approval.done {
			a := m.approval
			m.approval = nil
			m.input.Focus()
			a.req.Respond(a.decide())
			return m, tea.Batch(cmd, waitAgent(m.pending.events))
		}
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refreshSuggestions()
	return m, cmd
}

func (m *Model) resize(w, h int) {
	m.width = w
	m.height = h
	m.status.width = w
	dbgH := h - inputHeight - statusHeight - 1
	if dbgH < 3 {
		dbgH = 3
	}
	m.debug.viewport = viewport.New(w, dbgH)
	m.input.SetWidth(w - 4)
	m.theme.Apply(w)
}

func (m *Model) View() string {
	if m.debug.visible {
		return m.debug.View(m.width, m.height)
	}

	// Leading blank line so the live view (input box + status) has breathing
	// room against the last flushed assistant line in scrollback.
	parts := []string{""}

	if m.pending != nil {
		now := time.Now()
		inner := m.width - 4
		if inner < 1 {
			inner = 80
		}
		if m.pending.thinking != nil && m.pending.thinking.EndedAt.IsZero() {
			parts = append(parts, renderThinkingCard(m.theme, m.pending.thinking, now,
				m.spinner.frame, m.settings.Thinking.StreamMode, inner))
		}
		for _, tc := range m.pending.tools {
			parts = append(parts, renderToolCard(m.theme, tc, now, m.spinner.frame, inner))
		}
	}

	// Live tail (uncommitted assistant text) — the actual response.
	if m.pending != nil && m.pending.committed < len(m.pending.raw) {
		tail := string(m.pending.raw[m.pending.committed:])
		parts = append(parts, tail)
	}

	// Animated skill invocation card (shown while a slash-invoked skill's
	// turn is still running).
	if m.pending != nil && m.pending.skill != nil && !m.pending.skill.Flushed {
		elapsed := time.Since(m.pending.skill.StartedAt)
		parts = append(parts, renderSkillCard(m.theme, m.pending.skill, elapsed, m.spinner.frame, true))
	}

	bottom := m.renderInputBox()
	if m.modal != nil {
		bottom = m.modal.View()
	} else if m.approval != nil {
		bottom = m.approval.View()
	} else if m.suggest.active {
		bottom = m.renderSuggestions() + "\n" + m.renderInputBox()
	}
	parts = append(parts, bottom)

	for _, row := range m.StatusBar() {
		parts = append(parts, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) renderInputBox() string {
	style := m.theme.InputBox
	if m.input.Focused() {
		style = m.theme.InputBoxFocus
	}
	if m.width > 4 {
		style = style.Width(m.width - 2)
	}
	return style.Render(m.input.View())
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.modal != nil {
		cmd := m.modal.Update(msg)
		if m.modal.Done() {
			applyCmd := m.modal.Apply(m)
			m.modal = nil
			m.input.Focus()
			return m, tea.Batch(cmd, applyCmd)
		}
		return m, cmd
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		now := time.Now()
		if m.pending != nil {
			m.agent.CancelCurrent()
			m.pending = nil
			m.scanner = &blockScanner{}
			m.status.state = "cancelled"
			m.spinner.on = false
			m.lastCtrlC = now
			m.input.Focus()
			return m, nil
		}
		if now.Sub(m.lastCtrlC) < 500*time.Millisecond {
			return m, tea.Quit
		}
		m.lastCtrlC = now
		return m, nil

	case tea.KeyCtrlD:
		if strings.TrimSpace(m.input.Value()) == "" {
			return m, tea.Quit
		}

	case tea.KeyCtrlL:
		m.debug.visible = !m.debug.visible
		if m.debug.visible {
			return m, tea.EnterAltScreen
		}
		return m, tea.ExitAltScreen

	case tea.KeyEsc:
		// Suggestion menu dismissal beats cancel routing.
		if m.suggest.active {
			m.suggest.active = false
			return m, nil
		}
		// Approval prompt: let it consume the key (it has its own Esc handling).
		if m.approval != nil {
			return m, m.approval.Update(msg)
		}
		now := time.Now()
		if m.pending != nil {
			window := m.escDoubleWindow
			if window <= 0 {
				window = 500 * time.Millisecond
			}
			if !m.lastEscTime.IsZero() && now.Sub(m.lastEscTime) < window {
				// Esc-Esc within window: abort the whole turn.
				m.agent.CancelTurn(agent.CancelModeAbort)
				m.lastEscTime = time.Time{}
				m.status.state = "cancelling"
				return m, nil
			}
			// Single Esc with a turn live: cancel the current dispatch unit.
			m.agent.CancelTurn(agent.CancelModeGranular)
			m.lastEscTime = now
			return m, nil
		}
		// Idle: clear a non-empty input draft on the first Esc.
		if m.input.Value() != "" {
			m.input.Reset()
			m.adjustInputHeight()
			m.lastEscTime = now
			return m, nil
		}
		// Idle empty input: reserved for future double-Esc semantics.
		m.lastEscTime = now
		return m, nil

	case tea.KeyUp:
		if m.suggest.active {
			if m.suggest.selected > 0 {
				m.suggest.selected--
			}
			return m, nil
		}

	case tea.KeyDown:
		if m.suggest.active {
			if m.suggest.selected < len(m.suggest.matches)-1 {
				m.suggest.selected++
			}
			return m, nil
		}

	case tea.KeyTab:
		if m.suggest.active && len(m.suggest.matches) > 0 {
			m.completeSuggestion()
			return m, nil
		}

	case tea.KeyEnter:
		if m.suggest.active && len(m.suggest.matches) > 0 {
			m.completeSuggestion()
			return m, nil
		}
		if m.approval != nil {
			cmd := m.approval.Update(msg)
			if m.approval.done {
				a := m.approval
				m.approval = nil
				m.input.Focus()
				a.req.Respond(a.decide())
				return m, tea.Batch(cmd, waitAgent(m.pending.events))
			}
			return m, cmd
		}
		if m.pending != nil {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		// Alt+Enter always inserts a newline (textarea keymap matches "alt+enter").
		if msg.Alt {
			m.growInputForNewline()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		// Trailing backslash continuation: "foo\" + Enter becomes "foo\n".
		raw := m.input.Value()
		if strings.HasSuffix(raw, "\\") {
			m.input.SetValue(strings.TrimSuffix(raw, "\\"))
			m.input.CursorEnd()
			m.growInputForNewline()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
			return m, cmd
		}
		text := strings.TrimSpace(raw)
		if text == "" {
			return m, nil
		}
		m.input.Reset()
		m.adjustInputHeight()
		m.suggest.active = false
		return m.startTurn(text)
	}

	if m.approval != nil {
		return m, m.approval.Update(msg)
	}
	// Pre-grow input for keys that will insert a newline so the textarea
	// viewport has room on the new line and doesn't scroll line 0 (with the
	// prompt arrow) out of view.
	if msg.Type == tea.KeyCtrlJ || msg.String() == "shift+enter" {
		m.growInputForNewline()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.adjustInputHeight()
	m.refreshSuggestions()
	return m, cmd
}

// adjustInputHeight grows the textarea to fit its content (counting
// soft-wrapped rows, not just logical lines), clamped so the input box
// doesn't swallow the entire screen on huge pastes.
const minInputHeightCap = 10

func (m *Model) inputHeightCap() int {
	cap := minInputHeightCap
	if m.height > 0 {
		if h := m.height / 2; h > cap {
			cap = h
		}
	}
	return cap
}

func (m *Model) adjustInputHeight() {
	m.setInputHeight(m.visualLineCount())
}

// visualLineCount counts the number of terminal rows the current input
// will occupy after soft-wrapping, matching what bubbles/textarea renders.
// Approximates the textarea's word-wrap with ceil(width/lineWidth); off by
// at most a row at word boundaries, which is acceptable for sizing.
func (m *Model) visualLineCount() int {
	width := m.input.Width()
	if width <= 0 {
		return m.input.LineCount()
	}
	val := m.input.Value()
	if val == "" {
		return 1
	}
	total := 0
	for _, line := range strings.Split(val, "\n") {
		w := lipgloss.Width(line)
		if w == 0 {
			total++
			continue
		}
		rows := (w + width - 1) / width
		if rows < 1 {
			rows = 1
		}
		total += rows
	}
	if total < 1 {
		total = 1
	}
	return total
}

func (m *Model) setInputHeight(n int) {
	if n < 1 {
		n = 1
	}
	if cap := m.inputHeightCap(); n > cap {
		n = cap
	}
	if n != m.input.Height() {
		m.input.SetHeight(n)
	}
}

// growInputForNewline enlarges the input box ahead of a newline insertion so
// the textarea's internal viewport has room for the cursor on the new line
// instead of scrolling the first line (prompt arrow) out of view.
func (m *Model) growInputForNewline() {
	m.setInputHeight(m.visualLineCount() + 1)
}

func (m *Model) refreshSuggestions() {
	v := strings.TrimLeft(m.input.Value(), " ")
	if !strings.HasPrefix(v, "/") {
		m.suggest.active = false
		m.suggest.matches = nil
		m.suggest.selected = 0
		return
	}
	if strings.ContainsRune(v, ' ') {
		m.suggest.active = false
		return
	}
	var matches []string
	for _, s := range suggestionsFor(m.skills) {
		// Compare against the slash portion only (ignore " <argument-hint>").
		slash := s.Name
		if i := strings.IndexByte(slash, ' '); i > 0 {
			slash = slash[:i]
		}
		if strings.HasPrefix(slash, v) {
			matches = append(matches, s.Name+"  "+s.Help)
		}
	}
	m.suggest.matches = matches
	m.suggest.active = len(matches) > 0
	if m.suggest.selected >= len(matches) {
		m.suggest.selected = 0
	}
}

func (m *Model) completeSuggestion() {
	if len(m.suggest.matches) == 0 {
		return
	}
	chosen := strings.Fields(m.suggest.matches[m.suggest.selected])[0]
	m.input.SetValue(chosen + " ")
	m.input.CursorEnd()
	m.suggest.active = false
}

func (m *Model) renderSuggestions() string {
	var b strings.Builder
	for i, s := range m.suggest.matches {
		if i == m.suggest.selected {
			b.WriteString(m.theme.SuggestSelected.Render("› " + s))
		} else {
			b.WriteString(m.theme.Suggest.Render("  " + s))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) startTurn(text string) (tea.Model, tea.Cmd) {
	if cmd, arg, sk := parseCommand(text, m.skills); cmd != CmdNone {
		return m.dispatchCommand(cmd, arg, sk)
	}

	userMsg := m.renderUserMsg(text)
	m.status.state = "thinking"
	m.status.iter = 0
	m.status.turnIn = 0
	m.status.turnOut = 0
	m.status.turnCacheRead = 0
	m.turnStart = time.Now()
	m.spinner.on = m.settings.Statusbar.Spinner
	m.input.Blur()

	events := m.agent.Submit(context.Background(), text)
	m.pending = &pendingTurn{events: events}
	m.scanner = &blockScanner{}

	return m, tea.Sequence(
		tea.Printf("%s", userMsg),
		tea.Batch(
			waitAgent(events),
			tea.Tick(spinnerTickInterval, func(time.Time) tea.Msg { return spinnerTickMsg{} }),
		),
	)
}

func (m *Model) dispatchCommand(cmd Command, arg string, sk *skills.Skill) (tea.Model, tea.Cmd) {
	switch cmd {
	case CmdQuit:
		return m, tea.Quit

	case CmdClear:
		m.agent.ClearHistory()
		m.pending = nil
		m.scanner = &blockScanner{}
		m.status.turnIn = 0
		m.status.turnOut = 0
		m.status.turnCacheRead = 0
		m.status.sessionIn = 0
		m.status.sessionOut = 0
		m.input.Reset()
		m.suggest.active = false
		return m, m.clearAndAnchorBottom()

	case CmdReset:
		m.agent.Reset()
		m.pending = nil
		m.scanner = &blockScanner{}
		m.status.lastIterIn = 0
		m.status.turnIn = 0
		m.status.turnOut = 0
		m.status.turnCacheRead = 0
		m.status.sessionIn = 0
		m.status.sessionOut = 0
		m.input.Reset()
		m.suggest.active = false
		return m, m.clearAndAnchorBottom()

	case CmdModel:
		m.input.Reset()
		m.suggest.active = false
		if arg != "" {
			return m, m.applyModelSpec(arg)
		}
		m.modal = newModelForm(m.status.provider, m.status.model, m.providers[m.status.provider].Models)
		m.input.Blur()
		return m, m.modal.Init()

	case CmdProvider:
		m.input.Reset()
		m.suggest.active = false
		if arg != "" {
			return m, m.switchProvider(arg, "")
		}
		m.modal = newProviderForm(m.providers, m.status.provider)
		m.input.Blur()
		return m, m.modal.Init()

	case CmdAuth:
		m.input.Reset()
		m.suggest.active = false
		return m, m.handleAuth(arg)

	case CmdSettings:
		m.input.Reset()
		m.suggest.active = false
		m.modal = newSettingsModal(m.settings, m.status.provider, m.factory, m.skills, m.providers)
		m.input.Blur()
		return m, m.modal.Init()

	case CmdCwd:
		m.input.Reset()
		m.suggest.active = false
		return m, m.addInfo("cwd: " + m.agent.LaunchDir())

	case CmdHelp:
		m.input.Reset()
		m.suggest.active = false
		return m, m.addInfo(helpTextFor(m.skills))

	case CmdSkill:
		m.input.Reset()
		m.suggest.active = false
		return m.startSkillTurn(sk, arg)

	case CmdReloadSkills:
		m.input.Reset()
		m.suggest.active = false
		return m, m.reloadSkills()

	case CmdShowSkill:
		m.input.Reset()
		m.suggest.active = false
		return m, m.showSkill(arg)

	case CmdShowTool:
		m.input.Reset()
		m.suggest.active = false
		return m, m.showTool(arg)

	case CmdShowThinking:
		m.input.Reset()
		m.suggest.active = false
		return m, m.showThinking(arg)

	case CmdUnknown:
		m.input.Reset()
		m.suggest.active = false
		return m, m.addInfo("unknown command: /" + arg)
	}

	m.input.Reset()
	m.suggest.active = false
	return m, nil
}

func (m *Model) addInfo(text string) tea.Cmd {
	return tea.Printf("%s", m.theme.Info.Render(text))
}

// clearAndAnchorBottom empties the scrollback and pushes the live view
// (input + status bar) to the bottom of the terminal by printing enough
// blank lines to fill the height above.
func (m *Model) clearAndAnchorBottom() tea.Cmd {
	// Live view footprint: 1 leading blank + input box (3) + up to 2 status rows = ~6.
	const liveRows = 6
	blanks := m.height - liveRows
	if blanks < 0 {
		blanks = 0
	}
	return tea.Sequence(
		tea.ClearScreen,
		tea.Printf("%s", strings.Repeat("\n", blanks)),
	)
}

func (m *Model) handleAgentEvent(msg agentEventMsg) (tea.Model, tea.Cmd) {
	if m.pending == nil {
		return m, nil
	}
	switch ev := msg.Ev.(type) {
	case agent.SkillInvoked:
		// Record body so /show-skill can expand later; drive the animated
		// card from pending.skill while the turn runs. Card flushes to
		// scrollback on TurnDone.
		m.recordInvoke(skillInvocation{Header: ev.Header, Body: ev.Body, Source: ev.Source})
		idx := len(m.recentInvokes) - 1
		m.pending.skill = &skillCardState{
			Header:    ev.Header,
			Body:      ev.Body,
			Source:    ev.Source,
			Index:     idx,
			StartedAt: time.Now(),
		}
		return m, waitAgent(m.pending.events)

	case agent.MessageStart:
		return m, waitAgent(m.pending.events)

	case agent.ThinkingDelta:
		m.applyAgentEvent(ev)
		m.status.state = "thinking…"
		return m, waitAgent(m.pending.events)

	case agent.TextDelta:
		m.applyAgentEvent(ev)
		m.status.state = "responding"

		cmds := []tea.Cmd{}
		if think := m.flushSettledThinking(); think != "" {
			cmds = append(cmds, tea.Printf("%s", think))
		}

		tail := m.pending.raw[m.pending.committed:]
		idx := m.scanner.SafeSplit(tail)
		if idx != 0 {
			prefix := tail[:idx]
			rendered := m.pending.padAssistant(safeGlamourRender(m.theme.Glamour(), string(prefix)))
			m.scanner.Advance(prefix)
			m.pending.committed += idx
			cmds = append(cmds, tea.Printf("%s", rendered))
		}
		cmds = append(cmds, waitAgent(m.pending.events))
		return m, tea.Sequence(cmds...)

	case agent.ToolCall:
		m.status.state = "tool:" + ev.Name
		cmds := []tea.Cmd{}
		if m.pending.committed < len(m.pending.raw) {
			tail := m.pending.raw[m.pending.committed:]
			rendered := m.pending.padAssistant(safeGlamourRender(m.theme.Glamour(), string(tail)))
			m.scanner.Advance(tail)
			m.pending.committed = len(m.pending.raw)
			cmds = append(cmds, tea.Printf("%s", rendered))
		}
		m.applyAgentEvent(ev)
		if think := m.flushSettledThinking(); think != "" {
			cmds = append(cmds, tea.Printf("%s", think))
		}
		cmds = append(cmds, waitAgent(m.pending.events))
		return m, tea.Sequence(cmds...)

	case agent.ToolResult:
		m.applyAgentEvent(ev)
		m.status.iter++
		cmds := []tea.Cmd{}
		if card := m.flushSettledTool(ev.ID); card != "" {
			cmds = append(cmds, tea.Printf("%s", card))
		}
		cmds = append(cmds, waitAgent(m.pending.events))
		return m, tea.Sequence(cmds...)

	case agent.ApprovalRequest:
		m.status.state = "awaiting-approval"
		m.approval = newApproval(ev)
		m.input.Blur()
		return m, m.approval.Init()

	case agent.UsageEvent:
		m.status.lastIterIn = ev.InputTokens
		m.status.turnIn += ev.InputTokens
		m.status.turnOut += ev.OutputTokens
		m.status.turnCacheRead += ev.CacheReadInput
		m.status.sessionIn += ev.InputTokens
		m.status.sessionOut += ev.OutputTokens
		return m, waitAgent(m.pending.events)

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
			rendered := m.pending.padAssistant(safeGlamourRender(m.theme.Glamour(), string(tail)))
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
	}

	return m, waitAgent(m.pending.events)
}

// startSkillTurn kicks off a turn whose user message is a rendered skill body.
// The agent emits SkillInvoked first so the TUI can paint the collapsed header.
func (m *Model) startSkillTurn(sk *skills.Skill, args string) (tea.Model, tea.Cmd) {
	if sk == nil {
		return m, m.addInfo("skill not found")
	}
	m.status.state = "thinking"
	m.status.iter = 0
	m.status.turnIn = 0
	m.status.turnOut = 0
	m.status.turnCacheRead = 0
	m.turnStart = time.Now()
	m.spinner.on = m.settings.Statusbar.Spinner
	m.input.Blur()

	name := sk.Name
	if sk.Shadowed {
		name = sk.RootLabel + ":" + sk.Name
	}
	events, err := m.agent.SubmitSkill(context.Background(), name, args)
	if err != nil {
		m.status.state = "idle"
		m.spinner.on = false
		m.input.Focus()
		return m, m.addInfo(fmt.Sprintf("skill error: %v", err))
	}
	m.pending = &pendingTurn{events: events}
	m.scanner = &blockScanner{}
	return m, tea.Batch(
		waitAgent(events),
		tea.Tick(spinnerTickInterval, func(time.Time) tea.Msg { return spinnerTickMsg{} }),
	)
}

// showSkill prints the body of a previously-invoked skill. arg is the 0-based
// index into recentInvokes (empty = latest). Negative or out-of-range indices
// yield an error toast.
func (m *Model) showSkill(arg string) tea.Cmd {
	if len(m.recentInvokes) == 0 {
		return m.addInfo("no skill invocations yet")
	}
	idx := len(m.recentInvokes) - 1
	if arg != "" {
		n, err := parseNonNegInt(arg)
		if err != nil {
			return m.addInfo("show-skill: bad index: " + arg)
		}
		if n >= len(m.recentInvokes) {
			return m.addInfo(fmt.Sprintf("show-skill: index %d out of range (have %d)", n, len(m.recentInvokes)))
		}
		idx = n
	}
	inv := m.recentInvokes[idx]
	header := m.theme.UserMsg.Render(fmt.Sprintf("▼ %s  (%s)  [index %d]", inv.Header, inv.Source, idx))
	body := safeGlamourRender(m.theme.Glamour(), inv.Body)
	return tea.Printf("%s\n%s", header, body)
}

func (m *Model) showTool(arg string) tea.Cmd {
	if len(m.recentTools) == 0 {
		return m.addInfo("no tool invocations yet")
	}
	idx := 0
	if arg != "" {
		n, err := parseNonNegInt(arg)
		if err != nil {
			return m.addInfo("show-tool: bad index: " + arg)
		}
		idx = n
	}
	return tea.Printf("%s", m.resolveToolDump(idx))
}

func (m *Model) showThinking(arg string) tea.Cmd {
	if len(m.recentThinking) == 0 {
		return m.addInfo("no thinking blocks yet")
	}
	idx := 0
	if arg != "" {
		n, err := parseNonNegInt(arg)
		if err != nil {
			return m.addInfo("show-thinking: bad index: " + arg)
		}
		idx = n
	}
	return tea.Printf("%s", m.resolveThinkingDump(idx))
}

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
	return m.theme.ThinkingHeader.Render(thinkingGlyph+" "+inv.Header) + "\n---\n" +
		m.theme.Thinking.Render(inv.Body)
}

func parseNonNegInt(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

func (m *Model) reloadSkills() tea.Cmd {
	if m.skills == nil {
		return m.addInfo("skills: registry not configured")
	}
	if err := m.skills.Reload(); err != nil {
		return m.addInfo("skills reload failed: " + err.Error())
	}
	m.agent.RebuildSkillCatalog()
	var enabled, shadowed, errors int
	for _, sk := range m.skills.List() {
		if sk.LoadError != nil {
			errors++
			continue
		}
		if sk.Shadowed {
			shadowed++
		}
		if sk.Enabled {
			enabled++
		}
	}
	return m.addInfo(fmt.Sprintf("skills reloaded: %d enabled, %d shadowed, %d errors", enabled, shadowed, errors))
}

// flushSettledThinking removes a settled thinking card from pending state,
// records it to the ring, and returns the rendered card for tea.Printf.
// Returns empty string if no thinking card or it is still live.
func (m *Model) flushSettledThinking() string {
	if m.pending == nil || m.pending.thinking == nil {
		return ""
	}
	th := m.pending.thinking
	if th.EndedAt.IsZero() {
		return ""
	}
	now := time.Now()
	inner := m.width - 4
	if inner < 1 {
		inner = 80
	}
	rendered := renderThinkingCard(m.theme, th, now, 0, "full", inner)
	m.recordThinking(thinkingInvocation{
		Index: th.Index,
		Header: fmt.Sprintf("thought for %s · %d chars",
			formatElapsed(th.EndedAt.Sub(th.StartedAt)), len(th.Text)),
		Body: th.Text,
	})
	m.pending.thinking = nil
	m.pending.cardFlushedSince = true
	return rendered
}

// flushSettledTool removes the settled tool card matching id from pending
// state, records it to the ring, and returns the rendered card.
func (m *Model) flushSettledTool(id string) string {
	if m.pending == nil {
		return ""
	}
	for i, tc := range m.pending.tools {
		if tc.ID != id {
			continue
		}
		if tc.EndedAt.IsZero() {
			return ""
		}
		now := time.Now()
		inner := m.width - 4
		if inner < 1 {
			inner = 80
		}
		rendered := renderToolCard(m.theme, tc, now, 0, inner)
		m.recordTool(toolInvocation{
			Index:   tc.Index,
			Header:  fmt.Sprintf("%s · %s", tc.Name, toolSummary(tc, now)),
			Input:   toolInputPreview(tc.Name, tc.Input),
			Output:  tc.Output,
			IsError: tc.IsError,
		})
		m.pending.tools = append(m.pending.tools[:i], m.pending.tools[i+1:]...)
		m.pending.cardFlushedSince = true
		return rendered
	}
	return ""
}

// flushTurnSettled marks any open cards as cancelled/settled, renders their
// final static forms, records them to ring buffers, and returns the rendered
// strings in chronological order (thinking → tools).
func (m *Model) flushTurnSettled() []string {
	if m.pending == nil {
		return nil
	}
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
			Index: m.pending.thinking.Index,
			Header: fmt.Sprintf("thought for %s · %d chars",
				formatElapsed(m.pending.thinking.EndedAt.Sub(m.pending.thinking.StartedAt)),
				len(m.pending.thinking.Text)),
			Body: m.pending.thinking.Text,
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
	if len(out) > 0 {
		m.pending.cardFlushedSince = true
	}
	return out
}

// applyAgentEvent mutates pending state in response to an agent event. It
// does not emit tea.Cmds; callers are responsible for scheduling follow-up
// work (waitAgent, tea.Printf, etc.). Exists as a test hook so the state
// transitions can be driven without the full Bubbletea event loop.
func (m *Model) applyAgentEvent(ev Event) {
	if m.pending == nil {
		return
	}
	switch ev := ev.(type) {
	case agent.ThinkingDelta:
		if m.pending.thinking == nil {
			m.pending.thinking = &thinkingCardState{
				Index:     m.nextThinkingIdx(),
				StartedAt: time.Now(),
			}
		}
		m.pending.thinking.Text += ev.Text

	case agent.TextDelta:
		if m.pending.thinking != nil && m.pending.thinking.EndedAt.IsZero() {
			m.pending.thinking.EndedAt = time.Now()
		}
		normalized := normalizeLineEndings(ev.Text)
		m.pending.raw = append(m.pending.raw, []rune(normalized)...)

	case agent.ToolCall:
		if m.pending.thinking != nil && m.pending.thinking.EndedAt.IsZero() {
			m.pending.thinking.EndedAt = time.Now()
		}
		m.pending.tools = append(m.pending.tools, &toolCardState{
			ID:        ev.ID,
			Name:      ev.Name,
			Input:     ev.Input,
			StartedAt: time.Now(),
			Index:     m.nextToolIdx(),
		})

	case agent.ToolResult:
		tc := m.findTool(ev.ID)
		if tc == nil {
			tc = &toolCardState{
				ID:    ev.ID,
				Name:  ev.Name,
				Index: m.nextToolIdx(),
			}
			m.pending.tools = append(m.pending.tools, tc)
		}
		tc.Output = ev.Output
		tc.Rewritten = ev.Rewritten
		tc.IsError = ev.IsError
		tc.EndedAt = time.Now()
		tc.Lines = countLines(ev.Output)
		tc.Bytes = len(ev.Output)
		if ev.Name == "Edit" {
			var data map[string]any
			if err := json.Unmarshal(tc.Input, &data); err == nil {
				if ns, ok := data["new_string"].(string); ok {
					tc.EditLines = strings.Count(ns, "\n") + 1
				}
			}
		}
	}
}

func (m *Model) findTool(id string) *toolCardState {
	if m.pending == nil {
		return nil
	}
	for _, tc := range m.pending.tools {
		if tc.ID == id {
			return tc
		}
	}
	return nil
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	trimmed := strings.TrimRight(s, "\n")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

func (m *Model) renderUserMsg(text string) string {
	return m.theme.UserMsg.Render("> " + text)
}
