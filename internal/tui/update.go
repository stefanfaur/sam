package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stefanfaur/sam/internal/agent"
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

	// Live tail (uncommitted assistant text).
	if m.pending != nil && m.pending.committed < len(m.pending.raw) {
		tail := string(m.pending.raw[m.pending.committed:])
		parts = append(parts, tail)
	}

	// Thinking card.
	if m.pending != nil && len(m.pending.thinkRaw) > 0 {
		card := renderThinkingCard(m.theme, string(m.pending.thinkRaw), true)
		parts = append(parts, card)
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
		if m.suggest.active {
			m.suggest.active = false
			return m, nil
		}

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
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.input.Reset()
		m.suggest.active = false
		return m.startTurn(text)
	}

	if m.approval != nil {
		return m, m.approval.Update(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refreshSuggestions()
	return m, cmd
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
	for _, s := range commandSuggestions {
		if strings.HasPrefix(s.Name, v) {
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
	if cmd, arg := parseCommand(text); cmd != CmdNone {
		return m.dispatchCommand(cmd, arg)
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

func (m *Model) dispatchCommand(cmd Command, arg string) (tea.Model, tea.Cmd) {
	switch cmd {
	case CmdQuit:
		return m, tea.Quit

	case CmdClear:
		m.pending = nil
		m.scanner = &blockScanner{}
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
		if arg != "" {
			m.agent.SetModel(arg)
			m.status.model = arg
			m.input.Reset()
			m.suggest.active = false
			return m, m.addInfo("model set to " + arg)
		}
		m.modal = newModelForm(m.status.provider, m.status.model)
		m.input.Blur()
		return m, m.modal.Init()

	case CmdSettings:
		m.input.Reset()
		m.suggest.active = false
		m.modal = newSettingsModal(m.settings, m.status.provider, m.factory)
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
	case agent.MessageStart:
		return m, waitAgent(m.pending.events)

	case agent.ThinkingDelta:
		m.pending.thinkRaw = append(m.pending.thinkRaw, []rune(ev.Text)...)
		m.status.state = "thinking…"
		return m, waitAgent(m.pending.events)

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
		rendered := safeGlamourRender(m.theme.Glamour(), string(prefix))
		m.scanner.Advance(prefix)
		m.pending.committed += idx
		return m, tea.Sequence(
			tea.Printf("%s", rendered),
			waitAgent(m.pending.events),
		)

	case agent.ToolCall:
		m.status.state = "tool:" + ev.Name
		var flushCmd tea.Cmd
		if m.pending.committed < len(m.pending.raw) {
			tail := m.pending.raw[m.pending.committed:]
			rendered := safeGlamourRender(m.theme.Glamour(), string(tail))
			m.scanner.Advance(tail)
			m.pending.committed = len(m.pending.raw)
			flushCmd = tea.Printf("%s", rendered)
		}
		m.pending.thinkRaw = nil
		toolCallStr := renderToolCall(m.theme, ev.Name, ev.Input)
		cmds := []tea.Cmd{}
		if flushCmd != nil {
			cmds = append(cmds, flushCmd)
		}
		cmds = append(cmds,
			tea.Printf("%s", toolCallStr),
			waitAgent(m.pending.events),
		)
		return m, tea.Sequence(cmds...)

	case agent.ToolResult:
		result := renderToolResult(m.theme, ev.Name, ev.Output, ev.IsError)
		m.status.iter++
		return m, tea.Sequence(
			tea.Printf("%s", result),
			waitAgent(m.pending.events),
		)

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
		return m, tea.Sequence(
			tea.Printf("%s", errorStr),
			waitAgent(m.pending.events),
		)

	case agent.TurnDone:
		m.pending.done = true
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
		if flush != nil {
			cmds = append(cmds, flush)
		}
		cmds = append(cmds, func() tea.Msg { return turnClosedMsg{} })
		return m, tea.Sequence(cmds...)
	}

	return m, waitAgent(m.pending.events)
}

func renderThinkingCard(t *Theme, text string, streaming bool) string {
	header := t.ThinkingHeader.Render("✦ thinking")
	body := strings.TrimRight(text, "\n")
	return t.Thinking.Render(header + "\n" + body)
}

func (m *Model) renderUserMsg(text string) string {
	return m.theme.UserMsg.Render("> " + text)
}
