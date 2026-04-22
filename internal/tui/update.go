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
	// 16ms ~ 60fps paints; glamour deferred until turn-done so streaming
	// paint is cheap (plain text into the viewport).
	renderFPSNS = 16_000_000 // ns
)

var lastRender time.Time

func (m *Model) Init() tea.Cmd {
	return m.input.Focus()
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
		m.status.state = "idle"
		m.status.iter = 0
		m.input.Focus()
		m.rebuildViewport()
		return m, nil

	case quitMsg:
		return m, tea.Quit

	case tickMsg:
		return m.handleTick()
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
	vpH := h - inputHeight - statusHeight - 1
	if vpH < 3 {
		vpH = 3
	}
	m.viewport = viewport.New(w, vpH)
	m.debug.viewport = viewport.New(w, vpH)
	m.input.SetWidth(w - 4) // account for border + padding
	if gl, err := glamourForWidth(w); err == nil {
		m.glam = gl
	}
	m.rebuildViewport()
}

func (m *Model) View() string {
	if m.debug.visible {
		return m.debug.View(m.width, m.height)
	}

	bottom := m.renderInputBox()
	if m.modal != nil {
		bottom = m.modal.View()
	} else if m.approval != nil {
		bottom = m.approval.View()
	} else if m.suggest.active {
		bottom = m.renderSuggestions() + "\n" + m.renderInputBox()
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		m.status.View(),
		"",
		bottom,
	)
}

func (m *Model) renderInputBox() string {
	style := inputBoxStyle
	if m.input.Focused() {
		style = inputBoxFocusStyle
	}
	if m.width > 4 {
		style = style.Width(m.width - 2)
	}
	return style.Render(m.input.View())
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// modal takes all keys
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
			m.status.state = "cancelled"
			m.lastCtrlC = now
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
		return m, nil

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
		// user already typed an arg — hide suggestions
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
			b.WriteString(suggestSelectedStyle.Render("› " + s))
		} else {
			b.WriteString(suggestStyle.Render("  " + s))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) startTurn(text string) (tea.Model, tea.Cmd) {
	if cmd, arg := parseCommand(text); cmd != CmdNone {
		return m.dispatchCommand(cmd, arg)
	}

	m.history = append(m.history, renderedBlock{kind: "userMsg", content: m.renderUserMsg(text)})
	m.status.state = "thinking"
	m.status.iter = 0
	m.input.Blur()

	events := m.agent.Submit(context.Background(), text)
	m.pending = &pendingTurn{events: events, toolRow: map[string]int{}}
	m.rebuildViewport()
	return m, waitAgent(events)
}

func (m *Model) dispatchCommand(cmd Command, arg string) (tea.Model, tea.Cmd) {
	switch cmd {
	case CmdQuit:
		return m, tea.Quit
	case CmdClear:
		m.history = nil
		m.rebuildViewport()
	case CmdReset:
		m.history = nil
		m.agent.Reset()
		m.rebuildViewport()
	case CmdModel:
		if arg != "" {
			m.agent.SetModel(arg)
			m.status.model = arg
			m.addInfo("model set to " + arg)
			break
		}
		m.modal = newModelForm(m.status.provider, m.status.model)
		m.input.Blur()
		return m, m.modal.Init()
	case CmdProvider:
		if arg != "" {
			if m.factory == nil {
				m.addInfo("provider switch not available (no factory)")
				break
			}
			prov, err := m.factory(arg, m.status.model)
			if err != nil {
				m.addInfo("provider build failed: " + err.Error())
				break
			}
			m.agent.SetProvider(prov)
			m.status.provider = arg
			m.addInfo("provider set to " + arg)
			break
		}
		m.modal = newProviderForm(m.status.provider, m.factory)
		m.input.Blur()
		return m, m.modal.Init()
	case CmdAuth:
		m.modal = newAuthForm(m.status.provider, m.factory)
		m.input.Blur()
		return m, m.modal.Init()
	case CmdCwd:
		m.addInfo("cwd: " + m.agent.LaunchDir())
	case CmdHelp:
		m.addInfo(helpText)
	case CmdUnknown:
		m.addInfo("unknown command: /" + arg)
	}
	m.input.Reset()
	return m, nil
}

func (m *Model) addInfo(text string) {
	m.history = append(m.history, renderedBlock{kind: "info", content: infoStyle.Render(text)})
	m.rebuildViewport()
}

func (m *Model) handleAgentEvent(msg agentEventMsg) (tea.Model, tea.Cmd) {
	if m.pending == nil {
		return m, nil
	}
	switch ev := msg.Ev.(type) {
	case agent.MessageStart:
		// no-op

	case agent.ThinkingDelta:
		m.pending.thinkRaw = append(m.pending.thinkRaw, []rune(ev.Text)...)
		m.status.state = "thinking…"
		return m, tea.Batch(waitAgent(m.pending.events), m.ensureTick())

	case agent.TextDelta:
		m.pending.raw = append(m.pending.raw, []rune(ev.Text)...)
		m.status.state = "responding"
		return m, tea.Batch(waitAgent(m.pending.events), m.ensureTick())

	case agent.ToolCall:
		m.status.state = "tool:" + ev.Name
		// Force-drain then finalize before adding the tool row.
		m.pending.shown = len(m.pending.raw)
		m.pending.thinkShown = len(m.pending.thinkRaw)
		if len(m.pending.raw) > 0 {
			m.redrawAssistantBufferFinal()
		}
		m.redrawThinkingBuffer()
		m.pending.thinkRaw = nil
		m.pending.thinkShown = 0
		m.pending.thinkIdx = 0
		m.pending.toolRow[ev.ID] = len(m.history)
		m.history = append(m.history, renderedBlock{kind: "toolCall", content: renderToolCall(ev.Name, ev.Input)})
		m.rebuildViewport()

	case agent.ToolResult:
		m.history = append(m.history, renderedBlock{kind: "toolResult", content: renderToolResult(ev.Name, ev.Output, ev.IsError)})
		m.status.iter++
		m.rebuildViewport()

	case agent.ApprovalRequest:
		m.status.state = "awaiting-approval"
		m.approval = newApproval(ev)
		m.input.Blur()
		return m, m.approval.Init()

	case agent.ErrorEvent:
		m.history = append(m.history, renderedBlock{kind: "error", content: renderError(ev.Err)})
		m.status.state = "error"
		m.rebuildViewport()

	case agent.TurnDone:
		m.pending.done = true
		m.status.state = "idle"
		return m, tea.Batch(waitAgent(m.pending.events), m.ensureTick())
	}

	return m, waitAgent(m.pending.events)
}

// Typewriter params.
const (
	typewriterTick = 20 * time.Millisecond
	// Glamour re-render cadence (multiples of typewriterTick).
	glamourEveryNTicks = 4
)

// revealRate maps the current unshown-queue depth to how many runes to
// reveal on this tick. Keeps the baseline rate slow (≈50 cps) while letting
// bigger bursts catch up progressively.
func revealRate(queue int) int {
	switch {
	case queue > 400:
		return 6
	case queue > 150:
		return 3
	case queue > 40:
		return 2
	default:
		return 1
	}
}

func (m *Model) ensureTick() tea.Cmd {
	if m.pending == nil || m.pending.ticking {
		return nil
	}
	m.pending.ticking = true
	return tea.Tick(typewriterTick, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) handleTick() (tea.Model, tea.Cmd) {
	if m.pending == nil {
		return m, nil
	}
	m.pending.ticking = false

	advanced := false
	if m.pending.thinkShown < len(m.pending.thinkRaw) {
		queue := len(m.pending.thinkRaw) - m.pending.thinkShown
		m.pending.thinkShown += revealRate(queue)
		if m.pending.thinkShown > len(m.pending.thinkRaw) {
			m.pending.thinkShown = len(m.pending.thinkRaw)
		}
		m.redrawThinkingBuffer()
		advanced = true
	}
	if m.pending.shown < len(m.pending.raw) {
		queue := len(m.pending.raw) - m.pending.shown
		m.pending.shown += revealRate(queue)
		if m.pending.shown > len(m.pending.raw) {
			m.pending.shown = len(m.pending.raw)
		}
		m.pending.tickCount++
		// Glamour every N ticks, else plain.
		if m.pending.tickCount%glamourEveryNTicks == 0 {
			m.redrawAssistantBufferGlam()
		} else {
			m.redrawAssistantBufferPlain()
		}
		advanced = true
	}

	// All caught up and turn is done → finalize glamour render.
	drained := m.pending.shown == len(m.pending.raw) && m.pending.thinkShown == len(m.pending.thinkRaw)
	if drained && m.pending.done {
		if len(m.pending.raw) > 0 {
			m.redrawAssistantBufferFinal()
		}
		return m, nil
	}
	if advanced || !drained || !m.pending.done {
		return m, m.ensureTick()
	}
	return m, nil
}

func (m *Model) redrawAssistantBufferPlain() {
	m.writeAssistantBlock(string(m.pending.raw[:m.pending.shown]))
}

func (m *Model) redrawAssistantBufferGlam() {
	text := string(m.pending.raw[:m.pending.shown])
	// Split into a stable prefix (ends at the last completed paragraph) and
	// a volatile tail. Only the prefix goes through glamour — once rendered
	// it doesn't get re-run, so nothing flickers on subsequent ticks.
	stable := ""
	if idx := strings.LastIndex(text, "\n\n"); idx >= 0 {
		stable = text[:idx+2]
	}
	if len(stable) > len(m.pending.stablePrefix) {
		r, err := m.glam.Render(stable)
		if err != nil {
			r = stable
		}
		m.pending.stableRendered = strings.TrimRight(r, "\n")
		m.pending.stablePrefix = stable
	}
	tail := text[len(m.pending.stablePrefix):]
	content := m.pending.stableRendered
	if tail != "" {
		if content != "" {
			content += "\n"
		}
		content += tail
	}
	m.writeAssistantBlock(content)
}

func (m *Model) writeAssistantBlock(content string) {
	if m.pending.rendered && len(m.history) > 0 && m.history[len(m.history)-1].kind == "assistantMsg" {
		m.history[len(m.history)-1].content = content
	} else {
		m.history = append(m.history, renderedBlock{kind: "assistantMsg", content: content})
		m.pending.rendered = true
	}
	m.rebuildViewport()
}

func (m *Model) redrawAssistantBufferFinal() {
	text := string(m.pending.raw)
	rendered, err := m.glam.Render(text)
	if err != nil {
		rendered = text
	}
	rendered = strings.TrimRight(rendered, "\n")
	if m.pending.rendered && len(m.history) > 0 && m.history[len(m.history)-1].kind == "assistantMsg" {
		m.history[len(m.history)-1].content = rendered
	} else {
		m.history = append(m.history, renderedBlock{kind: "assistantMsg", content: rendered})
		m.pending.rendered = true
	}
	m.pending.raw = nil
	m.pending.shown = 0
	m.pending.rendered = false
	m.pending.stablePrefix = ""
	m.pending.stableRendered = ""
	m.rebuildViewport()
}

func (m *Model) redrawThinkingBuffer() {
	text := string(m.pending.thinkRaw[:m.pending.thinkShown])
	streaming := m.pending.thinkShown < len(m.pending.thinkRaw)
	card := renderThinkingCard(text, streaming)
	if m.pending.thinkIdx > 0 && m.pending.thinkIdx <= len(m.history) {
		m.history[m.pending.thinkIdx-1].content = card
	} else {
		m.history = append(m.history, renderedBlock{kind: "thinking", content: card})
		m.pending.thinkIdx = len(m.history) // 1-based
	}
	m.rebuildViewport()
}

func renderThinkingCard(text string, streaming bool) string {
	header := thinkingHeaderStyle.Render("✦ thinking")
	body := strings.TrimRight(text, "\n")
	return thinkingStyle.Render(header + "\n" + body)
}

func (m *Model) rebuildViewport() {
	var sb strings.Builder
	for i, r := range m.history {
		sb.WriteString(r.content)
		if i < len(m.history)-1 {
			sb.WriteString("\n\n")
		}
	}
	content := sb.String()
	if content != "" {
		content += "\n"
	}
	// Anchor content to bottom with blank padding only for short content.
	lines := strings.Count(content, "\n") + 1
	if content == "" {
		lines = 0
	}
	if pad := m.viewport.Height - lines; pad > 0 {
		content = strings.Repeat("\n", pad) + content
	}
	// Skip setter + GotoBottom if nothing changed — prevents flicker during
	// ticker cycles that don't actually advance the buffer.
	if content == m.lastVP {
		return
	}
	m.lastVP = content
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m *Model) renderUserMsg(text string) string {
	return userMsgStyle.Render("> " + text)
}
