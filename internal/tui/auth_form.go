package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// authForm is a small modal that prompts for a provider's API key and
// delegates persistence to Model.saveAuthKey on apply.
type authForm struct {
	form     *huh.Form
	done     bool
	provider string
	key      string
}

func newAuthForm(provider string) *authForm {
	a := &authForm{provider: provider}
	a.form = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Set API key for "+provider),
			huh.NewInput().
				Title("API key").
				EchoMode(huh.EchoModePassword).
				Value(&a.key),
		),
	).WithShowHelp(true)
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

func (a *authForm) Apply(root *Model) tea.Cmd {
	if a.form.State == huh.StateAborted || a.key == "" {
		return root.addInfo("auth: cancelled")
	}
	return root.saveAuthKey(a.provider, a.key)
}
