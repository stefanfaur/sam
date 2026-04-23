package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stefanfaur/sam/internal/config"
)

// applyModelSpec resolves /model's argument (bare or provider/model) and
// applies the switch to the agent + status bar.
func (m *Model) applyModelSpec(spec string) tea.Cmd {
	provider, model, err := resolveModelSpec(spec, m.providers)
	if err != nil {
		return m.addInfo("model: " + err.Error())
	}
	if provider != m.status.provider {
		return m.switchProvider(provider, model)
	}
	m.agent.SetModel(model)
	m.status.model = model
	return m.addInfo(fmt.Sprintf("model set to %s/%s", provider, model))
}

// switchProvider rebuilds the provider for name, optionally overriding the
// model (empty model falls back to the entry's DefaultModel).
func (m *Model) switchProvider(name, model string) tea.Cmd {
	entry, ok := m.providers[name]
	if !ok {
		return m.addInfo(fmt.Sprintf("unknown provider %q (known: %s)", name, strings.Join(sortedProviderNames(m.providers), ", ")))
	}
	if m.factory == nil {
		return m.addInfo("provider factory unavailable")
	}
	if model == "" {
		model = entry.DefaultModel
	}
	p, err := m.factory(name, model)
	if err != nil {
		return m.addInfo("provider build failed: " + err.Error())
	}
	m.agent.SetProvider(p)
	m.agent.SetModel(model)
	m.status.provider = name
	m.status.model = model
	return m.addInfo(fmt.Sprintf("switched to %s/%s", name, model))
}

// providerListSummary renders a compact provider table — one row per entry
// with name, wire, and key-set status.
func (m *Model) providerListSummary() string {
	var b strings.Builder
	b.WriteString("Providers:\n")
	names := sortedProviderNames(m.providers)
	for _, name := range names {
		entry := m.providers[name]
		status := "✗"
		if entry.APIKeyEnv != "" && os.Getenv(entry.APIKeyEnv) != "" {
			status = "✓"
		}
		mark := " "
		if name == m.status.provider {
			mark = "*"
		}
		b.WriteString(fmt.Sprintf("  %s %s [%s] %s (env: %s)\n",
			mark, name, entry.Wire, status, entry.APIKeyEnv))
	}
	return strings.TrimRight(b.String(), "\n")
}

// handleAuth dispatches /auth:
//   - no arg → render provider key-status table
//   - `<name>` → prompt for key via modal, persist, reapply env, rebuild active provider
func (m *Model) handleAuth(arg string) tea.Cmd {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return m.addInfo(m.providerListSummary())
	}
	if _, ok := m.providers[arg]; !ok {
		return m.addInfo(fmt.Sprintf("unknown provider %q", arg))
	}
	m.modal = newAuthForm(arg)
	m.input.Blur()
	return m.modal.Init()
}

// saveAuthKey is the shared finalizer used by the /auth modal apply path.
// Persists the key to secrets.toml under [api_keys].<name> and rebuilds the
// active provider when <name> matches the current one.
func (m *Model) saveAuthKey(name, key string) tea.Cmd {
	s := config.LoadSecrets()
	if s.APIKeys == nil {
		s.APIKeys = map[string]string{}
	}
	s.APIKeys[name] = key
	if err := config.SaveSecrets(s); err != nil {
		return m.addInfo("save secrets failed: " + err.Error())
	}
	entry := m.providers[name]
	if entry.APIKeyEnv != "" {
		_ = os.Setenv(entry.APIKeyEnv, key)
	}
	if name == m.status.provider && m.factory != nil {
		p, err := m.factory(name, m.status.model)
		if err != nil {
			return m.addInfo("rebuild failed: " + err.Error())
		}
		m.agent.SetProvider(p)
	}
	return m.addInfo(fmt.Sprintf("auth: saved key for %s", name))
}

// renderProviderStatus returns a stable, sorted listing for the settings
// modal's Providers tab (read-only).
func renderProviderStatus(providers map[string]config.ProviderEntry, active string) string {
	names := make([]string, 0, len(providers))
	for k := range providers {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		entry := providers[name]
		status := "✗ missing"
		if entry.APIKeyEnv != "" && os.Getenv(entry.APIKeyEnv) != "" {
			status = "✓ set"
		}
		mark := " "
		if name == active {
			mark = "*"
		}
		b.WriteString(fmt.Sprintf("  %s %-10s %-10s %-20s %s\n",
			mark, name, entry.Wire, entry.APIKeyEnv, status))
	}
	return strings.TrimRight(b.String(), "\n")
}
