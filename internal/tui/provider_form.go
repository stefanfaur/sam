package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stefanfaur/sam/internal/config"
)

// providerForm is an interactive picker for switching the active provider.
// Selection drives switchProvider() on apply; the active entry's
// DefaultModel seeds the model.
type providerForm struct {
	form      *huh.Form
	done      bool
	providers map[string]config.ProviderEntry
	current   string
	choice    string
}

func newProviderForm(providers map[string]config.ProviderEntry, current string) *providerForm {
	p := &providerForm{providers: providers, current: current, choice: current}
	opts := make([]huh.Option[string], 0, len(providers))
	for _, name := range sortedProviderNames(providers) {
		entry := providers[name]
		label := fmt.Sprintf("%s (%s, %s)", name, entry.Wire, entry.DefaultModel)
		opts = append(opts, huh.NewOption(label, name))
	}
	p.form = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Switch provider"),
			huh.NewSelect[string]().
				Title("Provider").
				Options(opts...).Value(&p.choice),
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
	if p.form.State == huh.StateAborted || p.choice == "" || p.choice == p.current {
		return root.addInfo("provider unchanged")
	}
	return root.switchProvider(p.choice, "")
}
