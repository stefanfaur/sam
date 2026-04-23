package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
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
type ProviderFactory func(name, model string) (llm.Provider, error)

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
	root.persistSelection()
	return root.addInfo("model set to " + name)
}
