package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stefanfaur/sam/internal/config"
	"github.com/stefanfaur/sam/internal/llm"
)

// modal is a generic interactive form that captures input while active.
type modal interface {
	Init() tea.Cmd
	Update(tea.Msg) tea.Cmd
	View() string
	Done() bool
	Apply(*Model) tea.Cmd
}

// ProviderFactory builds an llm.Provider for the given name and model.
// Used by /provider and /auth so TUI can rebuild the underlying client
// without main needing to know about TUI internals.
type ProviderFactory func(name, model string) (llm.Provider, error)

// --- /auth ---

type authForm struct {
	form    *huh.Form
	done    bool
	factory ProviderFactory

	provider string
	key      string
}

func newAuthForm(defaultProvider string, f ProviderFactory) *authForm {
	a := &authForm{factory: f, provider: defaultProvider}
	a.form = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Authenticate SAM").
				Description("Saved to ~/.config/sam/secrets.toml (0600)."),
			huh.NewSelect[string]().
				Title("Provider").
				Options(
					huh.NewOption("Minimax", "minimax"),
					huh.NewOption("Anthropic", "anthropic"),
				).Value(&a.provider),
			huh.NewInput().
				Title("API key").
				EchoMode(huh.EchoModePassword).
				Value(&a.key),
		),
	).WithShowHelp(true).WithShowErrors(true)
	return a
}

func (a *authForm) Init() tea.Cmd { return a.form.Init() }

func (a *authForm) Update(msg tea.Msg) tea.Cmd {
	f, cmd := a.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		a.form = ff
	}
	if a.form.State == huh.StateCompleted || a.form.State == huh.StateAborted {
		a.done = true
	}
	return cmd
}

func (a *authForm) View() string { return a.form.View() }
func (a *authForm) Done() bool   { return a.done }

func (a *authForm) Apply(m *Model) tea.Cmd {
	if a.form.State == huh.StateAborted || a.key == "" {
		return m.addInfo("auth cancelled")
	}
	s := config.LoadSecrets()
	switch a.provider {
	case "minimax":
		s.MinimaxAPIKey = a.key
	case "anthropic":
		s.AnthropicAPIKey = a.key
	}
	if err := config.SaveSecrets(s); err != nil {
		return m.addInfo("save secrets failed: " + err.Error())
	}
	s.ApplyEnv()

	// Rebuild provider so the new key takes effect immediately.
	if a.factory != nil {
		p, err := a.factory(a.provider, m.status.model)
		if err != nil {
			return m.addInfo(fmt.Sprintf("%s key saved; provider rebuild failed: %v", a.provider, err))
		}
		m.agent.SetProvider(p)
		m.status.provider = a.provider
	}
	return m.addInfo(fmt.Sprintf("saved %s API key to %s", a.provider, config.SecretsPath()))
}

// --- /model ---

var modelCatalogue = map[string][]string{
	"minimax": {
		"MiniMax-M2.7",
		"MiniMax-M2.6",
		"MiniMax-M1",
	},
	"anthropic": {
		"claude-opus-4-7",
		"claude-sonnet-4-6",
		"claude-haiku-4-5",
	},
}

type modelForm struct {
	form   *huh.Form
	done   bool
	choice string
	custom string
}

func newModelForm(provider, current string) *modelForm {
	m := &modelForm{choice: current}
	opts := []huh.Option[string]{}
	for _, name := range modelCatalogue[provider] {
		opts = append(opts, huh.NewOption(name, name))
	}
	opts = append(opts, huh.NewOption("custom…", "__custom__"))

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Select model for "+provider),
			huh.NewSelect[string]().
				Title("Model").
				Options(opts...).Value(&m.choice),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Custom model name").
				Value(&m.custom),
		).WithHideFunc(func() bool { return m.choice != "__custom__" }),
	).WithShowHelp(true)
	return m
}

func (m *modelForm) Init() tea.Cmd { return m.form.Init() }

func (m *modelForm) Update(msg tea.Msg) tea.Cmd {
	f, cmd := m.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		m.form = ff
	}
	if m.form.State == huh.StateCompleted || m.form.State == huh.StateAborted {
		m.done = true
	}
	return cmd
}

func (m *modelForm) View() string { return m.form.View() }
func (m *modelForm) Done() bool   { return m.done }

func (m *modelForm) Apply(root *Model) tea.Cmd {
	if m.form.State == huh.StateAborted {
		return root.addInfo("model unchanged")
	}
	name := m.choice
	if name == "__custom__" {
		name = m.custom
	}
	if name == "" {
		return root.addInfo("model unchanged")
	}
	root.agent.SetModel(name)
	root.status.model = name
	return root.addInfo("model set to " + name)
}

// --- /provider ---

type providerForm struct {
	form    *huh.Form
	done    bool
	choice  string
	factory ProviderFactory
}

func newProviderForm(current string, factory ProviderFactory) *providerForm {
	p := &providerForm{choice: current, factory: factory}
	p.form = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Switch provider"),
			huh.NewSelect[string]().
				Title("Provider").
				Options(
					huh.NewOption("Minimax (minimax)", "minimax"),
					huh.NewOption("Anthropic (anthropic)", "anthropic"),
				).Value(&p.choice),
		),
	).WithShowHelp(true)
	return p
}

func (p *providerForm) Init() tea.Cmd { return p.form.Init() }

func (p *providerForm) Update(msg tea.Msg) tea.Cmd {
	f, cmd := p.form.Update(msg)
	if ff, ok := f.(*huh.Form); ok {
		p.form = ff
	}
	if p.form.State == huh.StateCompleted || p.form.State == huh.StateAborted {
		p.done = true
	}
	return cmd
}

func (p *providerForm) View() string { return p.form.View() }
func (p *providerForm) Done() bool   { return p.done }

func (p *providerForm) Apply(root *Model) tea.Cmd {
	if p.form.State == huh.StateAborted || p.choice == "" {
		return root.addInfo("provider unchanged")
	}
	if p.factory == nil {
		return root.addInfo("provider switch not available")
	}
	prov, err := p.factory(p.choice, root.status.model)
	if err != nil {
		return root.addInfo("provider build failed: " + err.Error())
	}
	root.agent.SetProvider(prov)
	root.status.provider = p.choice
	return root.addInfo("provider set to " + p.choice)
}
