package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/stefanfaur/sam/internal/config"
)

// settingsKeyMap binds arrow keys for field-to-field navigation on fields that
// don't otherwise consume up/down (Confirm, Input, Note). Selects and
// MultiSelects keep arrows for option selection — use Enter to advance out.
func settingsKeyMap() *huh.KeyMap {
	k := huh.NewDefaultKeyMap()
	k.Confirm.Next = key.NewBinding(key.WithKeys("enter", "down"), key.WithHelp("enter/↓", "next"))
	k.Confirm.Prev = key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("↑", "back"))
	k.Input.Next = key.NewBinding(key.WithKeys("enter", "down"), key.WithHelp("enter/↓", "next"))
	k.Input.Prev = key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("↑", "back"))
	k.Note.Next = key.NewBinding(key.WithKeys("enter", "down"), key.WithHelp("enter/↓", "next"))
	k.Note.Prev = key.NewBinding(key.WithKeys("shift+tab", "up"), key.WithHelp("↑", "back"))
	// Selects keep enter to advance to next field (arrows already taken by options).
	k.Select.Next = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "next"))
	k.Select.Prev = key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "back"))
	k.MultiSelect.Next = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "next"))
	k.MultiSelect.Prev = key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "back"))
	return k
}

type settingsTab int

const (
	tabStatusline settingsTab = iota
	tabProviders
	tabTheme
	numTabs
)

type settingsModal struct {
	forms     [numTabs]*huh.Form
	active    settingsTab
	done      bool
	cancelled bool

	pending      Settings
	pendingTheme *Theme

	provider     string
	minimaxKey   string
	anthropicKey string

	selectedSegments []string

	factory ProviderFactory
}

func newSettingsModal(current Settings, curProvider string, factory ProviderFactory) *settingsModal {
	m := &settingsModal{
		pending:      current,
		pendingTheme: NewTheme(current.Theme),
		provider:     curProvider,
		factory:      factory,
	}
	m.selectedSegments = segmentsStructToSlice(current.Statusbar.Segments)
	km := settingsKeyMap()
	m.forms[tabStatusline] = m.buildStatuslineForm().WithKeyMap(km)
	m.forms[tabProviders] = m.buildProvidersForm().WithKeyMap(km)
	m.forms[tabTheme] = m.buildThemeForm().WithKeyMap(km)
	return m
}

func (m *settingsModal) Init() tea.Cmd { return m.forms[m.active].Init() }

func (m *settingsModal) Update(msg tea.Msg) tea.Cmd {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.Type {
		case tea.KeyTab:
			m.active = (m.active + 1) % numTabs
			return m.forms[m.active].Init()
		case tea.KeyShiftTab:
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
	m.pending.Statusbar.Segments = segmentsSliceToStruct(m.selectedSegments)
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
		Render("Tab/Shift+Tab switch tab  ↑↓←→ navigate  ^S save  Esc cancel")
	sections := []string{tabs, body}
	if preview != "" {
		sections = append(sections, preview)
	}
	sections = append(sections, footer)
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *settingsModal) Done() bool { return m.done }

func (m *settingsModal) Apply(root *Model) tea.Cmd {
	if m.cancelled {
		return root.addInfo("settings cancelled")
	}
	probeTheme := NewTheme(m.pending.Theme)
	if probeTheme.Glamour() == nil {
		return root.addInfo("save aborted: glamour style invalid")
	}
	if err := SaveSettings(m.pending); err != nil {
		return root.addInfo("save settings failed: " + err.Error())
	}
	if cmd := m.applyProviders(root); cmd != nil {
		return cmd
	}
	root.settings = m.pending
	root.theme = probeTheme
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

func (m *settingsModal) buildStatuslineForm() *huh.Form {
	p := &m.pending.Statusbar
	return huh.NewForm(
		huh.NewGroup(
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
				).Value(&m.selectedSegments),
		),
	).WithShowHelp(true).WithShowErrors(false)
}

func (m *settingsModal) buildProvidersForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Provider").
				Options(
					huh.NewOption("Minimax", "minimax"),
					huh.NewOption("Anthropic", "anthropic"),
				).Value(&m.provider),
			huh.NewInput().Title("Minimax API key (blank = keep existing)").
				EchoMode(huh.EchoModePassword).Value(&m.minimaxKey),
			huh.NewInput().Title("Anthropic API key (blank = keep existing)").
				EchoMode(huh.EchoModePassword).Value(&m.anthropicKey),
		),
	).WithShowHelp(true)
}

func (m *settingsModal) buildThemeForm() *huh.Form {
	t := &m.pending.Theme
	validateHex := func(s string) error {
		if s == "" {
			return nil
		}
		if !strings.HasPrefix(s, "#") || len(s) != 7 {
			return errInvalidHex
		}
		return nil
	}
	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Glamour style").
				Options(
					huh.NewOption("dark", "dark"),
					huh.NewOption("light", "light"),
					huh.NewOption("dracula", "dracula"),
					huh.NewOption("notty", "notty"),
				).Value(&t.GlamourStyle),
			huh.NewInput().Title("Accent (#rrggbb)").Value(&t.Accent).Validate(validateHex),
			huh.NewInput().Title("Muted").Value(&t.Muted).Validate(validateHex),
			huh.NewInput().Title("User border").Value(&t.UserBorder).Validate(validateHex),
			huh.NewInput().Title("Error fg").Value(&t.ErrorFg).Validate(validateHex),
			huh.NewInput().Title("State: thinking").Value(&t.StateThinking).Validate(validateHex),
			huh.NewInput().Title("State: responding").Value(&t.StateResponding).Validate(validateHex),
			huh.NewInput().Title("State: tool").Value(&t.StateTool).Validate(validateHex),
			huh.NewInput().Title("State: error").Value(&t.StateError).Validate(validateHex),
			huh.NewInput().Title("State: approval").Value(&t.StateApproval).Validate(validateHex),
		),
	).WithShowHelp(true).WithShowErrors(true)
}

func (m *settingsModal) renderStatuslinePreview() string {
	preview := &Model{
		settings: m.pending,
		theme:    m.pendingTheme,
		width:    80,
		status: statusbarModel{
			provider:   "anthropic",
			model:      "claude-sonnet-4-6",
			state:      "responding",
			iter:       3,
			maxIter:    25,
			lastIterIn: 42_000,
			turnIn:     12_345,
			turnOut:    678,
		},
		git:       gitInfo{branch: "main", dirty: false},
		spinner:   spinnerState{on: m.pending.Statusbar.Spinner, frame: 0},
		turnStart: time.Now().Add(-1200 * time.Millisecond),
		ctxWinFn:  func(string) int { return 200_000 },
	}
	rows := preview.StatusBar()
	border := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)
	return border.Render(strings.Join(rows, "\n"))
}
func (m *settingsModal) renderThemePreview() string {
	sample := "# Heading\n\nSome **bold** text and `code`.\n\n- bullet one\n- bullet two"
	rendered := sample
	if g := m.pendingTheme.Glamour(); g != nil {
		if out, err := g.Render(sample); err == nil {
			rendered = out
		}
	}
	status := "⟳ responding · 1.2s · ↻ 3/25 · ctx 42% · anthropic/sonnet-4-6"
	styled := m.pendingTheme.StatusBar.Render(status)
	border := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)
	return border.Render(rendered + "\n" + styled)
}

var errInvalidHex = errHex("expected #rrggbb")

type errHex string

func (e errHex) Error() string { return string(e) }
func (m *settingsModal) applyProviders(root *Model) tea.Cmd {
	s := config.LoadSecrets()
	changed := false
	if m.minimaxKey != "" {
		s.MinimaxAPIKey = m.minimaxKey
		changed = true
	}
	if m.anthropicKey != "" {
		s.AnthropicAPIKey = m.anthropicKey
		changed = true
	}
	if changed {
		if err := config.SaveSecrets(s); err != nil {
			return root.addInfo("save secrets failed: " + err.Error())
		}
		s.ApplyEnv()
	}
	if m.factory != nil && m.provider != "" && m.provider != root.status.provider {
		p, err := m.factory(m.provider, root.status.model)
		if err != nil {
			return root.addInfo("provider build failed: " + err.Error())
		}
		root.agent.SetProvider(p)
		root.status.provider = m.provider
	}
	return nil
}

func segmentsStructToSlice(s StatusbarSegments) []string {
	var out []string
	if s.State {
		out = append(out, "state")
	}
	if s.Model {
		out = append(out, "model")
	}
	if s.Provider {
		out = append(out, "provider")
	}
	if s.Cwd {
		out = append(out, "cwd")
	}
	if s.Git {
		out = append(out, "git")
	}
	if s.Iterations {
		out = append(out, "iterations")
	}
	if s.Context {
		out = append(out, "context")
	}
	if s.Tokens {
		out = append(out, "tokens")
	}
	if s.Keybinds {
		out = append(out, "keybinds")
	}
	return out
}

func segmentsSliceToStruct(sel []string) StatusbarSegments {
	var s StatusbarSegments
	for _, name := range sel {
		switch name {
		case "state":
			s.State = true
		case "model":
			s.Model = true
		case "provider":
			s.Provider = true
		case "cwd":
			s.Cwd = true
		case "git":
			s.Git = true
		case "iterations":
			s.Iterations = true
		case "context":
			s.Context = true
		case "tokens":
			s.Tokens = true
		case "keybinds":
			s.Keybinds = true
		}
	}
	return s
}
