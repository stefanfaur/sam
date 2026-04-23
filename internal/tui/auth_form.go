package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stefanfaur/sam/internal/config"
)

// authForm prompts for an API key. When built via newAuthForm(name) it goes
// straight to the key input; via newAuthPicker() it first selects a provider
// then prompts for the key.
type authForm struct {
	form       *huh.Form
	done       bool
	providers  map[string]config.ProviderEntry
	provider   string
	key        string
	pickerMode bool
}

// newAuthForm builds a one-provider key-entry form (arg path: /auth <name>).
func newAuthForm(provider string) *authForm {
	a := &authForm{provider: provider}
	a.form = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Set API key for " + provider),
			huh.NewInput().
				Title("API key").
				EchoMode(huh.EchoModePassword).
				Value(&a.key),
		),
	).WithShowHelp(true)
	return a
}

// newAuthPicker builds a two-step picker: choose provider, then enter key.
// Used by /auth with no argument.
func newAuthPicker(providers map[string]config.ProviderEntry, current string) *authForm {
	a := &authForm{providers: providers, provider: current, pickerMode: true}
	opts := make([]huh.Option[string], 0, len(providers))
	for _, name := range sortedProviderNames(providers) {
		entry := providers[name]
		status := "✗"
		if entry.APIKeyEnv != "" && os.Getenv(entry.APIKeyEnv) != "" {
			status = "✓"
		}
		label := fmt.Sprintf("%s [%s] %s", name, status, entry.APIKeyEnv)
		opts = append(opts, huh.NewOption(label, name))
	}
	a.form = huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Set API key"),
			huh.NewSelect[string]().
				Title("Provider").
				Options(opts...).Value(&a.provider),
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
	if a.form.State == huh.StateAborted || a.key == "" || a.provider == "" {
		return root.addInfo("auth: cancelled")
	}
	return root.saveAuthKey(a.provider, a.key)
}
