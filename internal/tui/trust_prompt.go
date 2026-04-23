package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stefanfaur/sam/internal/skills"
)

// trustPromptModal is fired on boot when the registry has loaded project
// skills from a project root that is neither trusted nor denied.
type trustPromptModal struct {
	reg        *skills.Registry
	projectDir string
	pending    []*skills.Skill
	choice     rune // 'y' | 'o' | 'n' | 'v' while viewing
	viewing    bool
	done       bool
}

// newTrustPromptModal builds a trust prompt for the first pending project root
// among the registry's skills. Returns nil if there are no pending skills.
func newTrustPromptModal(reg *skills.Registry) *trustPromptModal {
	if reg == nil {
		return nil
	}
	byRoot := map[string][]*skills.Skill{}
	for _, sk := range reg.List() {
		if sk.Pending && sk.Source == skills.SourceProject {
			byRoot[sk.Root] = append(byRoot[sk.Root], sk)
		}
	}
	if len(byRoot) == 0 {
		return nil
	}
	var keys []string
	for k := range byRoot {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	first := keys[0]
	return &trustPromptModal{
		reg:        reg,
		projectDir: first,
		pending:    byRoot[first],
	}
}

func (t *trustPromptModal) Init() tea.Cmd { return nil }

func (t *trustPromptModal) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	if t.viewing {
		if km.Type == tea.KeyEsc || (km.Type == tea.KeyRunes && len(km.Runes) == 1 && km.Runes[0] == 'v') {
			t.viewing = false
		}
		return nil
	}
	if km.Type != tea.KeyRunes || len(km.Runes) != 1 {
		return nil
	}
	switch km.Runes[0] {
	case 'v':
		t.viewing = true
	case 'y':
		t.choice = 'y'
		t.done = true
	case 'o':
		t.choice = 'o'
		t.done = true
	case 'n':
		t.choice = 'n'
		t.done = true
	}
	return nil
}

func (t *trustPromptModal) View() string {
	if t.viewing {
		var b strings.Builder
		b.WriteString("Project skills in ")
		b.WriteString(t.projectDir)
		b.WriteString(":\n\n")
		for _, sk := range t.pending {
			fmt.Fprintf(&b, "  • %s — %s\n", sk.Name, firstLine(sk.FM.Description))
			firstBody := firstLine(sk.Body)
			if firstBody != "" {
				fmt.Fprintf(&b, "      %s\n", firstBody)
			}
		}
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).
			Render("[v]/[Esc] back"))
		return b.String()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d skills in %s\n", len(t.pending), t.projectDir)
	b.WriteString("Skills can inject instructions into your agent turns.\n\n")
	b.WriteString("Trust this project?\n")
	b.WriteString("  [v]iew  [y]es permanent  [o]nce  [n]o (deny list)\n")
	return b.String()
}

func (t *trustPromptModal) Done() bool { return t.done }

func (t *trustPromptModal) Apply(root *Model) tea.Cmd {
	if t.reg == nil {
		return nil
	}
	trust := t.reg.Trust()
	switch t.choice {
	case 'y':
		trust.Trust(t.projectDir)
		ov := t.reg.Overrides()
		if ov.Trust == nil {
			ov.Trust = map[string]bool{}
		}
		ov.Trust[t.projectDir] = true
		if err := skills.SaveOverrides(ov); err != nil {
			return root.addInfo("trust save failed: " + err.Error())
		}
		t.reg.SetOverrides(ov)
		t.reg.SetTrust(trust)
		if root.agent != nil {
			root.agent.RebuildSkillCatalog()
		}
		return root.addInfo("project trusted: " + t.projectDir)
	case 'o':
		trust.Trust(t.projectDir)
		t.reg.SetTrust(trust)
		if root.agent != nil {
			root.agent.RebuildSkillCatalog()
		}
		return root.addInfo("project trusted for this session")
	case 'n':
		trust.Deny(t.projectDir)
		ov := t.reg.Overrides()
		if ov.Deny == nil {
			ov.Deny = map[string]bool{}
		}
		ov.Deny[t.projectDir] = true
		delete(ov.Trust, t.projectDir)
		if err := skills.SaveOverrides(ov); err != nil {
			return root.addInfo("trust save failed: " + err.Error())
		}
		t.reg.SetOverrides(ov)
		t.reg.SetTrust(trust)
		if root.agent != nil {
			root.agent.RebuildSkillCatalog()
		}
		return root.addInfo("project added to deny list")
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
